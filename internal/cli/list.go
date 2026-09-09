package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// The output formats of list. Table is the default, as in container and docker.
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

// listCommand reports the sandboxes of cove and returns the process exit code.
func listCommand(a *App, args []string) int {
	opts, err := parseList(args)
	if errors.Is(err, flag.ErrHelp) {
		printListUsage(a.Stdout)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove list: %v\n", err)
		printHelpHint(a.Stderr, "list")
		return ExitUsage
	}

	vms, err := sandbox.List(context.Background())
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove list: %v\n", err)
		return ExitPreflight
	}
	// The other VMs of the store are never reported, whatever --all says: cove lists what it
	// launched, and nothing tells the user about Apple's builder.
	sandboxes := sandbox.Sandboxes(vms, opts.All)

	switch {
	case opts.Format == formatJSON:
		sandbox.WriteJSON(a.Stdout, sandboxes)
	case opts.Quiet:
		sandbox.WriteIDs(a.Stdout, sandboxes)
	default:
		sandbox.WriteTable(a.Stdout, sandboxes)
	}
	return 0
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

// printListUsage writes the usage of list to w, in the shape of docker ps so that what developers
// already know applies. The aliases share it: the help of ls and ps is the help of list.
func printListUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage: cove list [OPTIONS]

List the sandboxes of cove, running ones by default. Only the VMs cove created
are reported: the store is shared with VMs that are not its own. The columns are
those of container list.

Aliases: cove ls, cove ps

Options:
  -a, --all             Show the stopped sandboxes too, those a run kept (--keep)
  -q, --quiet           Only print the sandbox IDs, one per line
      --format string   Output format, table or json (default table)

Exit codes: 0; 2 on a usage error; 125 when cove could not run container.
`)
}
