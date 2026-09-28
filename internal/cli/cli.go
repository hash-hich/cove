// Package cli parses the cove command line and dispatches to the requested command.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// ExitUsage is the exit code for a usage error, following the flag package convention.
const ExitUsage = 2

// ExitPreflight is the exit code when cove itself could not carry a command out, as the 125 of
// docker run.
const ExitPreflight = 125

// App holds the streams of a cove invocation.
type App struct {
	// Stdin is only read by an attached send, which hands it to the terminal of the agent.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run parses args and dispatches to the requested command. It returns the process exit code.
//
// Help that was asked for goes to stdout with exit code 0. An error goes to stderr with a non-zero
// code, followed by the shapes the command accepts and the way to its help rather than the help
// itself, as docker does: the options and the prose would drown the error, which is what the
// caller must read. Only a bare cove gets the whole usage.
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
		printUsageError(a.Stderr, "", coveUsage)
		return ExitUsage
	}

	if fs.NArg() == 0 {
		printUsage(a.Stderr, fs)
		return ExitUsage
	}

	cmd := fs.Arg(0)
	if cmd == "help" {
		printUsage(a.Stdout, fs)
		return 0
	}
	run, ok := commands[cmd]
	if !ok {
		_, _ = fmt.Fprintf(a.Stderr, "unknown command %q\n", cmd)
		printUsageError(a.Stderr, "", coveUsage)
		return ExitUsage
	}
	return run(a, fs.Args()[1:])
}

// commands maps each verb of cove to the function that runs it on the arguments that follow it.
var commands = map[string]func(*App, []string) int{
	// ls and ps are the names docker and podman gave the same verb; both reach list, whose help
	// and errors carry the canonical name.
	"list": listCommand,
	"ls":   listCommand,
	"ps":   listCommand,
	"pull": pullCommand,
	"rm":   rmCommand,
	"run":  runCommand,
	"send": sendCommand,
	"stop": stopCommand,
}

// terminal reports whether stream, a reader or a writer, is a character device, which a terminal
// is and a pipe or a file is not. It is an approximation of the real question, whether the stream
// renders what is written for a human: a redirection from another character device passes it.
func terminal(stream any) bool {
	f, ok := stream.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// printUsageError writes to w what follows the message of a usage error: the shapes the command
// accepts, then where its full help is, for cove itself when command is empty and for that
// command otherwise. The three parts are those of docker, and each answers a different question:
// the message says what was refused, the shape says what was expected, and the help stays one
// command away rather than drowning the message.
func printUsageError(w io.Writer, command string, usage string) {
	name := "cove"
	if command != "" {
		name += " " + command
	}
	_, _ = fmt.Fprintf(w, "\n%s\n\nSee '%s --help'.\n", synopsis(usage), name)
}

// synopsis returns the shapes a command accepts: the first paragraph of its usage text, which is
// its Usage lines, without the options and the prose that follow.
func synopsis(usage string) string {
	shapes, _, _ := strings.Cut(usage, "\n\n")
	return shapes
}

// coveUsage is the help of cove itself: the shape it takes, then its commands.
const coveUsage = `Usage: cove <command> [flags]

Commands:
  help    Show help
  list    List sandboxes (aliases: ls, ps)
  pull    Pull an image into the store of cove
  rm      Remove sandboxes
  run     Create a sandbox from a repository
  send    Talk to the agent of a sandbox
  stop    Stop sandboxes
`

// printUsage writes the usage text and the flag defaults of fs to w.
func printUsage(w io.Writer, fs *flag.FlagSet) {
	_, _ = fmt.Fprint(w, coveUsage)
	fs.SetOutput(w)
	fs.PrintDefaults()
}
