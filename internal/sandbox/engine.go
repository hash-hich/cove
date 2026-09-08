package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ErrNotInstalled reports that the container CLI could not be found.
var ErrNotInstalled = errors.New("container CLI not found")

// ErrImageMissing reports that Image is not in the local image store.
var ErrImageMissing = errors.New("sandbox image not found")

// binary is the container CLI, looked up on the PATH.
const binary = "container"

// Engine runs sandboxes through the container CLI.
type Engine struct {
	Stdout io.Writer
	Stderr io.Writer
}

// Run launches the sandbox described by spec with the engine's streams attached, and returns once
// the VM runs. It returns the exit code of container run, whose stdout carries the VM name, and an
// error only when cove itself could not launch it: ErrNotInstalled, ErrImageMissing, or a failure
// to execute the CLI.
func (e *Engine) Run(ctx context.Context, spec Spec) (int, error) {
	bin, err := lookPath()
	if err != nil {
		return 0, err
	}
	if err := checkImage(ctx, bin); err != nil {
		return 0, err
	}
	return e.exec(ctx, bin, spec.Args())
}

// exec runs the container CLI with args and the engine's streams attached. It returns the exit
// code of the CLI, and an error only when it could not be executed.
func (e *Engine) exec(ctx context.Context, bin string, args []string) (int, error) {
	//nolint:gosec // G204: bin comes from LookPath and the arguments from a Spec, never from a shell string.
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = e.Stdout, e.Stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() >= 0 {
		return exitErr.ExitCode(), nil
	}
	return 0, fmt.Errorf("run %s: %w", bin, err)
}

// lookPath finds the container CLI on the PATH, or returns ErrNotInstalled.
func lookPath() (string, error) {
	bin, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNotInstalled, err)
	}
	return bin, nil
}

// checkImage fails before container run would: without the image in the local store, run queries
// docker.io and fails with an authentication error that says nothing about the cause (D10).
func checkImage(ctx context.Context, bin string) error {
	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, bin, "image", "inspect", Image)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s (build it with: container build --platform linux/arm64 -t %s images/sandbox): %w",
			ErrImageMissing, strings.TrimSpace(stderr.String()), Image, err)
	}
	return nil
}
