package kernelcheck_test

import (
	"errors"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/kernelcheck"
)

func TestFromMountNamesTheOptionOfAFileSystemTheKernelDoesNotKnow(t *testing.T) {
	t.Parallel()

	// mount(2) says ENODEV for a type the kernel does not know: proc is mounted before the check
	// that reads /proc can run.
	require.Equal(t, []string{"CONFIG_PROC_FS"}, missingOf(t, kernelcheck.FromMount("proc", syscall.ENODEV)))
	require.Equal(t, []string{"CONFIG_UNIX98_PTYS"}, missingOf(t, kernelcheck.FromMount("devpts", syscall.ENODEV)))
}

func TestFromMountLeavesOtherFailuresAsTheyAre(t *testing.T) {
	t.Parallel()

	busy := errors.New("device busy")
	require.Equal(t, busy, kernelcheck.FromMount("proc", busy))
	// cgroup2 is not in the list: the init mounts it only when the kernel has it.
	require.Equal(t, syscall.ENODEV, kernelcheck.FromMount("cgroup2", syscall.ENODEV))
	require.NoError(t, kernelcheck.FromMount("proc", nil))
}
