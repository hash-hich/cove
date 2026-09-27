package vmmlaunch

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// Process names a cove-vmm by its pid and the time it started. A pid is given to another program
// once its process ended; the pair names one process only, and is what a cove that did not start
// the VM kills it by.
type Process struct {
	PID int `json:"pid"`
	// Started is when the process started, in the unit the system gives it.
	Started int64 `json:"started"`
}

// Kill sends SIGKILL to p, when p still runs. A process that ended, whose pid another program may
// hold by now, is left alone, and Kill returns nil: what it was asked to end is gone.
func (p Process) Kill() error {
	started, ok, err := startTime(p.PID)
	if err != nil {
		return err
	}
	if !ok || started != p.Started {
		return nil
	}
	// The pid could still pass to another program between the look and the kill, a window of a
	// system call.
	if err := unix.Kill(p.PID, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("kill %s %d: %w", Program, p.PID, err)
	}
	return nil
}
