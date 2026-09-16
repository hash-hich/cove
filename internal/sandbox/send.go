package sandbox

import (
	"context"
	"crypto/rand"
	"fmt"
)

// agent is the coding agent of the image, the only program cove starts inside a sandbox.
const agent = "claude"

// SendSpec describes one invocation of the agent inside a sandbox. Target must already be screened
// (Screen): container exec reaches any VM of the store. Thread must be set, by NewThreadID for a
// new conversation or by the caller for a resumed one.
type SendSpec struct {
	// Target is the sandbox to reach, by name or ID, resolved by container.
	Target string
	// Prompt is the turn to drive; empty attaches a terminal to the agent instead.
	Prompt string
	// Thread identifies the conversation: a UUID for a new one, a UUID or a display name for a
	// resumed one.
	Thread string
	// Resume tells whether Thread names a conversation that already exists.
	Resume bool
	// Continue picks up the last conversation of the sandbox instead of naming one; Thread is then
	// unused. Attached regime only: claude keeps no record of its driven threads for --continue, so
	// a driven turn would silently start afresh. The CLI refuses that combination before it gets
	// here.
	Continue bool
	// Name is the display name to give the thread; empty leaves it unnamed.
	Name string
}

// Args returns the container exec argument array for s.
//
// Cove owns the argv of the agent: the permission mode, the thread flags and the prompt are all
// that reach claude, so that the output contract below holds whatever the caller typed. Two regimes
// share the verb. With a prompt, claude runs in print mode and its JSON reaches stdout untouched;
// cove never parses it, and never promises its schema. Without one, the exec gets a TTY and the
// REPL of the agent is attached to the terminal of the caller, escape sequences included.
//
// The agent runs in bypass permissions mode in both regimes, and nothing lets a caller keep the
// prompts: the sandbox is the boundary, and a prompt inside it protects nothing. Without the flag,
// print mode never waits for an answer, it denies the tool and goes on, which is what a driven
// turn hit; attached, the human is asked at every edit and command. Claude Code refuses the flag
// as root unless the environment of the image declares a sandbox (IS_SANDBOX, set by the image next
// to the first launch state that answers the disclaimer shown once in the attached regime).
//
// The identity of the thread is always cove's: the UUID it drew (--session-id) or the one the
// caller resumes (--resume, which claude resolves from a UUID as from a display name). Cove keeps
// no index of its own and reads nothing inside the box to know which thread it is talking to.
// --continue is the one exception, by choice: the last thread is whatever claude says it is.
//
// The prompt comes after --, so that one starting with a dash is a prompt and not a flag of claude
// (measured: without it, a prompt of --version prints the version and exits 0).
func (s SendSpec) Args() []string {
	args := []string{execVerb}
	if s.Prompt == "" {
		args = append(args, "--interactive", "--tty")
	}
	args = append(args, s.Target, agent, "--dangerously-skip-permissions")
	switch {
	case s.Continue:
		args = append(args, "--continue")
	case s.Resume:
		args = append(args, "--resume", s.Thread)
	default:
		args = append(args, "--session-id", s.Thread)
	}
	if s.Name != "" {
		args = append(args, "--name", s.Name)
	}
	if s.Prompt != "" {
		args = append(args, "--print", "--output-format", "json", "--", s.Prompt)
	}
	return args
}

// NewThreadID draws the identifier of a new thread: a random version 4 UUID, the shape claude's
// --session-id takes. Cove draws it instead of reading one back from the box, so that a thread has
// an identity before the agent has said anything.
func NewThreadID() string {
	var b [16]byte
	// The error of crypto/rand.Read is always nil since Go 1.24.
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // Version 4.
	b[8] = b[8]&0x3f | 0x80 // Variant 10.
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Send drives the agent of a sandbox with the engine's streams attached. It returns the exit code
// of container exec, and an error only when cove itself could not run it: ErrNotInstalled or a
// failure to execute the CLI.
//
// Nothing is neutralized on the way out: the driven regime carries the JSON of claude, which
// escapes the control characters itself (RFC 8259), and the attached regime is a PTY a human is
// watching. A sandbox that does not run is not started for the occasion: container exec refuses it,
// and cove hands that refusal over as it comes.
func (e *Engine) Send(ctx context.Context, spec SendSpec) (int, error) {
	bin, err := lookPath()
	if err != nil {
		return 0, err
	}
	return e.exec(ctx, bin, spec.Args())
}
