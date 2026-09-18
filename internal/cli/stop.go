package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// exitRefused is the exit code when at least one target was refused or could not be stopped, as
// docker's 1; container's own failures already use it.
const exitRefused = 1

// stopCommand stops sandboxes and returns the process exit code.
func stopCommand(a *App, args []string) int {
	spec, all, err := parseStop(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, stopUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove stop: %v\n", err)
		printUsageError(a.Stderr, "stop", stopUsage)
		return ExitUsage
	}

	ctx := context.Background()
	vms, err := sandbox.List(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove stop: %v\n", err)
		return ExitPreflight
	}
	targets := screen(a, vms, spec.Targets, all)
	spec.Targets = targets.Kept

	code := 0
	if len(spec.Targets) > 0 {
		engine := &sandbox.Engine{Stdout: a.Stdout, Stderr: a.Stderr}
		code, err = engine.Stop(ctx, spec)
		if err != nil {
			_, _ = fmt.Fprintf(a.Stderr, "cove stop: %v\n", err)
			return ExitPreflight
		}
	}
	if code == 0 && len(targets.Refused) > 0 {
		return exitRefused
	}
	return code
}

// screen resolves the targets of stop against the store: the running sandboxes for an all, else
// the names given minus the VMs that are not cove's, each reported on stderr. Cove never stops a
// VM it did not launch.
func screen(a *App, vms []sandbox.VM, names []string, all bool) sandbox.Screening {
	if all {
		return sandbox.Screening{Kept: sandbox.Running(vms)}
	}
	s := sandbox.Screen(vms, names)
	for _, name := range s.Refused {
		_, _ = fmt.Fprintf(a.Stderr, "cove stop: %s is not a cove sandbox\n", name)
	}
	return s
}

// parseStop turns the arguments of stop into a stop spec and whether every sandbox was asked for.
// It returns flag.ErrHelp when help was asked for, and an error carrying the message to show on
// any other usage error.
func parseStop(args []string) (sandbox.StopSpec, bool, error) {
	var (
		spec    sandbox.StopSpec
		all     bool
		timeout int
	)
	fs := flag.NewFlagSet("cove stop", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&all, "a", false, "")
	fs.BoolVar(&all, "all", false, "")
	fs.StringVar(&spec.Signal, "s", "", "")
	fs.StringVar(&spec.Signal, "signal", "", "")
	fs.IntVar(&timeout, "t", 0, "")
	fs.IntVar(&timeout, "time", 0, "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return spec, false, err
	}
	// Zero is a valid delay (kill at once), so only a flag actually given reaches container.
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "t" || f.Name == "time" {
			spec.Timeout = &timeout
		}
	})
	if timeout < 0 {
		return spec, false, errors.New("--time must be positive")
	}
	if all && fs.NArg() > 0 {
		return spec, false, fmt.Errorf("--all takes no target (got %q)", fs.Arg(0))
	}
	if all {
		return spec, true, nil
	}
	if fs.NArg() == 0 {
		return spec, false, errors.New("requires at least 1 argument")
	}
	spec.Targets = fs.Args()
	return spec, false, nil
}

// stopUsage is the help of stop, in the shape of docker stop so that what
// developers already know applies.
const stopUsage = `Usage: cove stop [OPTIONS] SANDBOX [SANDBOX...]
       cove stop --all

Stop one or more sandboxes, given by name or ID. Prints the ID of each VM
stopped. Only the VMs cove created are stopped: another VM of the store is
refused and left as is. A stopped VM is removed unless run kept it (--keep).

Options:
  -a, --all             Stop every running sandbox of cove
  -s, --signal string   Signal to send to the VM
  -t, --time int        Seconds to wait before killing the VM (default: the one of container, 5)

Exit codes: 0 when every target stopped; 1 when one was refused or unknown; 2
on a usage error; 125 when cove could not run container.
`
