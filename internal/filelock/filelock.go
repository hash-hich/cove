// Package filelock takes the locks of the system cove uses to keep two of its processes off one
// piece of its cache. A lock lives on a file at a path they compute alone, without knowing of
// each other, and the kernel releases it when its holder dies, kill -9 included, so no lock left
// behind ever blocks the next run.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// Poll is how often a process that waits for a lock looks at it again. The wait is not a blocking
// flock, which the runtime restarts after a signal: cove must still answer a Ctrl-C while it
// waits for another process.
const Poll = 200 * time.Millisecond

// Lock is a lock held on a file. Its holder releases it with Release, and the kernel releases it
// if the holder dies first.
type Lock struct {
	f *os.File
}

// Try takes the lock at path without waiting. It reports false, and no error, when another
// process holds it.
func Try(path string) (*Lock, bool, error) {
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
	return &Lock{f: f}, true, nil
}

// Take takes the lock at path, waiting for as long as another process holds it: giving up would
// fail work that everything says is about to succeed. Only ctx ends the wait. onWait, when not
// nil, is called once, when the wait begins.
func Take(ctx context.Context, path string, onWait func()) (*Lock, error) {
	waited := false
	for {
		l, ok, err := Try(path)
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
		case <-time.After(Poll):
		}
	}
}

// Release releases the lock. The file stays: a lock file removed between the look of a sweep and
// the flock of another process would let a third recreate the name and lock another inode, and
// the two would then hold what they each believe is one lock.
func (l *Lock) Release() {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}
