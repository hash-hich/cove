package vmcreate_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/erofs"
	"gitlab.com/hich-hich/cove/internal/vmcreate"
	"gitlab.com/hich-hich/cove/internal/vminit/spec"
	"gitlab.com/hich-hich/cove/internal/vmlaunch"
	"gitlab.com/hich-hich/cove/internal/writedisk"
)

func TestTailKeepsTheLastLinesAndNothingATerminalWouldActOn(t *testing.T) {
	t.Parallel()

	console := "one\ntwo\r\n\x1b]0;owned\x07three\tend\n"

	got := vmcreate.Tail([]byte(console), 2)

	require.Equal(t, "  two\n  ?]0;owned?three\tend", got,
		"the guest writes the console, and an escape sequence must reach nobody's terminal")
}

func TestDescribeSizesEachLayerByItsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	disk := func(name string, size int) vmlaunch.Disk {
		require.NoError(t, os.WriteFile(dir+"/"+name, []byte(strings.Repeat("x", size)), 0o600))
		f, err := os.Open(dir + "/" + name) //nolint:gosec // G304: a file of the temporary directory of the test.
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		return vmlaunch.Disk{File: f, ReadOnly: true}
	}
	disks := []vmlaunch.Disk{disk("top", 3), disk("base", 5), disk("rw", 0)}
	req := vmcreate.Request{
		Name: "demo",
		Plan: erofs.Plan{
			MountOptions: []string{"xino=on"},
			Layers: []erofs.Mount{
				{DiffID: "sha256:top", Mountpoint: "/l/00"}, {DiffID: "sha256:base", Mountpoint: "/l/01"},
			},
		},
		Run: spec.Run{User: "agent", Project: "repo", Agent: "claude"},
	}

	got, err := vmcreate.Describe(disks, req, writedisk.Sizes[0])

	require.NoError(t, err)
	require.Equal(t, &spec.Run{
		Hostname: "demo", MountOptions: []string{"xino=on"},
		Layers: []spec.Layer{
			{Disk: spec.Disk{Size: 3}, DiffID: "sha256:top", Mountpoint: "/l/00"},
			{Disk: spec.Disk{Size: 5}, DiffID: "sha256:base", Mountpoint: "/l/01"},
		},
		Write: spec.Disk{Size: int64(writedisk.Sizes[0])},
		User:  "agent", Project: "repo", Agent: "claude",
	}, got)
}
