package kernel_test

import (
	"errors"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/kernel"
)

func TestCheckMountNamesTheOptionOfAFileSystemTheKernelDoesNotKnow(t *testing.T) {
	t.Parallel()

	// mount(2) says ENODEV for a type the kernel does not know: proc is mounted before the check
	// that reads /proc can run.
	require.Equal(t, []string{"CONFIG_PROC_FS"}, missingOf(t, kernel.CheckMount("proc", syscall.ENODEV)))
	require.Equal(t, []string{"CONFIG_UNIX98_PTYS"}, missingOf(t, kernel.CheckMount("devpts", syscall.ENODEV)))
}

func TestCheckMountLeavesOtherFailuresAsTheyAre(t *testing.T) {
	t.Parallel()

	busy := errors.New("device busy")
	require.Equal(t, busy, kernel.CheckMount("proc", busy))
	// cgroup2 is not in the list: the init mounts it only when the kernel has it.
	require.Equal(t, syscall.ENODEV, kernel.CheckMount("cgroup2", syscall.ENODEV))
	require.NoError(t, kernel.CheckMount("proc", nil))
}
