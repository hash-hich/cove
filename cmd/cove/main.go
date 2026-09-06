// Command cove runs a coding agent unsupervised in a micro-VM.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// exitUsage is the exit code for a usage error, following the flag package convention.
const exitUsage = 2

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses args and dispatches to the requested command. It returns the process exit code.
//
// Help that was asked for goes to stdout with exit code 0; usage shown after an error goes to
// stderr with a non-zero code, following the GNU convention.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	// flag prints the usage itself on any parse error, which would send -h to stderr.
	// Printing is done here instead, per branch.
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout, fs)
			return 0
		}
		printUsage(stderr, fs)
		return exitUsage
	}

	if fs.NArg() == 0 {
		printUsage(stderr, fs)
		return exitUsage
	}

	switch cmd := fs.Arg(0); cmd {
	case "help":
		printUsage(stdout, fs)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		printUsage(stderr, fs)
		return exitUsage
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
