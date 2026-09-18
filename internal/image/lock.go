package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// lockPoll is how often a pull that waits for a lock looks at it again. The wait is not a blocking
// flock, which the runtime restarts after a signal: cove must still answer a Ctrl-C while it waits
// for another pull.
const lockPoll = 200 * time.Millisecond

// fileLock is a lock of the system on a file of tmp/, at a path two pulls compute without knowing
// of each other. The kernel releases it when its holder dies, kill -9 included, so no lock left
// behind ever blocks the next pull.
type fileLock struct {
	f *os.File
}

// tryLock takes the lock at path without waiting. It reports false, and no error, when another
// process holds it.
func tryLock(path string) (*fileLock, bool, error) {
	//nolint:gosec // G304: path is built by the store from its root and a hex digest, never from user input.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open the lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock %s: %w", path, err)
	}
	return &fileLock{f: f}, true, nil
}

// lock takes the lock at path, waiting for as long as another process holds it: giving up would
// fail a pull that everything says is about to succeed. Only ctx ends the wait. onWait, when not
// nil, is called once, when the wait begins.
func lock(ctx context.Context, path string, onWait func()) (*fileLock, error) {
	waited := false
	for {
		l, ok, err := tryLock(path)
		if err != nil {
			return nil, err
		}
		if ok {
			return l, nil
		}
		if !waited && onWait != nil {
			onWait()
		}
		waited = true
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for %s: %w", path, context.Cause(ctx))
		case <-time.After(lockPoll):
		}
	}
}

// unlock releases the lock. The file stays: see Store.sweep for why a lock file is never removed.
func (l *fileLock) unlock() {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}
