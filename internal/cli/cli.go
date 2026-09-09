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

// App holds the streams of a cove invocation.
type App struct {
	// Stdin is only read by an attached send, which hands it to the terminal of the agent.
	Stdin  io.Reader
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
	// ls and ps are the names docker and container gave the same verb; both reach list, whose
	// help and errors carry the canonical name.
	case "list", "ls", "ps":
		return listCommand(a, fs.Args()[1:])
	case "run":
		return runCommand(a, fs.Args()[1:])
	case "send":
		return sendCommand(a, fs.Args()[1:])
	case "stop":
		return stopCommand(a, fs.Args()[1:])
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
	_, _ = fmt.Fprintln(w, "  list    List sandboxes (aliases: ls, ps)")
	_, _ = fmt.Fprintln(w, "  run     Create a sandbox")
	_, _ = fmt.Fprintln(w, "  send    Talk to the agent of a sandbox")
	_, _ = fmt.Fprintln(w, "  stop    Stop sandboxes")
	fs.SetOutput(w)
	fs.PrintDefaults()
}
