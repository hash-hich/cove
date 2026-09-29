// Package process is what cove shares with the processes it starts from libexec, cove-vmm and
// cove-net: finding libexec, starting a process of it, the JSON messages the two sides exchange,
// the build that both sides must come from, and what names such a process for good once cove has
// ended.
//
// A process of libexec runs in a session of its own, without the environment of cove and in the
// root directory, and outlives cove. It inherits one end of a socket pair on descriptor Pipe and,
// when there is one, the lock of the sandbox on descriptor Lock, the only descriptors it inherits
// past the standard streams.
package process

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// Pipe is the descriptor of the socket a process of libexec talks to cove on, the first after the
// standard streams.
const Pipe = 3

// Lock is the descriptor of the file whose lock a process of libexec holds for cove by living: it
// never touches it, and the kernel releases the lock when the process ends.
const Lock = 4

// build names the build of cove this binary comes from, set by the linker when make builds cove
// and libexec together; empty when go builds one alone.
var build string

// Build returns the build of cove this binary comes from, empty when it was not built by make.
// cove refuses a process of libexec of another build, since the two change together.
func Build() string { return build }

// Dir returns the directory of the files cove runs a VM with: libexec beside the bin directory cove
// was started from. It is found from the executable, never from PATH, since it decides what runs
// the VM. A missing libexec is named with how to get it, since the error comes after the image was
// pulled and a bare missing file would not say why.
func Dir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find cove: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("find cove: %w", err)
	}
	dir := filepath.Join(filepath.Dir(filepath.Dir(exe)), "libexec")
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("no libexec beside cove, at %s: make build builds cove with it", dir)
	} else if err != nil {
		return "", fmt.Errorf("find libexec: %w", err)
	}
	return dir, nil
}

// Command returns the process program of dir, ready to start: in a session of its own, without the
// environment of cove, in the root directory, its standard output and error on log, pipe on Pipe
// and lock, when not nil, on Lock. It takes no argument.
//
// Not CommandContext: the end of a context must not kill a process that outlives cove.
func Command(dir, program string, log, pipe, lock *os.File) *exec.Cmd {
	//nolint:gosec,noctx // G204: a program of libexec beside cove, which takes no argument.
	cmd := exec.Command(filepath.Join(dir, program))
	cmd.Dir = "/"
	cmd.Env = []string{}
	cmd.Stdout, cmd.Stderr = log, log
	// ExtraFiles puts its first file on descriptor 3, Pipe, and the next on Lock.
	cmd.ExtraFiles = []*os.File{pipe}
	if lock != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, lock)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// Socketpair returns a pair of connected sockets that no other program started meanwhile inherits:
// cove keeps the first, and the process of libexec inherits the second. macOS has no SOCK_CLOEXEC,
// so the flag is set after, under the lock os/exec forks under.
func Socketpair() ([2]*os.File, error) {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return [2]*os.File{}, fmt.Errorf("create the pipe: %w", err)
	}
	unix.CloseOnExec(fds[0])
	unix.CloseOnExec(fds[1])
	return [2]*os.File{os.NewFile(uintptr(fds[0]), "pipe"), os.NewFile(uintptr(fds[1]), "pipe")}, nil
}

// Send writes m on w as one message.
func Send(w io.Writer, m any) error {
	if err := json.NewEncoder(w).Encode(m); err != nil {
		return fmt.Errorf("send %T: %w", m, err)
	}
	return nil
}

// Receiver reads the messages of one side, in order.
type Receiver struct {
	dec *json.Decoder
}

// NewReceiver returns a receiver of the messages r carries. An unknown field is refused: the two
// sides come from one build.
func NewReceiver(r io.Reader) *Receiver {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return &Receiver{dec: dec}
}

// Receive reads the next message into m. It returns io.EOF when the other side closed its end
// before sending it.
func (r *Receiver) Receive(m any) error {
	if err := r.dec.Decode(m); err != nil {
		// Decode returns io.EOF itself, unwrapped, when nothing came before the end.
		if err == io.EOF {
			return io.EOF
		}
		return fmt.Errorf("receive %T: %w", m, err)
	}
	return nil
}
