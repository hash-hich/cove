package sandbox

import (
	"context"

	"gitlab.com/hich-hich/cove/internal/agent"
)

// Send drives the agent of a sandbox with the engine's streams attached. It returns the exit code
// of container exec, and an error only when cove itself could not run it: ErrNotInstalled or a
// failure to execute the CLI.
//
// Nothing is neutralized on the way out: the driven regime carries the JSON of claude, which
// escapes the control characters itself (RFC 8259), and the attached regime is a PTY a human is
// watching. A sandbox that does not run is not started for the occasion: container exec refuses it,
// and cove hands that refusal over as it comes.
func (e *Engine) Send(ctx context.Context, turn agent.Turn) (int, error) {
	bin, err := lookPath()
	if err != nil {
		return 0, err
	}
	return e.exec(ctx, bin, sendArgs(turn))
}

// sendArgs carries the command line of the agent into the VM: container exec reaches any VM of the
// store, and gives a terminal to the attached regime, the one the agent reads a human on.
func sendArgs(turn agent.Turn) []string {
	args := []string{execVerb}
	if turn.Prompt == "" {
		args = append(args, "--interactive", "--tty")
	}
	args = append(args, turn.Target)
	return append(args, turn.Args()...)
}
