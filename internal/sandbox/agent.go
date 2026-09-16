package sandbox

import (
	"context"
	"errors"
	"fmt"
)

// ErrAgentCheck reports that the agent of a sandbox did not answer its version check. The exit
// code of container exec cannot tell an image without the agent from a failure of the CLI itself
// (both are 1, docs/decisions.md), so the error states what was observed, and the stderr of
// container, relayed right above it, says which it was.
var ErrAgentCheck = errors.New("the agent did not answer")

// agentArgs returns the container exec argument array that asks the agent of target for its
// version: the program itself, found on the PATH of the image as a send would find it, and the
// cheapest thing it does (measured at 0.4 s).
func agentArgs(target string) []string {
	return []string{execVerb, target, agent, "--version"}
}

// CheckAgent verifies that the sandbox target carries the agent, right after its creation and
// before anything is put in it. It is the one thing checked of an image: the rules a profile must
// keep are documented, not controlled. The version the agent prints goes to the engine's Stdout.
// It returns ErrAgentCheck when the exec fails, ErrNotInstalled or a failure to execute the CLI
// otherwise.
func (e *Engine) CheckAgent(ctx context.Context, target string) error {
	bin, err := lookPath()
	if err != nil {
		return err
	}
	code, err := e.exec(ctx, bin, agentArgs(target))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("%w: %s --version exited %d", ErrAgentCheck, agent, code)
	}
	return nil
}
