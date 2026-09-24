package kernelcheck_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/kernelcheck"
)

// killInit is the panic of a kernel whose init exited with status 2, which a Go program that
// throws does.
const killInit = "Kernel panic - not syncing: Attempted to kill init! exitcode=0x00000200\n"

func TestFromConsoleNamesWhatTheConsoleShows(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, fixture, after, want string
	}{
		// Captured from a kernel built without BINFMT_ELF, booted with an ELF init.
		{"no elf", "testdata/console-no-binfmt-elf.txt", "", binfmtELF},
		// Captured from a Go program whose epoll_create1 and eventfd2 returned ENOSYS, followed by
		// the panic the kernel prints when that program is the init.
		{"no epoll", "testdata/go-no-epoll.txt", killInit, "CONFIG_EPOLL"},
		{"no eventfd", "testdata/go-no-eventfd.txt", killInit, "CONFIG_EVENTFD"},
	} {
		console, err := os.ReadFile(tc.fixture)
		require.NoError(t, err, tc.name)
		console = append(console, tc.after...)
		require.Equal(t, []string{tc.want}, missingOf(t, kernelcheck.FromConsole(console)), tc.name)
	}
}

func TestFromConsoleReadsTheOtherPanicsOfAnInitThatDoesNotRun(t *testing.T) {
	t.Parallel()

	for console, want := range map[string]string{
		// init/main.c says it this way when the init is the one of the initramfs, not named by init=.
		"Failed to execute /init (error -8)\nKernel panic - not syncing: No working init found.  " +
			"Try passing init= option to kernel.\n": binfmtELF,
		// Without an initramfs the kernel falls back to a root device cove never gives
		// (init/do_mounts.c).
		"VFS: Cannot open root device \"\" or unknown-block(0,0): error -6\n" +
			"Kernel panic - not syncing: VFS: Unable to mount root fs on unknown-block(0,0)\n": "CONFIG_BLK_DEV_INITRD",
		// Taken from the runtime source (os_linux.go), and never measured: without futexes the
		// runtime spins, and this line comes only at the first wake of a parked thread.
		"futexwakeup addr=0x21cb60 returned -38\nSIGSEGV: segmentation violation\n" + killInit: futex,
	} {
		require.Equal(t, []string{want}, missingOf(t, kernelcheck.FromConsole([]byte(console))), console)
	}
}

func TestFromConsoleCountsASignOnlyBeforeThePanicItEndsIn(t *testing.T) {
	t.Parallel()

	for _, console := range []string{
		// No panic: whatever the lines say, the run did not end on one.
		"runtime: epollcreate failed with 38\n",
		// The sign is there, but the kernel panicked for another reason.
		"runtime: epollcreate failed with 38\nKernel panic - not syncing: Oops: Fatal exception\n",
		// The sign comes after the panic, a line of something else that happens to carry it.
		killInit + "runtime: eventfd failed with 38\n",
		// The init died of its own error: not the kernel's.
		"panic: runtime error: index out of range\n" + killInit,
		// The init was not found, which is the initramfs cove built, not the kernel.
		"Failed to execute /init (error -2)\nKernel panic - not syncing: No working init found.\n",
		// Another errno from epoll is not a missing option.
		"runtime: epollcreate failed with 24\n" + killInit,
		"",
	} {
		require.NoError(t, kernelcheck.FromConsole([]byte(console)), console)
	}
}
