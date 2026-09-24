package kernelcheck_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/kernelcheck"
)

func TestFromConsoleNamesWhatTheConsoleShows(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, fixture, want string
	}{
		// Captured from a kernel built without BINFMT_ELF, booted with an ELF init.
		{"no elf", "testdata/console-no-binfmt-elf.txt", "CONFIG_BINFMT_ELF"},
		// Captured from a Go program whose epoll_create1 and eventfd2 returned ENOSYS.
		{"no epoll", "testdata/go-no-epoll.txt", "CONFIG_EPOLL"},
		{"no eventfd", "testdata/go-no-eventfd.txt", "CONFIG_EVENTFD"},
	} {
		console, err := os.ReadFile(tc.fixture)
		require.NoError(t, err, tc.name)
		require.Equal(t, []string{tc.want}, missingOf(t, kernelcheck.FromConsole(console)), tc.name)
	}
}

func TestFromConsoleReadsTheOtherMessageOfAnInitThatDoesNotRun(t *testing.T) {
	t.Parallel()

	// init/main.c says it this way when the init is the one of the initramfs, and not named by
	// init=.
	console := []byte("Failed to execute /init (error -8)\nKernel panic - not syncing: No working init found.\n")
	require.True(t, kernelcheck.Panicked(console))
	require.Equal(t, []string{"CONFIG_BINFMT_ELF"}, missingOf(t, kernelcheck.FromConsole(console)))
}

func TestFromConsoleReadsAFutexTheRuntimeCannotWake(t *testing.T) {
	t.Parallel()

	// Taken from the runtime source (os_linux.go): a futex that returns ENOSYS cannot be isolated
	// by seccomp, which runc meets before the program starts.
	console := []byte("futexwakeup addr=0x21cb60 returned -38\nSIGSEGV: segmentation violation\n")
	require.Equal(t, []string{"CONFIG_FUTEX"}, missingOf(t, kernelcheck.FromConsole(console)))
}

func TestFromConsoleIgnoresOtherFailures(t *testing.T) {
	t.Parallel()

	for _, console := range []string{
		// The init died of its own error: not the kernel's.
		"panic: runtime error: index out of range\nKernel panic - not syncing: Attempted to kill init! exitcode=0x00000200\n",
		// The init was not found, which is the initramfs cove built, not the kernel.
		"Failed to execute /init (error -2)\nKernel panic - not syncing: No working init found.\n",
		// Another errno from epoll is not a missing option.
		"runtime: epollcreate failed with 24\n",
		"",
	} {
		require.NoError(t, kernelcheck.FromConsole([]byte(console)), console)
	}
	require.False(t, kernelcheck.Panicked([]byte("Run /init as init process\n")))
}
