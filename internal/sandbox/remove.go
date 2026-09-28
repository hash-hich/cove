package sandbox

import (
	"context"
	"errors"

	"gitlab.com/hich-hich/cove/internal/inventory"
)

// ErrRunning is returned by Remove for a sandbox whose VM runs, when the removal is not forced.
var ErrRunning = errors.New("its VM is running: stop it first, or force the removal")

// Remove removes the sandbox e of root, its write disk included. The lock of the sandbox says
// whether its VM runs, at the moment of the removal: Remove returns ErrRunning when it is held,
// unless force, which kills cove-vmm at once first. Remove returns ErrStateUnknown for a sandbox
// without a lock, since nothing says that no VM writes on its disk.
func Remove(ctx context.Context, root string, e inventory.Entry, force bool) error {
	if e.State == inventory.Unknown {
		return ErrStateUnknown
	}
	lock, ok, err := inventory.TryHold(e.Dir)
	if err != nil {
		return err //nolint:wrapcheck // TryHold names the lock.
	}
	if !ok {
		if !force {
			return ErrRunning
		}
		// The disk goes with the sandbox, so nothing the init would flush is kept: the VM is
		// killed without asking it, as docker rm -f kills a container.
		if lock, err = end(ctx, e.Dir, 0, &Stopped{}); err != nil {
			return err
		}
	}
	defer lock.Release()
	// The lock is held, so no VM starts in the sandbox while its files go.
	return inventory.Remove(ctx, root, e.ID) //nolint:wrapcheck // Remove names the sandbox.
}
