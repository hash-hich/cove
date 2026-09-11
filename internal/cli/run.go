package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// ExitPreflight is the exit code when cove itself could not run container, as the 125 of docker
// run; container never returns it (every failure of its CLI is 1, D10).
const ExitPreflight = 125

// envFlag accumulates the values of a repeatable -e flag.
type envFlag []string

func (e *envFlag) String() string { return strings.Join(*e, ",") }

func (e *envFlag) Set(value string) error {
	*e = append(*e, value)
	return nil
}

// runCommand creates a sandbox and returns the process exit code.
func runCommand(a *App, args []string) int {
	spec, err := parseRun(args)
	if errors.Is(err, flag.ErrHelp) {
		printRunUsage(a.Stdout)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove run: %v\n", err)
		printHelpHint(a.Stderr, "run")
		return ExitUsage
	}

	ctx := context.Background()
	if err := sandbox.Preflight(ctx); err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove run: %v\n", err)
		return ExitPreflight
	}
	engine := &sandbox.Engine{Stdout: a.Stdout, Stderr: a.Stderr}
	// No signal handling on purpose: a signal to cove leaves the VM running (D10); stopping it is
	// an explicit verb.
	name, code, err := engine.Run(ctx, spec)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove run: %v\n", err)
		return ExitPreflight
	}
	if code != 0 {
		return code
	}
	_, _ = fmt.Fprintln(a.Stdout, name)
	return 0
}

// parseRun turns the arguments of run into a sandbox spec. It returns flag.ErrHelp when help was
// asked for, and an error carrying the message to show on any other usage error.
func parseRun(args []string) (sandbox.Spec, error) {
	var (
		spec     sandbox.Spec
		rm, keep bool
		env      envFlag
	)
	fs := flag.NewFlagSet("cove run", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&spec.Name, "name", "", "")
	fs.BoolVar(&rm, "rm", false, "")
	fs.BoolVar(&keep, "keep", false, "")
	fs.IntVar(&spec.CPUs, "cpus", 0, "")
	fs.StringVar(&spec.Memory, "m", "", "")
	fs.StringVar(&spec.Memory, "memory", "", "")
	fs.Var(&env, "e", "")
	fs.Var(&env, "env", "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return spec, err
	}
	if rm && keep {
		return spec, errors.New("--rm and --keep are mutually exclusive")
	}
	if spec.CPUs < 0 {
		return spec, errors.New("--cpus must be positive")
	}
	if fs.NArg() > 0 {
		return spec, fmt.Errorf("takes no command, the sandbox only waits for instructions (got %q)", fs.Arg(0))
	}
	spec.Keep = keep
	spec.Env = env
	return spec, nil
}

// printRunUsage writes the usage of run to w, in the shape of docker run so that what developers
// already know applies.
func printRunUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage: cove run [OPTIONS]

Create a sandbox: a micro-VM from the `+sandbox.Image+` image, started detached
and kept alive until it is stopped. Prints its name. Every instruction to the
agent is a separate command.

Options:
      --name string     Assign a name to the VM; container picks one otherwise
      --rm              Remove the VM when it stops (default)
      --keep            Keep the stopped VM for inspection instead
      --cpus int        Number of CPUs
  -m, --memory string   Memory limit with a suffix, e.g. 512M or 4G
  -e, --env list        Set environment variables, KEY=VALUE or KEY to inherit from the host

Exit codes: 0 once the VM runs, else the one of container; 2 on a usage error;
125 when cove could not launch it.
`)
}
