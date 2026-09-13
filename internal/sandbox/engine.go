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

// ErrImageMissing reports that the image of a sandbox is not in the local image store, and names no
// registry to be pulled from.
var ErrImageMissing = errors.New("sandbox image not found")

// binary is the container CLI, looked up on the PATH.
const binary = "container"

// execVerb is the verb of that CLI which runs a program in a VM that already runs.
const execVerb = "exec"

// Engine runs sandboxes through the container CLI.
type Engine struct {
	// Stdin is what the CLI reads; nil gives it an empty input, which is what every verb but an
	// attached send wants.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Preflight verifies what Run needs before anything else is spent on a sandbox: it returns
// ErrNotInstalled when the container CLI is not on the PATH, ErrImageMissing when image is not in
// the local store and names no registry, the failure of the pull when it names one and the pull
// fails, and nil otherwise. The progress of a pull goes to the engine's Stdout.
func (e *Engine) Preflight(ctx context.Context, image string) error {
	bin, err := lookPath()
	if err != nil {
		return err
	}
	if err := checkImage(ctx, bin, image); err == nil || !namesRegistry(image) {
		return err
	}
	return e.pull(ctx, bin, image)
}

// pull brings image into the local store from the registry it names, the progress of container on
// the engine's streams. Container run would pull it itself, but after the repository was fetched
// and without a word on why it takes long.
func (e *Engine) pull(ctx context.Context, bin string, image string) error {
	code, err := e.exec(ctx, bin, []string{"image", "pull", "--", image})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("pull %s: %s exited %d", image, binary, code)
	}
	return nil
}

// Run launches the sandbox described by spec, its stderr attached to the engine's, and returns once
// the VM runs. Preflight must have passed. It returns the name of the VM, which container run
// prints on stdout and which cove captures so that the caller decides when it is announced, the
// exit code of container run, and an error only when cove itself could not launch it:
// ErrNotInstalled or a failure to execute the CLI.
func (e *Engine) Run(ctx context.Context, spec Spec) (string, int, error) {
	bin, err := lookPath()
	if err != nil {
		return "", 0, err
	}
	var stdout strings.Builder
	//nolint:gosec // G204: bin comes from LookPath and the arguments from a Spec, never from a shell string.
	cmd := exec.CommandContext(ctx, bin, spec.Args()...)
	cmd.Stdout, cmd.Stderr = &stdout, e.Stderr
	code, err := run(cmd, bin)
	return strings.TrimSpace(stdout.String()), code, err
}

// exec runs the container CLI with args and the engine's streams attached. It returns the exit
// code of the CLI, and an error only when it could not be executed.
func (e *Engine) exec(ctx context.Context, bin string, args []string) (int, error) {
	//nolint:gosec // G204: bin comes from LookPath and the arguments from a Spec, never from a shell string.
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.Stdin, e.Stdout, e.Stderr
	return run(cmd, bin)
}

// run runs cmd, the container CLI at bin with its streams already set. It returns the exit code
// of the CLI, and an error only when it could not be executed.
func run(cmd *exec.Cmd, bin string) (int, error) {
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

// checkImage returns ErrImageMissing when image is not in the local store, before container run
// would look for it on docker.io and fail with an authentication error that says nothing about the
// cause. The build hint names the directory of the default image, the only one cove knows.
func checkImage(ctx context.Context, bin string, image string) error {
	var stderr strings.Builder
	//nolint:gosec // G204: bin comes from LookPath and image from the caller, behind --.
	cmd := exec.CommandContext(ctx, bin, "image", "inspect", "--", image)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		dir := "<the directory of its Dockerfile>"
		if image == Image {
			dir = "images/sandbox"
		}
		return fmt.Errorf("%w: %s (build it with: container build --platform linux/arm64 -t %s %s): %w",
			ErrImageMissing, strings.TrimSpace(stderr.String()), image, dir, err)
	}
	return nil
}
