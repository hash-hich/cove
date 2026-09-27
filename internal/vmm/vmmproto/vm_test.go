package vmmproto_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vmm/vmmproto"
)

func TestFilesSplitsWhatTheMonitorReadsWritesAndListensOn(t *testing.T) {
	t.Parallel()

	vm := vmmproto.VM{
		Kernel: "/k", Initramfs: "/i", Console: "/c",
		Disks: []vmmproto.Disk{{Path: "/top", ReadOnly: true}, {Path: "/base", ReadOnly: true}, {Path: "/rw"}},
		Vsock: []vmmproto.VsockPort{{Port: 1024, Socket: "/s"}},
	}

	require.Equal(t, vmmproto.Files{
		Read: []string{"/k", "/i", "/top", "/base"}, Write: []string{"/rw", "/c"}, Listen: []string{"/s"},
	}, vm.Files())
}
