package sandbox_test

import (
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// The fixture testdata/list.json was recorded on container 1.3.1 with Apple's builder VM, a
// running sandbox and a stopped one kept with --keep.
const (
	builder = "buildkit"
	runSB   = "cove-fx-run"
	keepSB  = "cove-fx-keep"
	unknown = "nope"
	other   = "receiver"
	running = "running"
	stopped = "stopped"
	linux   = "linux"
	arm64   = "arm64"
)

var (
	builderLabels = map[string]string{
		"com.apple.container.plugin":        "builder",
		"com.apple.container.resource.role": "builder",
	}
	sandboxLabels = map[string]string{"cove": "sandbox"}
)

// store mirrors testdata/list.json.
var store = []sandbox.VM{
	{
		ID: builder, Labels: builderLabels, State: running,
		Image: "ghcr.io/apple/container-builder-shim/builder:0.13.1", OS: linux, Architecture: arm64,
		IPv4Address: "192.168.64.118/24", CPUs: 2, MemoryInBytes: 2147483648,
		StartedDate: "2026-09-07T17:11:05Z",
	},
	{
		ID: runSB, Labels: sandboxLabels, State: running,
		Image: sandbox.DefaultImage, OS: linux, Architecture: arm64,
		IPv4Address: "192.168.64.152/24", CPUs: 4, MemoryInBytes: 1073741824,
		StartedDate: "2026-09-08T15:13:38Z",
	},
	{
		ID: keepSB, Labels: sandboxLabels, State: stopped,
		Image: sandbox.DefaultImage, OS: linux, Architecture: arm64,
		CPUs: 4, MemoryInBytes: 1073741824,
		StartedDate: "2026-09-08T15:13:42Z",
	},
}

func TestParseList(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/list.json")
	require.NoError(t, err)

	vms, err := sandbox.ParseList(data)

	require.NoError(t, err)
	require.Equal(t, store, vms)
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

func TestScreen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names []string
		want  sandbox.Screening
	}{
		{name: "sandbox", names: []string{runSB}, want: sandbox.Screening{Kept: []string{runSB}}},
		{name: "stopped sandbox", names: []string{keepSB}, want: sandbox.Screening{Kept: []string{keepSB}}},
		{name: "other vm", names: []string{builder}, want: sandbox.Screening{Refused: []string{builder}}},
		{name: "unknown is left to container", names: []string{unknown}, want: sandbox.Screening{Kept: []string{unknown}}},
		{
			name:  "mixed keeps the order",
			names: []string{builder, runSB, unknown},
			want:  sandbox.Screening{Kept: []string{runSB, unknown}, Refused: []string{builder}},
		},
		{name: "wrong label value", names: []string{other}, want: sandbox.Screening{Refused: []string{other}}},
	}

	vms := slices.Concat(store, []sandbox.VM{{ID: other, Labels: map[string]string{"cove": other}, State: running}})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, sandbox.Screen(vms, tt.names))
		})
	}
}

func TestRunning(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{runSB}, sandbox.Running(store))
	require.Empty(t, sandbox.Running(store[:1]))
}

func TestSandboxes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		all  bool
		want []sandbox.VM
	}{
		{name: "running only", want: []sandbox.VM{store[1]}},
		{name: "all", all: true, want: []sandbox.VM{store[1], store[2]}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, sandbox.Sandboxes(store, tt.all))
		})
	}
}

func TestSandboxesEmpty(t *testing.T) {
	t.Parallel()

	require.Empty(t, sandbox.Sandboxes(store[:1], true))
	require.Empty(t, sandbox.Sandboxes(nil, true))
}
