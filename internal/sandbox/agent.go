package sandbox

import (
	"context"
	"errors"
	"fmt"
)

// ErrAgentMissing reports that the image of a sandbox does not carry the agent.
var ErrAgentMissing = errors.New("the image does not carry the agent")

// agentArgs returns the container exec argument array that asks the agent of target for its
// version: the program itself, found on the PATH of the image as a send would find it, and the
// cheapest thing it does (measured at 0.4 s).
func agentArgs(target string) []string {
	return []string{execVerb, target, agent, "--version"}
}

// CheckAgent verifies that the sandbox target carries the agent, right after its creation and
// before anything is put in it. It is the one thing checked of an image: the rules a profile must
// keep are documented, not controlled. The version the agent prints goes to the engine's Stdout.
// It returns ErrAgentMissing when the exec fails, ErrNotInstalled or a failure to execute the CLI
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
		return fmt.Errorf("%w: %s --version exited %d", ErrAgentMissing, agent, code)
	}
	return nil
}
