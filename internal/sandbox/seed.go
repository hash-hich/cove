package sandbox

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
)

// Work is the directory of the repository in the sandbox, owned by the user of the image.
const Work = "/work"

// bundlePath is where the bundle lands in the VM for the time of the fetch. It doubles the disk
// space of the repository until it is removed; the agent does not run yet, so nothing races it.
const bundlePath = "/tmp/cove.bundle"

// SeedSpec describes the seeding of Work in a sandbox that was just created: the bundle of the
// receiver becomes a repository on Branch, with every branch and tag of the forge as local ones and
// no remote, and the identity the agent commits under.
type SeedSpec struct {
	// Target is the sandbox to seed, by name or ID, resolved by container.
	Target string
	// Branch is the branch the agent starts on; it must be in the bundle.
	Branch string
	// Author and Email are the identity written to the config of the repository: without one git
	// refuses to commit at all. They live in Work, not in the image nor in $HOME.
	Author string
	Email  string
}

// Step is one container exec of the seeding.
type Step struct {
	// Args is the argument array of the exec.
	Args []string
	// Bundle tells whether the step reads the bundle on its stdin.
	Bundle bool
}

// Steps returns the container execs that seed Work, in order, each a fixed program with fixed
// arguments: no shell and no user override, the exec runs as the user of the image (uid 1000).
//
// The repository is initialized on Branch, so HEAD names it, unborn, before the fetch; the fetch
// refuses to write the branch HEAD names, even unborn, unless --update-head-ok (measured), and it
// leaves the index and the tree empty, which reset --hard fills from HEAD, the documented meaning
// of the verb. The identity refspecs copy every branch and tag under its own name: the repository
// is new, nothing is there to collide with. Nothing sets a remote: a push fails for lack of a
// destination, without asking for a credential.
func (s SeedSpec) Steps() []Step {
	git := []string{execVerb, s.Target, "git", "-C", Work}
	return []Step{
		{Args: append(slices.Clone(git), "init", "-q", "-b", s.Branch)},
		{Args: []string{execVerb, "--interactive", s.Target, "cp", "/dev/stdin", bundlePath}, Bundle: true},
		{Args: append(slices.Clone(git), "fetch", "-q", "--update-head-ok", "--no-write-fetch-head", bundlePath,
			"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")},
		{Args: []string{execVerb, s.Target, "rm", bundlePath}},
		{Args: append(slices.Clone(git), "reset", "-q", "--hard")},
		{Args: append(slices.Clone(git), "config", "user.name", s.Author)},
		{Args: append(slices.Clone(git), "config", "user.email", s.Email)},
	}
}

// Seed runs the steps of spec in the sandbox, the bundle on the stdin of the one that copies it.
// It stops at the first step that fails and returns its command and exit code; the message of git
// or container is on Stderr. It returns ErrNotInstalled or a failure to execute the CLI otherwise.
func (e *Engine) Seed(ctx context.Context, spec SeedSpec, bundle io.Reader) error {
	bin, err := lookPath()
	if err != nil {
		return err
	}
	for _, step := range spec.Steps() {
		//nolint:gosec // G204: bin comes from LookPath and the arguments from a SeedSpec, never from a shell string.
		cmd := exec.CommandContext(ctx, bin, step.Args...)
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
			return fmt.Errorf("seed %s: %s %s: exit status %d", Work, binary, strings.Join(step.Args, " "), code)
		}
	}
	return nil
}
