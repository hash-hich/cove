package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// StopSpec describes a stop: the parsed command line of the verb, the VMs to stop and the options
// the user may set.
type StopSpec struct {
	// Targets are the sandboxes to stop, by name or ID: an exact name, never a prefix.
	Targets []string
	// Signal is the signal to send; empty leaves the default of the backend.
	Signal string
	// Timeout is the number of seconds to wait before killing; nil leaves the default of the
	// backend, and zero kills without waiting.
	Timeout *int
}

// stopCommand parses the arguments of stop and returns the process exit code. Nothing is stopped:
// the backend that would run the sandboxes is being replaced.
func stopCommand(a *App, args []string) int {
	_, _, err := parseStop(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, stopUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove stop: %v\n", err)
		printUsageError(a.Stderr, "stop", stopUsage)
		return ExitUsage
	}
	return notImplemented(a, "stop")
}

// parseStop turns the arguments of stop into a stop spec and whether --all was given. It returns
// flag.ErrHelp when help was asked for, and an error carrying the message to show on any other
// usage error.
func parseStop(args []string) (StopSpec, bool, error) {
	var (
		spec    StopSpec
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
	// Zero is a valid delay (kill at once), so only a flag actually given reaches the backend.
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
stopped. Only the VMs cove created are stopped: another VM of the host is
refused and left as is. A stopped VM is removed.

Options:
  -a, --all             Stop every running sandbox of cove
  -s, --signal string   Signal to send to the VM
  -t, --time int        Seconds to wait before killing the VM (default: the one of the backend)

Not implemented yet: the micro-VM backend is being replaced. stop validates its
arguments as described above, then exits 125 having stopped nothing. The exit
codes below are the contract it comes back with.

Exit codes: 0 when every target stopped; 1 when one was refused or unknown; 2
on a usage error; 125 when cove could not carry the command out.
`
