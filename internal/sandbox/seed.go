package sandbox

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"gitlab.com/hich-hich/cove/internal/codebase"
)

// Seed runs the steps of spec in the sandbox, the bundle on the stdin of the one that copies it.
// It stops at the first step that fails and returns its command and exit code; the message of git
// or container is on Stderr. It returns ErrNotInstalled or a failure to execute the CLI otherwise.
func (e *Engine) Seed(ctx context.Context, spec codebase.SeedSpec, bundle io.Reader) error {
	bin, err := lookPath()
	if err != nil {
		return err
	}
	for _, step := range spec.Steps() {
		args := stepArgs(spec.Target, step)
		//nolint:gosec // G204: bin comes from LookPath and the arguments from a SeedSpec, never from a shell string.
		cmd := exec.CommandContext(ctx, bin, args...)
		if step.Bundle {
			cmd.Stdin = bundle
		}
		// The stdout of run is the name of the VM and nothing else, so whatever a step prints goes
		// with the progress.
		cmd.Stdout, cmd.Stderr = e.Stderr, e.Stderr
		code, err := run(cmd, bin)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("seed %s: %s %s: exit status %d", codebase.Work, binary, strings.Join(args, " "), code)
		}
	}
	return nil
}

// stepArgs carries one step of the seeding into the VM: container exec reaches any VM of the store,
// and the step that reads the bundle needs its stdin held open.
func stepArgs(target string, step codebase.Step) []string {
	args := []string{execVerb}
	if step.Bundle {
		args = append(args, "--interactive")
	}
	args = append(args, target)
	return append(args, step.Args...)
}
