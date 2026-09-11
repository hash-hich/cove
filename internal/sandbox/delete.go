package sandbox

import (
	"context"
	"fmt"
)

// deleteArgs returns the container delete argument array that removes target, running or not.
// --force is what makes a running VM go: container refuses it otherwise (D10).
func deleteArgs(target string) []string {
	return []string{"delete", "--force", target}
}

// Delete removes the sandbox target, by name or ID, whether it runs or not, with the engine's
// streams attached. It is the way back from a sandbox that could not be seeded: without its
// repository it is not a sandbox to inspect, --keep or not. It returns ErrNotInstalled, a failure
// to execute the CLI, or the exit code of container delete when it is not zero.
func (e *Engine) Delete(ctx context.Context, target string) error {
	bin, err := lookPath()
	if err != nil {
		return err
	}
	code, err := e.exec(ctx, bin, deleteArgs(target))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("delete %s: %s exited %d", target, binary, code)
	}
	return nil
}
