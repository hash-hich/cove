package vmlaunch_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vmlaunch"
	"gitlab.com/hich-hich/cove/internal/vmmproto"
)

func TestLayoutNamesEachFileByTheDescriptorItLandsOn(t *testing.T) {
	t.Parallel()

	file := func(name string) *os.File {
		f, err := os.Create(t.TempDir() + "/" + name) //nolint:gosec // G304: in the temporary directory of the test.
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	pipe, kernel, initramfs, console := file("pipe"), file("kernel"), file("initramfs"), file("console")
	layer, write := file("layer"), file("write")

	files, boot := vmlaunch.Layout(pipe, vmlaunch.Request{
		CPUs: 4, MemoryMiB: 1024, Kernel: kernel, KernelFormat: vmmproto.KernelELF, Initramfs: initramfs,
		Cmdline: "console=hvc0", Console: console,
		Disks: []vmlaunch.Disk{{File: layer, ReadOnly: true}, {File: write}},
	})

	// os/exec gives the nth extra file descriptor 3+n, so the Boot must name each file by that.
	require.Equal(t, []*os.File{pipe, kernel, initramfs, console, layer, write}, files)
	require.Equal(t, vmmproto.Boot{
		CPUs: 4, MemoryMiB: 1024, Kernel: 4, KernelFormat: vmmproto.KernelELF, Initramfs: 5,
		Cmdline: "console=hvc0", Console: 6,
		Disks: []vmmproto.Disk{{FD: 7, ReadOnly: true}, {FD: 8}},
	}, boot)
}
