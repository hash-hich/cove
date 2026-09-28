package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/internal/agent"
	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/sandbox"
	"gitlab.com/hich-hich/cove/internal/tty"
	"gitlab.com/hich-hich/cove/internal/vminit/turn"
)

// The exit codes of send besides the one of the agent. They are a summary for a shell, in the
// numbers callers already know from docker run and timeout(1); the cause is what tells them apart
// from an agent that exits with the same number. Each cause has its number before it exists, since
// the table is a contract.
const (
	// exitTimeout is a turn cut because its time was up, as timeout(1) ends.
	exitTimeout = 124
	// exitCannotRun is an agent found but not run, as a shell ends for any errno but ENOENT.
	exitCannotRun = 126
	// exitNotFound is an agent not found, as a shell ends on ENOENT.
	exitNotFound = 127
	// exitStopped is a turn cut by cove stop, as docker run ends when its container is stopped.
	exitStopped = 143
)

// errAbandoned is why a send stopped waiting for the end of its turn: a second signal, or a signal
// to an attached one, which has no cancel of its own but the hang up of its connection.
var errAbandoned = errors.New("the turn was abandoned")

// sendCommand drives the turn the arguments of send describe, and returns the process exit code.
func sendCommand(a *App, args []string) int {
	t, err := parseSend(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, sendUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: %v\n", err)
		printUsageError(a.Stderr, "send", sendUsage)
		return ExitUsage
	}
	e, err := findSandbox(t.Target)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: %v\n", err)
		return ExitPreflight
	}
	if !t.Resume && !t.Continue {
		t.Thread = agent.NewThreadID()
	}
	req := turn.Request{ID: rand.Text(), Args: t.Args(), Grace: turn.DefaultGrace}
	if t.Prompt == "" {
		return attach(a, e, t, req)
	}
	return drive(a, e, req)
}

// findSandbox returns the sandbox of the inventory target names.
func findSandbox(target string) (inventory.Entry, error) {
	root, err := sandbox.DefaultRoot()
	if err != nil {
		return inventory.Entry{}, err //nolint:wrapcheck // DefaultRoot names the directory.
	}
	entries, err := inventory.List(context.Background(), root)
	if err != nil {
		return inventory.Entry{}, err //nolint:wrapcheck // List names the inventory.
	}
	e, err := inventory.Find(entries, target)
	if err != nil {
		return e, err //nolint:wrapcheck // Find names the target.
	}
	if e.State != inventory.Running {
		return e, fmt.Errorf("%s: %w", target, sandbox.ErrNotRunning)
	}
	return e, nil
}

// drive runs the driven turn req in e. The first SIGINT, SIGTERM or SIGHUP asks the init to cancel
// it, and send goes on reading until its end; the second one abandons it: the connection is closed,
// which the init takes as a terminal that closed.
func drive(a *App, e inventory.Entry, req turn.Request) int {
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, unix.SIGINT, unix.SIGTERM, unix.SIGHUP)
	defer signal.Stop(sigs)
	ctx, abandon := context.WithCancelCause(context.Background())
	defer abandon(nil)
	started := make(chan struct{})
	go func() {
		select {
		case <-sigs:
		case <-ctx.Done():
			return
		}
		// Before the agent started, the init has no turn to cancel and would refuse: the cancel
		// waits for it.
		select {
		case <-started:
		case <-sigs:
			abandon(errAbandoned)
			return
		case <-ctx.Done():
			return
		}
		if err := sandbox.Cancel(e, req.ID, turn.CauseCancel, req.Grace); err != nil {
			_, _ = fmt.Fprintf(a.Stderr, "cove send: %v\n", err)
		}
		select {
		case <-sigs:
			abandon(errAbandoned)
		case <-ctx.Done():
		}
	}()
	exit, err := sandbox.Turn(ctx, e, req, sandbox.Streams{
		Stdout: a.Stdout, Stderr: a.Stderr, Started: func() { close(started) },
	})
	return exitCode(a.Stderr, exit, err)
}

// attach runs the attached turn req of t in e, on the terminal of the caller, raw for as long as
// the turn lasts. Ctrl-C is a key the agent reads, not a cancel: only SIGTERM or SIGHUP abandon the
// turn, which hangs up the agent as a closed window does.
func attach(a *App, e inventory.Entry, t agent.Turn, req turn.Request) int {
	in, ok := a.Stdin.(*os.File)
	if !ok || !terminal(in) {
		_, _ = fmt.Fprintln(a.Stderr, "cove send: without a prompt, a terminal is attached, and the input is not one")
		return ExitPreflight
	}
	fd := int(in.Fd())
	size, err := tty.SizeOf(fd)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: %v\n", err)
		return ExitPreflight
	}
	// The identifier is in no JSON when a terminal is attached: it is said before the screen is
	// the agent's.
	if !t.Resume && !t.Continue {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: thread %s\n", t.Thread)
	}
	req.TTY, req.Size = true, turn.Size{Rows: size.Rows, Cols: size.Cols}
	restore, err := tty.MakeRaw(fd)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: %v\n", err)
		return ExitPreflight
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, unix.SIGTERM, unix.SIGHUP)
	defer signal.Stop(sigs)
	ctx, abandon := context.WithCancelCause(context.Background())
	defer abandon(nil)
	go func() {
		select {
		case <-sigs:
			abandon(errAbandoned)
		case <-ctx.Done():
		}
	}()
	exit, err := sandbox.Turn(ctx, e, req, sandbox.Streams{
		Stdin: in, Stdout: a.Stdout, Stderr: a.Stderr, Resize: resizes(ctx, fd),
	})
	if rerr := restore(); rerr != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: %v\n", rerr)
	}
	return exitCode(a.Stderr, exit, err)
}

// resizes returns the sizes the terminal fd takes, until ctx is done.
func resizes(ctx context.Context, fd int) <-chan turn.Size {
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, unix.SIGWINCH)
	out := make(chan turn.Size)
	go func() {
		defer signal.Stop(winch)
		for {
			select {
			case <-winch:
			case <-ctx.Done():
				return
			}
			size, err := tty.SizeOf(fd)
			if err != nil {
				continue
			}
			select {
			case out <- turn.Size{Rows: size.Rows, Cols: size.Cols}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// execExitCode returns the exit code of an agent that could not be run for e, as a shell folds an
// errno: 127 for ENOENT, 126 for any other, and 125 when the init failed before execve.
func execExitCode(e *turn.ExecError) int {
	switch e.Errno {
	case 0:
		return ExitPreflight
	case turn.ErrnoNotFound:
		return exitNotFound
	default:
		return exitCannotRun
	}
}

// exitCode returns the exit code of a turn that ended as exit, or failed with err, and says on w
// why when the agent is not the one that ended it. The cause wins over the status of the agent: an
// agent a stop hung up dies of SIGHUP, and the turn is still a stop.
func exitCode(w io.Writer, exit turn.Exit, err error) int {
	if e, ok := errors.AsType[*turn.ExecError](err); ok {
		_, _ = fmt.Fprintf(w, "cove send: the agent could not be run: %s\n", e.Message)
		return execExitCode(e)
	}
	if errors.Is(err, errAbandoned) {
		_, _ = fmt.Fprintln(w, "cove send: the turn was abandoned, and the agent hung up")
		return exitInterrupt
	}
	if err != nil {
		_, _ = fmt.Fprintf(w, "cove send: %v\n", err)
		return ExitPreflight
	}
	//exhaustive:ignore // The init reports no other cause; the host sets exec and cove on errors.
	switch exit.Cause {
	case turn.CauseAgent:
		if exit.Code != nil {
			return *exit.Code
		}
		return 128 + exit.Signal
	case turn.CauseStop:
		_, _ = fmt.Fprintln(w, "cove send: the turn was cut: the sandbox was stopped")
		return exitStopped
	case turn.CauseCancel:
		_, _ = fmt.Fprintln(w, "cove send: the turn was cancelled")
		return exitInterrupt
	case turn.CauseTimeout:
		_, _ = fmt.Fprintln(w, "cove send: the turn was cut: its time was up")
		return exitTimeout
	default:
		_, _ = fmt.Fprintf(w, "cove send: the turn ended for %q, a cause cove does not know\n", exit.Cause)
		return ExitPreflight
	}
}

// parseSend turns the arguments of send into a turn. It returns flag.ErrHelp when help was
// asked for, and an error carrying the message to show on any other usage error.
func parseSend(args []string) (agent.Turn, error) {
	var t agent.Turn
	fs := flag.NewFlagSet("cove send", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&t.Thread, "r", "", "")
	fs.StringVar(&t.Thread, "resume", "", "")
	fs.BoolVar(&t.Continue, "c", false, "")
	fs.BoolVar(&t.Continue, "continue", false, "")
	fs.StringVar(&t.Name, "n", "", "")
	fs.StringVar(&t.Name, "name", "", "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return t, err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "r" || f.Name == "resume" {
			t.Resume = true
		}
	})
	if err := readOperands(&t, fs.Args()); err != nil {
		return t, err
	}
	if err := checkThread(t); err != nil {
		return t, err
	}
	return t, nil
}

// checkThread rejects the thread flags that name no thread or two at once.
func checkThread(t agent.Turn) error {
	if t.Resume && t.Thread == "" {
		return errors.New("--resume requires a thread, by UUID or by display name")
	}
	if t.Resume && t.Continue {
		return errors.New("--continue and --resume are mutually exclusive")
	}
	// Driven, claude's --continue skips the threads driven turns created and starts afresh without
	// a word: the caller would believe the agent has lost the thread.
	if t.Continue && t.Prompt != "" {
		return errors.New("--continue takes no prompt; to send one to an existing thread, use --resume THREAD")
	}
	return nil
}

// readOperands reads the positional arguments of send into t: the sandbox, and the prompt when
// one is given.
func readOperands(t *agent.Turn, args []string) error {
	switch len(args) {
	case 0:
		return errors.New("requires at least 1 argument")
	case 1:
		t.Target = args[0]
	case 2:
		// An empty prompt is a caller whose variable was empty, not a request for a terminal:
		// attaching one would hang on an input nobody is typing.
		if args[1] == "" {
			return errors.New("the prompt is empty")
		}
		t.Target, t.Prompt = args[0], args[1]
	default:
		return fmt.Errorf("takes one prompt, quote it as a single argument (got %q)", args[2])
	}
	return nil
}

// sendUsage is the help of send, in the shape of docker exec so that what developers
// already know applies.
const sendUsage = `Usage: cove send [OPTIONS] SANDBOX [PROMPT]

Talk to the agent of a sandbox, given by name or ID. With a prompt, the agent
runs that one turn and the JSON it answers is copied to stdout as it comes.
Without one, a terminal is attached to the agent and the conversation lives in
its REPL until it is left. In both cases the agent asks for no permission: the
sandbox is the boundary, and nothing brings the prompts back.

Each send opens a new thread unless --resume or --continue picks one up. A
sandbox carries as many threads as it is sent, all sharing its files, and cove
arbitrates neither their writes nor their names. The identifier of a new thread
is the session_id of the JSON, and is printed on stderr when a terminal is
attached. --continue reaches the last attached thread only: the agent keeps no
record of driven ones for it, so it takes no prompt.

Options:
  -c, --continue        Attach to the last thread of the sandbox
  -r, --resume string   Continue a thread, by UUID or by display name
  -n, --name string     Set a display name for the thread, to resume it by

A driven turn is cancelled by the first SIGINT, SIGTERM or SIGHUP send gets:
the agent is interrupted, killed once 10s have passed, and send waits for its
end. A second signal leaves at once, which hangs the agent up. Attached,
Ctrl-C is a key the agent reads; SIGTERM or SIGHUP hang it up.

Exit codes: the one of the agent, or 128 plus the signal that ended it; 2 on a
usage error; 124 when the time of the turn was up; 125 when cove could not
carry the command out, the sandbox unknown or not running included; 126 when
the agent could not be run, 127 when it was not found; 130 when the turn was
cancelled or abandoned; 143 when the sandbox was stopped during the turn. When
cove ended the turn, it says why on stderr.
`
