package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gitlab.com/hich-hich/cove/internal/filelock"
	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/vminit/control"
	"gitlab.com/hich-hich/cove/internal/vmm/vmmlaunch"
)

// DefaultStopTimeout is how long a stop waits for the VM to end before it kills cove-vmm: the grace
// the init leaves the processes of the image, as docker gives it, plus FlushTime.
const DefaultStopTimeout = 15 * time.Second

// FlushTime is what a stop keeps of its timeout for the init to flush and close the write disk
// once the processes ended; the rest is their grace.
const FlushTime = 5 * time.Second

// killWait bounds the wait for a cove-vmm that was sent SIGKILL.
const killWait = 5 * time.Second

// drainWait bounds the reading of the steps left on the connection of a VM that ended.
const drainWait = time.Second

// ErrStateUnknown is returned by Stop for a sandbox whose directory has no lock: nothing says
// whether its VM runs, nor names the process to stop.
var ErrStateUnknown = errors.New("its state is unknown, and cove has nothing to stop it with")

// Stopped is what a stop did.
type Stopped struct {
	// Killed is set when cove-vmm was killed rather than powered off by the init, and says why.
	Killed error
	// Steps are the steps the init reported, in order.
	Steps []control.Step
	// Removed is true when the sandbox was removed with its disk, as run --rm asked.
	Removed bool
}

// Stop stops the VM of the sandbox e of root and returns once cove-vmm has ended. The init is asked
// to stop the processes of the image and to close the write disk, and cove-vmm is killed when the
// VM has not ended within timeout, or at once when the init cannot be reached or timeout is zero.
// A VM that already ended is not an error. The sandbox is removed afterwards when run --rm marked
// it. Stop returns ErrStateUnknown for a sandbox without a lock.
func Stop(ctx context.Context, root string, e inventory.Entry, timeout time.Duration) (Stopped, error) {
	var st Stopped
	var (
		lock *filelock.Lock
		err  error
	)
	switch e.State {
	case inventory.Unknown:
		return st, ErrStateUnknown
	case inventory.Running:
		lock, err = end(ctx, e.Dir, timeout, &st)
	case inventory.Stopped:
		lock, err = inventory.Hold(ctx, e.Dir)
	}
	if err != nil {
		return st, err //nolint:wrapcheck // end and Hold name what failed.
	}
	defer lock.Release()
	if _, err := os.Stat(filepath.Join(e.Dir, removeFile)); err == nil {
		// The lock is held, so the VM cannot start again while its files go.
		if err := inventory.Remove(ctx, root, e.ID); err != nil {
			return st, err //nolint:wrapcheck // Remove names the sandbox.
		}
		st.Removed = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return st, fmt.Errorf("look for the mark of run --rm: %w", err)
	}
	return st, nil
}

// end ends the VM of the sandbox in dir, recording in st how, and returns the lock of the sandbox,
// held.
func end(ctx context.Context, dir string, timeout time.Duration, st *Stopped) (*filelock.Lock, error) {
	var (
		wait func(ended bool)
		why  error
	)
	if timeout > 0 {
		var err error
		if wait, err = ask(dir, timeout, st); err != nil {
			why = fmt.Errorf("the init could not be reached: %w", err)
		}
	} else {
		why = errors.New("stopped without waiting")
	}
	if why == nil {
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		lock, err := inventory.Hold(waitCtx, dir)
		cancel()
		wait(err == nil)
		if err == nil || ctx.Err() != nil {
			return lock, err //nolint:wrapcheck // Hold names the lock and why it gave up.
		}
		why = fmt.Errorf("the VM did not end within %s", timeout)
	}
	if err := kill(dir); err != nil {
		return nil, fmt.Errorf("%w, and cove-vmm could not be killed: %w", why, err)
	}
	st.Killed = why
	killCtx, cancel := context.WithTimeout(ctx, killWait)
	defer cancel()
	lock, err := inventory.Hold(killCtx, dir)
	if err != nil {
		return nil, fmt.Errorf("cove-vmm was killed and did not end: %w", err)
	}
	return lock, nil
}

// ask sends the init of the sandbox in dir a Stop that leaves the processes of the image timeout
// less FlushTime, and records each step it answers in st, which is not to be read before the
// returned wait returns. wait is told whether the VM ended, and then returns once the steps are
// all read, or at once when the VM did not.
func ask(dir string, timeout time.Duration, st *Stopped) (func(ended bool), error) {
	conn, err := dialControl(dir)
	if err != nil {
		return nil, err
	}
	if err := control.Send(conn, control.Stop{Grace: max(0, timeout-FlushTime)}); err != nil {
		_ = conn.Close()
		return nil, err //nolint:wrapcheck // Send names the message.
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		r := control.NewReceiver(conn)
		for {
			var step control.Step
			if r.Receive(&step) != nil {
				return
			}
			st.Steps = append(st.Steps, step)
		}
	}()
	return func(ended bool) {
		if ended {
			// cove-vmm closed its end as it exited, so the last steps are read up to the end of
			// the connection. The lock can go a moment before, the wait is bounded.
			select {
			case <-done:
			case <-time.After(drainWait):
			}
		}
		// The steps a VM that did not end has yet to send are not waited for.
		_ = conn.Close()
		<-done
	}, nil
}

// dialMu is held while cove works in the directory of a sandbox to connect to its socket.
var dialMu sync.Mutex

// dialControl connects to the socket cove-vmm listens on for the init of the sandbox in dir. A
// Unix socket takes a path of 104 bytes at most on macOS, shorter than the one of a sandbox, so the
// socket is reached by its name from dir, the working directory of cove for the time of the
// connect.
func dialControl(dir string) (net.Conn, error) {
	dialMu.Lock()
	defer dialMu.Unlock()
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("find the working directory: %w", err)
	}
	if err := os.Chdir(dir); err != nil {
		return nil, fmt.Errorf("enter the directory of the sandbox: %w", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	//nolint:noctx // A connect on a Unix socket does not wait: the listener answers or refuses.
	conn, err := net.Dial("unix", controlFile)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", controlFile, err)
	}
	return conn, nil
}

// writeProcess writes in dir what names the cove-vmm of vm, for a stop to kill it.
func writeProcess(dir string, vm *vmmlaunch.VM) error {
	p, err := vm.Process()
	if err != nil {
		return err //nolint:wrapcheck // Process names cove-vmm.
	}
	out, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode the process of cove-vmm: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, processFile), out, 0o600); err != nil {
		return fmt.Errorf("write the process of cove-vmm: %w", err)
	}
	return nil
}

// kill kills the cove-vmm of the sandbox in dir, named by its processFile.
func kill(dir string) error {
	//nolint:gosec // G304: a file of the sandbox, in its directory.
	out, err := os.ReadFile(filepath.Join(dir, processFile))
	if err != nil {
		return fmt.Errorf("find the cove-vmm to kill: %w", err)
	}
	var p vmmlaunch.Process
	if err := json.Unmarshal(out, &p); err != nil {
		return fmt.Errorf("find the cove-vmm to kill: %s: %w", processFile, err)
	}
	return p.Kill() //nolint:wrapcheck // Kill names cove-vmm.
}
