package vmmlaunch

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// startTime returns when the process pid started, in clock ticks since the boot, and false when
// there is no such process.
func startTime(pid int) (int64, bool, error) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("look at process %d: %w", pid, err)
	}
	// The name, in parentheses, may hold spaces and parentheses: the fields are read after the
	// last one, starttime the twentieth of them.
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	if len(fields) < 20 {
		return 0, false, fmt.Errorf("look at process %d: a stat of %d fields", pid, len(fields))
	}
	started, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("look at process %d: %w", pid, err)
	}
	return started, true, nil
}
