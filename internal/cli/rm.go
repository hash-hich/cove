package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// RmSpec describes a removal: the parsed command line of rm.
type RmSpec struct {
	// Targets are the sandboxes to remove, by name, ID or prefix of an ID.
	Targets []string
	// Force kills the VM of a running target, which is refused otherwise.
	Force bool
}

// rmCommand removes the sandboxes the arguments of rm name, and returns the process exit code.
func rmCommand(a *App, args []string) int {
	spec, err := parseRm(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, rmUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove rm: %v\n", err)
		printUsageError(a.Stderr, "rm", rmUsage)
		return ExitUsage
	}
	ctx, stop := notify(context.Background())
	defer stop()
	root, err := sandbox.DefaultRoot()
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove rm: %v\n", err)
		return ExitPreflight
	}
	entries, err := inventory.List(ctx, root)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove rm: %v\n", err)
		return ExitPreflight
	}
	targets, ok := resolve(a, "rm", entries, spec.Targets, false)
	for _, t := range targets {
		if ctx.Err() != nil {
			break
		}
		if err := sandbox.Remove(ctx, root, t.entry, spec.Force); err != nil {
			_, _ = fmt.Fprintf(a.Stderr, "cove rm: %s: %v\n", t.named, err)
			ok = false
			continue
		}
		_, _ = fmt.Fprintln(a.Stdout, t.named)
	}
	if sig, interrupted := errors.AsType[*signalled](context.Cause(ctx)); interrupted {
		return sig.exitCode()
	}
	if !ok {
		return exitFailed
	}
	return 0
}

// parseRm turns the arguments of rm into a removal spec. It returns flag.ErrHelp when help was asked
// for, and an error carrying the message to show on any other usage error.
func parseRm(args []string) (RmSpec, error) {
	var spec RmSpec
	fs := flag.NewFlagSet("cove rm", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&spec.Force, "f", false, "")
	fs.BoolVar(&spec.Force, "force", false, "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return spec, err
	}
	if fs.NArg() == 0 {
		return spec, errors.New("requires at least 1 argument")
	}
	spec.Targets = fs.Args()
	return spec, nil
}

// rmUsage is the help of rm, in the shape of docker rm so that what developers already know
// applies.
const rmUsage = `Usage: cove rm [OPTIONS] SANDBOX [SANDBOX...]

Remove one or more sandboxes, given by name, by ID or by a prefix of an ID that
starts no other, as list shows them, with their write disk and all cove kept of
them. Prints each sandbox once removed, as it was named. A target that fails
does not stop the others.

A sandbox whose VM runs is refused unless -f is given: its cove-vmm is then
killed at once, without asking the init to stop, since the disk goes with it.
A sandbox whose state is unknown is refused, -f or not: nothing says that no VM
writes on its disk.

Options:
  -f, --force   Kill the VM of a running sandbox, then remove it

Exit codes: 0 when every target was removed; 1 when one was unknown, running
or could not be removed; 2 on a usage error; 125 when cove could not carry the
command out; 130 on SIGINT and 143 on SIGTERM.
`
