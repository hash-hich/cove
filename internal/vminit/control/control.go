// Package control is how the host drives the init of a VM, apart from the streams of the turns: the
// port the init listens on, and the messages that go through it. The host sends one Request. For a
// Stop, the init answers with a Step as it reaches each one, then powers the VM off, which ends the
// connection. For a Cancel, it answers Received, with an error when no turn bears the ID, and
// closes. The init and the host come from one build, so an unknown field is refused.
//
// The steps are what tells the host an init at work from one that got nothing: a stop that has to
// be forced says how far the init had gone.
package control

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Port is the vsock port the init listens on for the host.
const Port = 1024

// Request is what the host asks the init, one of its fields set.
type Request struct {
	Stop   *Stop   `json:"stop,omitempty"`
	Cancel *Cancel `json:"cancel,omitempty"`
}

// Cancel asks the init to cut a turn: its agent is hung up, as a terminal that closes hangs up a
// shell, and its session killed once Grace has passed. The turn still ends with its Exited frame,
// Cause as its cause.
type Cancel struct {
	// Turn is the ID of the turn, as its Request gave it.
	Turn string `json:"turn"`
	// Cause is turn.CauseCancel or turn.CauseTimeout.
	Cause string `json:"cause"`
	// Grace is how long the processes of the turn have to end before SIGKILL.
	Grace time.Duration `json:"grace"`
}

// Stop asks the init to stop the VM: every process of the image is sent its stop signal, killed
// once Grace has passed, then the write disk is flushed and made read only, and the VM powered off.
type Stop struct {
	// Grace is how long the processes have to end after their stop signal, before SIGKILL.
	Grace time.Duration `json:"grace"`
}

// Step is a step of a stop, or the answer to a Cancel, the init has reached, or failed to reach.
type Step struct {
	// Name is one of the steps below.
	Name string `json:"name"`
	// Error says why the step failed; the init goes on to the next one all the same, since a VM
	// that powers off with its disk flushed is still worth more than one that is killed.
	Error string `json:"error,omitempty"`
}

// The steps of a stop, in their order.
const (
	// Received is the Request read.
	Received = "received"
	// ProcessesEnded is every process of the image ended, by its stop signal or by SIGKILL.
	ProcessesEnded = "processes ended"
	// Synced is what the processes wrote flushed to the disks.
	Synced = "synced"
	// ReadOnly is the write disk made read only, its journal closed.
	ReadOnly = "write disk read only"
)

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

// NewReceiver returns a receiver of the messages r carries.
func NewReceiver(r io.Reader) *Receiver {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return &Receiver{dec: dec}
}

// Receive reads the next message into m. It returns io.EOF when the other side closed its end
// before sending it, which is how the host sees the VM power off.
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
