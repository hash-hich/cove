package vmmlaunch

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// startTime returns when the process pid started, in microseconds since the epoch, and false when
// there is no such process.
func startTime(pid int) (int64, bool, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		return 0, false, fmt.Errorf("look at process %d: %w", pid, err)
	}
	if len(procs) == 0 {
		return 0, false, nil
	}
	t := procs[0].Proc.P_starttime
	return t.Sec*1_000_000 + int64(t.Usec), true, nil
}
