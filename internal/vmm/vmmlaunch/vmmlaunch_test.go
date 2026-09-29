package vmmlaunch_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vmm/vmmlaunch"
	"gitlab.com/hich-hich/cove/internal/vmm/vmmproto"
)

func TestBootOfNamesEachFileByItsPathWithNoLinkLeft(t *testing.T) {
	t.Parallel()

	resolved, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	link := t.TempDir() + "/link"
	require.NoError(t, os.Symlink(resolved, link))
	for _, name := range []string{"kernel", "initramfs", "console", "layer", "write"} {
		require.NoError(t, os.WriteFile(resolved+"/"+name, nil, 0o600))
	}

	boot, err := vmmlaunch.BootOf(vmmlaunch.Request{
		CPUs: 4, MemoryMiB: 1024, Kernel: link + "/kernel", KernelFormat: vmmproto.KernelELF,
		Initramfs: link + "/initramfs", Cmdline: "console=hvc0", Console: link + "/console",
		Disks: []vmmproto.Disk{{Path: link + "/layer", ReadOnly: true}, {Path: link + "/write"}},
	})

	require.NoError(t, err)
	require.Equal(t, vmmproto.Boot{VM: vmmproto.VM{
		CPUs: 4, MemoryMiB: 1024, Kernel: resolved + "/kernel", KernelFormat: vmmproto.KernelELF,
		Initramfs: resolved + "/initramfs", Cmdline: "console=hvc0", Console: resolved + "/console",
		Disks: []vmmproto.Disk{{Path: resolved + "/layer", ReadOnly: true}, {Path: resolved + "/write"}},
	}}, boot, "the sandbox of cove-vmm allows a path, and a link in it is another path")
}

func TestBootOfRefusesAFileThatDoesNotExist(t *testing.T) {
	t.Parallel()

	_, err := vmmlaunch.BootOf(vmmlaunch.Request{Kernel: t.TempDir() + "/kernel"})

	require.ErrorContains(t, err, "resolve a file of the VM", "the monitor creates no file")
}

func TestBootOfResolvesTheDirectoryOfASocket(t *testing.T) {
	t.Parallel()

	resolved, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	link := t.TempDir() + "/link"
	require.NoError(t, os.Symlink(resolved, link))

	boot, err := vmmlaunch.BootOf(vmmlaunch.Request{
		Vsock: []vmmproto.VsockPort{{Port: 1024, Socket: link + "/control.sock"}},
		Card:  &vmmproto.Card{Socket: link + "/card.sock", MAC: "5a:94:ef:e4:0c:ee"},
	})

	require.NoError(t, err)
	require.Equal(t, []vmmproto.VsockPort{{Port: 1024, Socket: resolved + "/control.sock"}}, boot.VM.Vsock,
		"the socket does not exist yet, and its directory has no link left")
	require.Equal(t, &vmmproto.Card{Socket: resolved + "/card.sock", MAC: "5a:94:ef:e4:0c:ee"}, boot.VM.Card)
}
