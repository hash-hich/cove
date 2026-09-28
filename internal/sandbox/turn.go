package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/vminit/control"
	"gitlab.com/hich-hich/cove/internal/vminit/turn"
)

// ErrNotRunning is returned for a turn sent to a sandbox whose VM does not run: cove never starts
// one for the occasion.
var ErrNotRunning = errors.New("the sandbox is not running")

// chunk is the most a Stdin frame carries.
const chunk = 32 << 10

// Streams are the streams of a turn on the host.
type Streams struct {
	// Stdin is what the agent reads, until its end; nil gives it an end at once.
	Stdin io.Reader
	// Stdout and Stderr receive what the agent writes, as it comes. An agent on a terminal
	// writes only Stdout.
	Stdout, Stderr io.Writer
	// Resize carries the sizes the terminal of the agent takes after the first, nil for none.
	Resize <-chan turn.Size
	// Started, when set, is called once the agent started: from then on, a Cancel finds the turn.
	Started func()
}

func (s Streams) started() {
	if s.Started != nil {
		s.Started()
	}
}

// Turn runs req in the sandbox e and relays its streams to s until the turn ended, then returns
// how, as the init reported it. It returns a *turn.ExecError when the agent could not be run, and
// ErrNotRunning for a sandbox whose VM does not run. When ctx is done, the turn is abandoned: its
// connection is closed, which hangs it up in the guest, and Turn returns the cause of ctx. Any
// other error means the turn ended without its end reaching cove.
func Turn(ctx context.Context, e inventory.Entry, req turn.Request, s Streams) (turn.Exit, error) {
	if e.State != inventory.Running {
		return turn.Exit{}, ErrNotRunning
	}
	conn, err := dial(e.Dir, turnFile)
	if err != nil {
		return turn.Exit{}, err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	w := &frameWriter{conn: conn}
	if err := w.writeJSON(turn.Exec, req); err != nil {
		return turn.Exit{}, abandoned(ctx, err)
	}
	done := make(chan struct{})
	defer close(done)
	go w.stdin(s.Stdin)
	go w.resize(s.Resize, done)
	exit, err := readTurn(conn, s)
	return exit, abandoned(ctx, err)
}

// abandoned returns the cause of ctx when it is done, err otherwise: a turn abandoned fails to
// read or write its connection, closed under it.
func abandoned(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil {
		return context.Cause(ctx) //nolint:wrapcheck // The cause is the caller's own, to be matched as is.
	}
	return err
}

// readTurn reads the frames of a turn on conn, writes its streams to s, and returns its end.
func readTurn(conn net.Conn, s Streams) (turn.Exit, error) {
	for {
		k, p, err := turn.Read(conn)
		if errors.Is(err, io.EOF) {
			return turn.Exit{}, errors.New("the connection to the turn ended before the turn did")
		}
		if err != nil {
			return turn.Exit{}, err //nolint:wrapcheck // Read names the frame.
		}
		//exhaustive:ignore // The frames the host sends never come from the init.
		switch k {
		case turn.Started:
			s.started()
		case turn.Stdout:
			// A caller that stopped reading loses what follows; the turn goes on.
			_, _ = s.Stdout.Write(p)
		case turn.Stderr:
			_, _ = s.Stderr.Write(p)
		case turn.Exited:
			var exit turn.Exit
			return exit, turn.Decode(p, &exit) //nolint:wrapcheck // Decode names the frame.
		case turn.Failed:
			e, err := turn.ParseFailed(p)
			if err != nil {
				return turn.Exit{}, err //nolint:wrapcheck // ParseFailed names the payload.
			}
			return turn.Exit{Cause: turn.CauseExec}, e
		default:
			return turn.Exit{}, fmt.Errorf("frame %d from the init, unknown", k)
		}
	}
}

// frameWriter writes the frames of the host on the connection of a turn, one at a time.
type frameWriter struct {
	conn io.Writer
	mu   sync.Mutex
}

func (w *frameWriter) write(k turn.Kind, p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return turn.Write(w.conn, k, p) //nolint:wrapcheck // Write names the frame.
}

func (w *frameWriter) writeJSON(k turn.Kind, v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return turn.WriteJSON(w.conn, k, v) //nolint:wrapcheck // WriteJSON names the frame.
}

// stdin sends r to the agent, then its end. A connection closed under it ends it, as the turn did.
func (w *frameWriter) stdin(r io.Reader) {
	if r != nil {
		buf := make([]byte, chunk)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if w.write(turn.Stdin, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				break
			}
		}
	}
	_ = w.write(turn.StdinEOF, nil)
}

// resize sends each size of sizes until done is closed.
func (w *frameWriter) resize(sizes <-chan turn.Size, done <-chan struct{}) {
	for {
		select {
		case size := <-sizes:
			if w.write(turn.Resize, turn.SizePayload(size)) != nil {
				return
			}
		case <-done:
			return
		}
	}
}

// Cancel asks the init of the sandbox e to cut the turn id for cause, turn.CauseCancel or
// turn.CauseTimeout, its processes given grace. The turn still ends with its Exited frame, which
// Turn returns.
func Cancel(e inventory.Entry, id string, cause turn.Cause, grace time.Duration) error {
	conn, err := dial(e.Dir, controlFile)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	req := control.Request{Cancel: &control.Cancel{Turn: id, Cause: string(cause), Grace: grace}}
	if err := control.Send(conn, req); err != nil {
		return err //nolint:wrapcheck // Send names the message.
	}
	var step control.Step
	if err := control.NewReceiver(conn).Receive(&step); err != nil {
		return fmt.Errorf("the init did not answer the cancel: %w", err)
	}
	if step.Error != "" {
		return fmt.Errorf("the init refused the cancel: %s", step.Error)
	}
	return nil
}
