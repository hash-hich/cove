package sandbox_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// TestParseList reads a container list --all --format json output recorded on 1.3.1, with Apple's
// builder VM, a running sandbox and a stopped one kept with --keep.
func TestParseList(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/list.json")
	require.NoError(t, err)

	vms, err := sandbox.ParseList(data)

	require.NoError(t, err)
	require.Equal(t, []sandbox.VM{
		{
			ID:     "buildkit",
			Labels: map[string]string{"com.apple.container.plugin": "builder", "com.apple.container.resource.role": "builder"},
			State:  "running",
		},
		{ID: "cove-fx-run", Labels: map[string]string{"cove": "sandbox"}, State: "running"},
		{ID: "cove-fx-keep", Labels: map[string]string{"cove": "sandbox"}, State: "stopped"},
	}, vms)
	require.False(t, vms[0].IsSandbox())
	require.True(t, vms[1].IsSandbox())
	require.True(t, vms[1].Running())
	require.False(t, vms[2].Running())
}

func TestParseListErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{name: "empty", data: ""},
		{name: "not an array", data: `{"configuration":{}}`},
		{name: "truncated", data: `[{"configuration":{"id":"x"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := sandbox.ParseList([]byte(tt.data))

			require.Error(t, err)
		})
	}
}

func TestParseListEmpty(t *testing.T) {
	t.Parallel()

	vms, err := sandbox.ParseList([]byte("[]"))

	require.NoError(t, err)
	require.Empty(t, vms)
}
