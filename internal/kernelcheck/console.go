package kernelcheck

import (
	"bytes"
	"regexp"
)

// consoleSigns are what a kernel prints on the console when it cannot run the init, each with the
// option whose absence it shows. The kernel lines come from init/main.c: error -8 is ENOEXEC, a
// kernel that does not run ELF binaries. The Go lines come from the runtime, which prints them
// and dies before main when a system call it needs returns ENOSYS, 38 on both architectures.
var consoleSigns = []struct {
	sign   *regexp.Regexp
	option string
}{
	{regexp.MustCompile(`Requested init \S+ failed \(error -8\)|Failed to execute \S+ \(error -8\)`), "CONFIG_BINFMT_ELF"},
	{regexp.MustCompile(`futexwakeup addr=\S+ returned -38\b`), "CONFIG_FUTEX"},
	{regexp.MustCompile(`runtime: epollcreate failed with -?38\b`), "CONFIG_EPOLL"},
	{regexp.MustCompile(`runtime: eventfd failed with -?38\b`), "CONFIG_EVENTFD"},
}

// FromConsole reads the console of a VM that stopped before the init wrote anything, and returns
// a *MissingError naming the options whose absence the console shows: a kernel that cannot run
// the init is refused by the host, since the init never runs to refuse it. It returns nil when the
// console shows none of them, whatever else stopped the VM.
func FromConsole(console []byte) error {
	var missing []string
	for _, s := range consoleSigns {
		if s.sign.Match(console) {
			missing = append(missing, s.option)
		}
	}
	if len(missing) > 0 {
		return &MissingError{Options: missing}
	}
	return nil
}

// panicLine is how the kernel marks a panic on the console, what FromConsole is called on.
var panicLine = []byte("Kernel panic - not syncing:")

// Panicked says whether the console shows a kernel panic.
func Panicked(console []byte) bool {
	return bytes.Contains(console, panicLine)
}
