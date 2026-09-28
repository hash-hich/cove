// Package turn is how a turn of the agent goes between the host and the init: the port the init
// listens on, one connection per turn, and the frames that go through it. The host sends a Request
// first, then what the agent reads; the init answers Started once the agent runs, what it writes,
// and Exited last, or Failed when it could not run it. The init closes the connection after the
// last frame, so the host knows everything was read once it has one.
//
// Every stream of a turn goes through the one connection, as docker attach, SSH and Kubernetes
// carry theirs: the end of the agent cannot overtake the last bytes it wrote, as it can when it
// comes by another way. A slow reader on the host slows stdout and stderr alike, which costs
// nothing, since the host reads everything.
//
// A frame is its Kind on one byte, the length of its payload on four, big endian, then the
// payload. The init and the host come from one build, so an unknown kind or field is refused.
package turn

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Port is the vsock port the init listens on for turns, apart from control.Port: a turn is
// cancelled on that one, by its ID, and never reports on it.
const Port = 1025

// DefaultGrace is how long the processes of a turn that is cut have to end before SIGKILL, as
// docker gives a process that is stopped.
const DefaultGrace = 10 * time.Second

// MaxPayload bounds the payload of a frame. The streams are cut in chunks well below it.
const MaxPayload = 1 << 20

// Kind is the kind of a frame.
type Kind byte

// The frames the host sends.
const (
	// Exec is the Request, the first frame and the only one of its kind.
	Exec Kind = 16
	// Stdin is bytes for the agent to read.
	Stdin Kind = 17
	// StdinEOF closes what the agent reads. It is a frame rather than a half closed connection,
	// which the Unix sockets of the monitors are not known to carry to the guest.
	StdinEOF Kind = 18
	// Resize gives the terminal of the agent a size: its rows then its columns, two bytes each.
	Resize Kind = 19
)

// The frames the init sends.
const (
	// Started says the agent runs, once its execve succeeded, with its pid on four bytes.
	Started Kind = 1
	// Stdout and Stderr are bytes the agent wrote. An agent on a terminal writes only Stdout.
	Stdout Kind = 2
	Stderr Kind = 3
	// Exited is the Exit of the agent, the last frame of a turn that started.
	Exited Kind = 4
	// Failed is an ExecError, the last frame of a turn that did not start.
	Failed Kind = 5
)

// Request is the turn to run.
type Request struct {
	// ID names the turn, for a cancellation to reach it. The host draws it.
	ID string `json:"id"`
	// Args is the command line of the agent.
	Args []string `json:"args"`
	// TTY gives the agent a terminal of Size, which the host keeps resizing; its streams are
	// pipes otherwise.
	TTY  bool `json:"tty,omitempty"`
	Size Size `json:"size,omitzero"`
	// Grace is how long the processes of the turn have to end before SIGKILL once the connection
	// is lost.
	Grace time.Duration `json:"grace"`
}

// Size is the size of a terminal. Zero leaves the size the kernel gives.
type Size struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// Cause is why a turn ended.
type Cause string

// The causes of the end of a turn. The init reports CauseAgent, CauseCancel, CauseTimeout and
// CauseStop; the host adds CauseExec, for a Failed turn, and CauseCove, for a turn whose end it
// never got.
const (
	// CauseAgent is the agent that ended by itself, by a signal cove had no part in included.
	CauseAgent Cause = "agent"
	// CauseExec is a turn whose agent could not be run.
	CauseExec Cause = "exec"
	// CauseTimeout is a turn cut because its time was up.
	CauseTimeout Cause = "timeout"
	// CauseCancel is a turn cut because the host asked, on a signal it got.
	CauseCancel Cause = "cancel"
	// CauseStop is a turn cut because its sandbox was stopped.
	CauseStop Cause = "stop"
	// CauseCove is a turn cove lost track of: the connection ended before the last frame.
	CauseCove Cause = "cove"
)

// Exit is how a turn ended: its cause, then the status of the agent, its exit code or the signal
// that ended it. The cause wins over the status: an agent cut by a stop ends by a signal, and is
// still a Stop.
type Exit struct {
	Cause Cause `json:"cause"`
	// Code is the exit code, nil when a signal ended the agent.
	Code *int `json:"code,omitempty"`
	// Signal is the signal that ended the agent, 0 when it exited.
	Signal int `json:"signal,omitempty"`
}

// ExecError says why the agent of a turn could not be run.
type ExecError struct {
	// Errno is the error of Linux that execve or the lookup of the program returned, 0 when the
	// init failed before that.
	Errno uint32
	// Message says it for a person.
	Message string
}

func (e *ExecError) Error() string {
	return e.Message
}

// ErrnoNotFound is ENOENT of Linux: no such program.
const ErrnoNotFound = 2

// Write writes the frame of kind k with payload p on w, in one call, so that frames written from
// several goroutines under one lock never interleave.
func Write(w io.Writer, k Kind, p []byte) error {
	if len(p) > MaxPayload {
		return fmt.Errorf("frame %d: %d bytes, more than %d", k, len(p), MaxPayload)
	}
	b := make([]byte, 5, 5+len(p))
	b[0] = byte(k)
	binary.BigEndian.PutUint32(b[1:], uint32(len(p))) //nolint:gosec // G115: bounded by MaxPayload.
	if _, err := w.Write(append(b, p...)); err != nil {
		return fmt.Errorf("write frame %d: %w", k, err)
	}
	return nil
}

// WriteJSON writes the frame of kind k with v in JSON as payload.
func WriteJSON(w io.Writer, k Kind, v any) error {
	p, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode frame %d: %w", k, err)
	}
	return Write(w, k, p)
}

// Read reads the next frame of r. It returns io.EOF when r ended between two frames, and
// io.ErrUnexpectedEOF when it ended within one.
func Read(r io.Reader) (Kind, []byte, error) {
	var h [5]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		if err == io.EOF {
			return 0, nil, io.EOF
		}
		return 0, nil, fmt.Errorf("read a frame: %w", err)
	}
	n := binary.BigEndian.Uint32(h[1:])
	if n > MaxPayload {
		return 0, nil, fmt.Errorf("frame %d: %d bytes, more than %d", h[0], n, MaxPayload)
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, fmt.Errorf("read frame %d: %w", h[0], err)
	}
	return Kind(h[0]), p, nil
}

// Decode decodes the JSON payload p of a frame into v, refusing a field v does not have.
func Decode(p []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(p))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode %T: %w", v, err)
	}
	return nil
}

// Uint32 reads the payload of a Started frame, or the errno of a Failed one.
func Uint32(p []byte) (uint32, error) {
	if len(p) < 4 {
		return 0, fmt.Errorf("a payload of %d bytes where 4 were due", len(p))
	}
	return binary.BigEndian.Uint32(p), nil
}

// PutUint32 returns the payload of a Started frame.
func PutUint32(v uint32) []byte {
	return binary.BigEndian.AppendUint32(nil, v)
}

// SizePayload returns the payload of a Resize frame.
func SizePayload(s Size) []byte {
	return binary.BigEndian.AppendUint16(binary.BigEndian.AppendUint16(nil, s.Rows), s.Cols)
}

// ParseSize reads the payload of a Resize frame.
func ParseSize(p []byte) (Size, error) {
	if len(p) != 4 {
		return Size{}, fmt.Errorf("a size of %d bytes where 4 were due", len(p))
	}
	return Size{Rows: binary.BigEndian.Uint16(p), Cols: binary.BigEndian.Uint16(p[2:])}, nil
}

// FailedPayload returns the payload of a Failed frame for e: its errno on four bytes, then its
// message.
func FailedPayload(e *ExecError) []byte {
	return append(PutUint32(e.Errno), e.Message...)
}

// ParseFailed reads the payload of a Failed frame.
func ParseFailed(p []byte) (*ExecError, error) {
	errno, err := Uint32(p)
	if err != nil {
		return nil, err
	}
	return &ExecError{Errno: errno, Message: string(p[4:])}, nil
}
