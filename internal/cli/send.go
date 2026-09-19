package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"

	"gitlab.com/hich-hich/cove/internal/agent"
	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// sendCommand drives the agent of a sandbox and returns the process exit code.
func sendCommand(a *App, args []string) int {
	turn, err := parseSend(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, sendUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: %v\n", err)
		printUsageError(a.Stderr, "send", sendUsage)
		return ExitUsage
	}

	ctx := context.Background()
	vms, err := sandbox.List(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: %v\n", err)
		return ExitPreflight
	}
	// Cove never talks to a VM it did not launch, as stop refuses one. A target it knows nothing
	// about is left to container, which resolves IDs and reports the unknown ones itself.
	if len(sandbox.Screen(vms, []string{turn.Target}).Refused) > 0 {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: %s is not a cove sandbox\n", turn.Target)
		return exitRefused
	}

	if !turn.Resume && !turn.Continue {
		turn.Thread = agent.NewThreadID()
		// Driven, the identifier comes back in the session_id of the JSON, and stdout must carry
		// that JSON and nothing else. Attached there is no JSON and stdout is the PTY, so stderr is
		// the only place left to name the thread a later turn would resume.
		if announced(a, turn, vms) {
			_, _ = fmt.Fprintf(a.Stderr, "thread %s\n", turn.Thread)
		}
	}

	// Stdin reaches the agent only when a terminal is attached to it: a driven turn reads nothing.
	stdin := a.Stdin
	if turn.Prompt != "" {
		stdin = nil
	}
	engine := &sandbox.Engine{Stdin: stdin, Stdout: a.Stdout, Stderr: a.Stderr}
	code, err := engine.Send(ctx, turn)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove send: %v\n", err)
		return ExitPreflight
	}
	return code
}

// announced reports whether the thread of turn is to be announced: a terminal is about to be
// attached, on a sandbox that runs, from a terminal (container exec -t needs one). An
// identifier for a thread that never opened would be resumed in vain.
func announced(a *App, turn agent.Turn, vms []sandbox.VM) bool {
	return turn.Prompt == "" && slices.Contains(sandbox.Running(vms), turn.Target) && terminal(a.Stdin)
}

// parseSend turns the arguments of send into a turn. It returns flag.ErrHelp when help was
// asked for, and an error carrying the message to show on any other usage error.
func parseSend(args []string) (agent.Turn, error) {
	var turn agent.Turn
	fs := flag.NewFlagSet("cove send", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&turn.Thread, "r", "", "")
	fs.StringVar(&turn.Thread, "resume", "", "")
	fs.BoolVar(&turn.Continue, "c", false, "")
	fs.BoolVar(&turn.Continue, "continue", false, "")
	fs.StringVar(&turn.Name, "n", "", "")
	fs.StringVar(&turn.Name, "name", "", "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return turn, err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "r" || f.Name == "resume" {
			turn.Resume = true
		}
	})
	if err := readOperands(&turn, fs.Args()); err != nil {
		return turn, err
	}
	if err := checkThread(turn); err != nil {
		return turn, err
	}
	return turn, nil
}

// checkThread rejects the thread flags that name no thread or two at once.
func checkThread(turn agent.Turn) error {
	if turn.Resume && turn.Thread == "" {
		return errors.New("--resume requires a thread, by UUID or by display name")
	}
	if turn.Resume && turn.Continue {
		return errors.New("--continue and --resume are mutually exclusive")
	}
	// Driven, claude's --continue skips the threads driven turns created and starts afresh without
	// a word: the caller would believe the agent has lost the thread.
	if turn.Continue && turn.Prompt != "" {
		return errors.New("--continue takes no prompt; to send one to an existing thread, use --resume THREAD")
	}
	return nil
}

// readOperands reads the positional arguments of send into turn: the sandbox, and the prompt when
// one is given.
func readOperands(turn *agent.Turn, args []string) error {
	switch len(args) {
	case 0:
		return errors.New("requires at least 1 argument")
	case 1:
		turn.Target = args[0]
	case 2:
		// An empty prompt is a caller whose variable was empty, not a request for a terminal:
		// attaching one would hang on an input nobody is typing.
		if args[1] == "" {
			return errors.New("the prompt is empty")
		}
		turn.Target, turn.Prompt = args[0], args[1]
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

Exit codes: the one of container exec, which carries the one of the agent; 1
when the target is not a sandbox of cove; 2 on a usage error; 125 when cove
could not run container.
`
