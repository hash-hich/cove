package sandbox

import (
	"context"
	"strconv"
)

// StopSpec describes a stop: the VMs to stop and the options the user may set. The targets must
// already be screened (Screen or Running): container stop takes any ID and stops it.
type StopSpec struct {
	// Targets are the IDs to stop, resolved by container: an exact name, never a prefix.
	Targets []string
	// Signal is the signal to send; empty leaves the container default.
	Signal string
	// Timeout is the number of seconds container waits before killing; nil leaves its default
	// (5, against 10 for docker and podman), and zero kills without waiting.
	Timeout *int
}

// Args returns the container stop argument array for s. --all is never emitted: it would stop the
// VMs of the store that are not cove's, so an "all" is resolved into Targets beforehand.
func (s StopSpec) Args() []string {
	args := []string{"stop"}
	if s.Signal != "" {
		args = append(args, "--signal", s.Signal)
	}
	if s.Timeout != nil {
		args = append(args, "--time", strconv.Itoa(*s.Timeout))
	}
	return append(args, s.Targets...)
}

// Stop stops the VMs described by spec with the engine's streams attached. It returns the exit
// code of container stop, whose stdout carries the ID of each VM stopped and whose stderr reports
// each target it could not stop (observed on 1.3.1: it goes on past an unknown ID and exits 1),
// and an error only when cove itself could not run it: ErrNotInstalled or a failure to execute
// the CLI.
func (e *Engine) Stop(ctx context.Context, spec StopSpec) (int, error) {
	bin, err := lookPath()
	if err != nil {
		return 0, err
	}
	return e.exec(ctx, bin, spec.Args())
}
