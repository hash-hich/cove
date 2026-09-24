package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// The output formats of list. Table is the default, as in docker and podman.
const (
	formatTable = "table"
	formatJSON  = "json"
)

// ListOptions are the options of list: which sandboxes to report, and how.
type ListOptions struct {
	// All adds the stopped sandboxes to the running ones.
	All bool
	// Quiet reduces the output to one ID per line, ignored by a format that has its own shape.
	Quiet bool
	// Format is formatTable or formatJSON.
	Format string
}

// listCommand parses the arguments of list and returns the process exit code. Nothing is reported:
// there is no backend to hold a sandbox, so an empty list would say something cove cannot know.
func listCommand(a *App, args []string) int {
	_, err := parseList(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, listUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove list: %v\n", err)
		printUsageError(a.Stderr, "list", listUsage)
		return ExitUsage
	}
	return notImplemented(a, "list")
}

// parseList turns the arguments of list into its options. It returns flag.ErrHelp when help was
// asked for, and an error carrying the message to show on any other usage error.
func parseList(args []string) (ListOptions, error) {
	var opts ListOptions
	fs := flag.NewFlagSet("cove list", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&opts.All, "a", false, "")
	fs.BoolVar(&opts.All, "all", false, "")
	fs.BoolVar(&opts.Quiet, "q", false, "")
	fs.BoolVar(&opts.Quiet, "quiet", false, "")
	fs.StringVar(&opts.Format, "format", formatTable, "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return opts, err
	}
	if opts.Format != formatTable && opts.Format != formatJSON {
		return opts, fmt.Errorf("--format must be %s or %s (got %q)", formatTable, formatJSON, opts.Format)
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("takes no argument, it reports every sandbox of cove (got %q)", fs.Arg(0))
	}
	return opts, nil
}

// listUsage is the help of list, in the shape of docker ps so that what developers
// already know applies. The aliases share it: the help of ls and ps is the help of list.
const listUsage = `Usage: cove list [OPTIONS]

List the sandboxes of cove, running ones by default. Only the VMs cove created
are reported: the host runs VMs that are not its own. The columns follow those
of docker ps.

Aliases: cove ls, cove ps

Options:
  -a, --all             Show the stopped sandboxes too
  -q, --quiet           Only print the sandbox IDs, one per line
      --format string   Output format, table or json (default table)

Not implemented yet: the micro-VM backend is being replaced. list validates its
arguments as described above, then exits 125 rather than printing an empty
list, which would claim that nothing runs. The exit codes below are the
contract it comes back with.

Exit codes: 0; 2 on a usage error; 125 when cove could not carry the command out.
`
