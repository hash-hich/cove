package sandbox_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// The sandbox and the thread the send tests name.
const (
	demo   = "demo"
	thread = "0b7f1a3c-9e42-4d51-8a6b-2f0c5d7e1934"
	review = "review"
)

func TestSendSpecArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec sandbox.SendSpec
		// want is the argument array without the prompt, which Args always puts last and which the
		// assertion appends verbatim: a prompt is one argument, spaces included.
		want string
	}{
		{
			name: "driven turn",
			spec: sandbox.SendSpec{Target: demo, Prompt: "fix the build", Thread: thread},
			want: "exec " + demo + " claude --session-id " + thread + " --print --output-format json --",
		},
		{
			name: "a prompt that starts with a dash",
			spec: sandbox.SendSpec{Target: demo, Prompt: "--version", Thread: thread},
			want: "exec " + demo + " claude --session-id " + thread + " --print --output-format json --",
		},
		{
			name: "attached terminal",
			spec: sandbox.SendSpec{Target: demo, Thread: thread},
			want: "exec --interactive --tty " + demo + " claude --session-id " + thread,
		},
		{
			name: "resumed by UUID",
			spec: sandbox.SendSpec{Target: demo, Prompt: "go on", Thread: thread, Resume: true},
			want: "exec " + demo + " claude --resume " + thread + " --print --output-format json --",
		},
		{
			name: "resumed by display name, attached",
			spec: sandbox.SendSpec{Target: demo, Thread: review, Resume: true},
			want: "exec --interactive --tty " + demo + " claude --resume review",
		},
		{
			name: "named thread",
			spec: sandbox.SendSpec{Target: demo, Prompt: "hi", Thread: thread, Name: review},
			want: "exec " + demo + " claude --session-id " + thread + " --name review --print --output-format json --",
		},
		{
			name: "continued, attached",
			spec: sandbox.SendSpec{Target: demo, Continue: true},
			want: "exec --interactive --tty " + demo + " claude --continue",
		},
		{
			name: "continued and named",
			spec: sandbox.SendSpec{Target: demo, Continue: true, Name: review},
			want: "exec --interactive --tty " + demo + " claude --continue --name review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want := strings.Fields(tt.want)
			if tt.spec.Prompt != "" {
				want = append(want, tt.spec.Prompt)
			}
			require.Equal(t, want, tt.spec.Args())
		})
	}
}

func TestNewThreadID(t *testing.T) {
	t.Parallel()

	// The shape claude's --session-id takes: a version 4 UUID, variant 10.
	uuid4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	seen := make(map[string]bool, 64)
	for range 64 {
		id := sandbox.NewThreadID()
		require.Regexp(t, uuid4, id)
		require.False(t, seen[id], "drew %s twice", id)
		seen[id] = true
	}
}
