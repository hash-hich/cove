// Package cli parses the cove command line and dispatches to the requested command.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

// ExitUsage is the exit code for a usage error, following the flag package convention.
const ExitUsage = 2

// App holds the streams a cove invocation writes to.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
}

// Run parses args and dispatches to the requested command. It returns the process exit code.
//
// Help that was asked for goes to stdout with exit code 0; usage shown after an error goes to
// stderr with a non-zero code, following the GNU convention.
func (a *App) Run(args []string) int {
	fs := flag.NewFlagSet("cove", flag.ContinueOnError)
	fs.SetOutput(a.Stderr)
	// flag prints the usage itself on any parse error, which would send -h to stderr.
	// Printing is done here instead, per branch.
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(a.Stdout, fs)
			return 0
		}
		printUsage(a.Stderr, fs)
		return ExitUsage
	}

	if fs.NArg() == 0 {
		printUsage(a.Stderr, fs)
		return ExitUsage
	}

	switch cmd := fs.Arg(0); cmd {
	case "help":
		printUsage(a.Stdout, fs)
		return 0
	default:
		_, _ = fmt.Fprintf(a.Stderr, "unknown command %q\n", cmd)
		printUsage(a.Stderr, fs)
		return ExitUsage
	}
}

// printUsage writes the usage text and the flag defaults of fs to w.
func printUsage(w io.Writer, fs *flag.FlagSet) {
	_, _ = fmt.Fprintln(w, "Usage: cove <command> [flags]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Commands:")
	_, _ = fmt.Fprintln(w, "  help    Show help")
	fs.SetOutput(w)
	fs.PrintDefaults()
}
