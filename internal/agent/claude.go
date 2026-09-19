package agent

// program is Claude Code as the image installs it, found on the PATH of the VM.
const program = "claude"

// Args returns the command line of the agent for t, as it runs inside the VM. The backend carries
// it in, and gives it a terminal when Prompt is empty, which is the attached regime.
//
// Cove owns the argv of the agent: the permission mode, the thread flags and the prompt are all
// that reach claude, so that the output contract below holds whatever the caller typed. Two regimes
// share the verb. With a prompt, claude runs in print mode and its JSON reaches stdout untouched;
// cove never parses it, and never promises its schema. Without one, the process gets a TTY and the
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
func (t Turn) Args() []string {
	args := []string{program, "--dangerously-skip-permissions"}
	switch {
	case t.Continue:
		args = append(args, "--continue")
	case t.Resume:
		args = append(args, "--resume", t.Thread)
	default:
		args = append(args, "--session-id", t.Thread)
	}
	if t.Name != "" {
		args = append(args, "--name", t.Name)
	}
	if t.Prompt != "" {
		args = append(args, "--print", "--output-format", "json", "--", t.Prompt)
	}
	return args
}
