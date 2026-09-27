package sandbox_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/erofs"
	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/rwdisk"
	"gitlab.com/hich-hich/cove/internal/sandbox"
	"gitlab.com/hich-hich/cove/internal/vminit/spec"
)

func TestTailKeepsTheLastLinesAndNothingATerminalWouldActOn(t *testing.T) {
	t.Parallel()

	console := "one\ntwo\r\n\x1b]0;owned\x07three\tend\n"

	got := sandbox.Tail([]byte(console), 2)

	require.Equal(t, "  two\n  ?]0;owned?three\tend", got,
		"the guest writes the console, and an escape sequence must reach nobody's terminal")
}

func TestDescribeSizesEachLayerByItsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	disk := func(name string, size int) string {
		require.NoError(t, os.WriteFile(dir+"/"+name, []byte(strings.Repeat("x", size)), 0o600))
		return dir + "/" + name
	}
	req := sandbox.Request{
		Record: inventory.Record{Name: "demo"},
		Plan: erofs.Plan{
			MountOptions: []string{"xino=on"},
			Layers: []erofs.Mount{
				{Path: disk("top", 3), DiffID: "sha256:top", Mountpoint: "/l/00"},
				{Path: disk("base", 5), DiffID: "sha256:base", Mountpoint: "/l/01"},
			},
		},
		Run: spec.Run{User: "agent", Project: "repo", Agent: "claude"},
	}

	got, err := sandbox.Describe(req, rwdisk.Sizes[0])

	require.NoError(t, err)
	require.Equal(t, &spec.Run{
		Hostname: "demo", MountOptions: []string{"xino=on"},
		Layers: []spec.Layer{
			{Disk: spec.Disk{Size: 3}, DiffID: "sha256:top", Mountpoint: "/l/00"},
			{Disk: spec.Disk{Size: 5}, DiffID: "sha256:base", Mountpoint: "/l/01"},
		},
		Write: spec.Disk{Size: int64(rwdisk.Sizes[0])},
		User:  "agent", Project: "repo", Agent: "claude",
	}, got)
}
