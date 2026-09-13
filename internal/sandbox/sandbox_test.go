package sandbox_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

func TestSpecArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec sandbox.Spec
		want string
	}{
		{
			name: "minimal",
			spec: sandbox.Spec{},
			want: "run -d --rm --init --label cove=sandbox cove-sandbox:local sleep infinity",
		},
		{
			name: "all options",
			spec: sandbox.Spec{Name: "demo", CPUs: 2, Memory: "4G", Env: []string{"FOO=bar", "TERM"}},
			want: "run -d --rm --init --name demo --label cove=sandbox" +
				" --cpus 2 --memory 4G -e FOO=bar -e TERM cove-sandbox:local sleep infinity",
		},
		{
			name: "keep",
			spec: sandbox.Spec{Keep: true},
			want: "run -d --init --label cove=sandbox cove-sandbox:local sleep infinity",
		},
		{
			name: "branch",
			spec: sandbox.Spec{Branch: "main"},
			want: "run -d --rm --init --label cove=sandbox --label cove.branch=main cove-sandbox:local sleep infinity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, strings.Fields(tt.want), tt.spec.Args())
		})
	}
}

func TestCheckBranch(t *testing.T) {
	t.Parallel()

	require.NoError(t, sandbox.CheckBranch("feat/x-1"))
	require.ErrorIs(t, sandbox.CheckBranch("feat/x=1"), sandbox.ErrBranchLabel)
}

// TestSpecArgsNeverMountsOrOverridesUser is the test the image README asks for: what can defeat
// the contract is not the image but the way it is started, so no Spec, however hostile, may produce
// a mount, a user override, a working directory, a host credential or a network option.
func TestSpecArgsNeverMountsOrOverridesUser(t *testing.T) {
	t.Parallel()

	spec := sandbox.Spec{
		Name:   "-v",
		Keep:   true,
		CPUs:   1,
		Memory: "--volume",
		Env:    []string{"-v", "--mount", "-u", "-w"},
		Branch: "-w",
	}
	args := spec.Args()

	image := slices.Index(args, sandbox.Image)
	require.Positive(t, image)
	require.Equal(t, []string{"sleep", "infinity"}, args[image+1:])

	// Every flag before the image is one cove emits itself; a valued flag consumes the next token.
	bare := strings.Fields("run -d --rm --init")
	valued := strings.Fields("--name --label --cpus --memory -e")
	for i := 0; i < image; i++ {
		switch {
		case slices.Contains(bare, args[i]):
		case slices.Contains(valued, args[i]):
			i++
		default:
			require.Failf(t, "unexpected flag", "%q", args[i])
		}
	}
}
