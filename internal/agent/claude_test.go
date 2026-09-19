package agent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/agent"
)

// The sandbox and the thread the tests of the package name.
const (
	demo   = "demo"
	thread = "0b7f1a3c-9e42-4d51-8a6b-2f0c5d7e1934"
	review = "review"
)

func TestTurnArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		turn agent.Turn
		// want is the command line without the prompt, which Args always puts last and which the
		// assertion appends verbatim: a prompt is one argument, spaces included. The target is not
		// in it: reaching the sandbox is the backend's work, this is what runs once inside.
		want string
	}{
		{
			name: "driven turn",
			turn: agent.Turn{Target: demo, Prompt: "fix the build", Thread: thread},
			want: "claude --dangerously-skip-permissions --session-id " + thread +
				" --print --output-format json --",
		},
		{
			name: "a prompt that starts with a dash",
			turn: agent.Turn{Target: demo, Prompt: "--version", Thread: thread},
			want: "claude --dangerously-skip-permissions --session-id " + thread +
				" --print --output-format json --",
		},
		{
			name: "attached terminal",
			turn: agent.Turn{Target: demo, Thread: thread},
			want: "claude --dangerously-skip-permissions --session-id " + thread,
		},
		{
			name: "resumed by UUID",
			turn: agent.Turn{Target: demo, Prompt: "go on", Thread: thread, Resume: true},
			want: "claude --dangerously-skip-permissions --resume " + thread +
				" --print --output-format json --",
		},
		{
			name: "resumed by display name, attached",
			turn: agent.Turn{Target: demo, Thread: review, Resume: true},
			want: "claude --dangerously-skip-permissions --resume review",
		},
		{
			name: "named thread",
			turn: agent.Turn{Target: demo, Prompt: "hi", Thread: thread, Name: review},
			want: "claude --dangerously-skip-permissions --session-id " + thread +
				" --name review --print --output-format json --",
		},
		{
			name: "continued, attached",
			turn: agent.Turn{Target: demo, Continue: true},
			want: "claude --dangerously-skip-permissions --continue",
		},
		{
			name: "continued and named",
			turn: agent.Turn{Target: demo, Continue: true, Name: review},
			want: "claude --dangerously-skip-permissions --continue --name review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want := strings.Fields(tt.want)
			if tt.turn.Prompt != "" {
				want = append(want, tt.turn.Prompt)
			}
			require.Equal(t, want, tt.turn.Args())
		})
	}
}
