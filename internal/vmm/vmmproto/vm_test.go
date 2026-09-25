package vmmproto_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vmm/vmmproto"
)

func TestFilesSplitsWhatTheMonitorReadsFromWhatItWrites(t *testing.T) {
	t.Parallel()

	vm := vmmproto.VM{
		Kernel: "/k", Initramfs: "/i", Console: "/c",
		Disks: []vmmproto.Disk{{Path: "/top", ReadOnly: true}, {Path: "/base", ReadOnly: true}, {Path: "/rw"}},
	}

	require.Equal(t, vmmproto.Files{
		Read: []string{"/k", "/i", "/top", "/base"}, Write: []string{"/rw", "/c"},
	}, vm.Files())
}
