//go:build !(darwin && cgo)

package confine

import (
	"errors"
	"runtime"
)

// Enter refuses: no confinement is written for this platform yet, and a process of libexec never
// runs without one.
func Enter(string, Paths) error {
	return errors.New("no confinement is written for " + runtime.GOOS + " yet, and nothing of libexec runs without one")
}
