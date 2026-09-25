//go:build !(darwin && cgo)

package confine

import (
	"errors"
	"runtime"
)

// Enter refuses: no confinement is written for this platform yet, and a monitor is never run
// without one.
func Enter() error {
	return errors.New("cove-vmm has no confinement on " + runtime.GOOS + " yet, and runs no VM without one")
}
