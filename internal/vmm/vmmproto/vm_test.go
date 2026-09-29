package vmmproto_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vmm/vmmproto"
)

func TestFilesSplitsWhatTheMonitorReadsWritesListensAndConnectsTo(t *testing.T) {
	t.Parallel()

	vm := vmmproto.VM{
		Kernel: "/k", Initramfs: "/i", Console: "/c",
		Disks: []vmmproto.Disk{{Path: "/top", ReadOnly: true}, {Path: "/base", ReadOnly: true}, {Path: "/rw"}},
		Vsock: []vmmproto.VsockPort{{Port: 1024, Socket: "/s"}},
		Card:  &vmmproto.Card{Socket: "/card", MAC: "5a:94:ef:e4:0c:ee"},
	}

	require.Equal(t, vmmproto.Files{
		Read: []string{"/k", "/i", "/top", "/base"}, Write: []string{"/rw", "/c"}, Listen: []string{"/s"},
		Connect: []string{"/card"},
	}, vm.Files())
}
