package kernelcheck

import (
	"bytes"
	"regexp"
)

// panicMark opens the line on which the kernel says why it panics.
var panicMark = []byte("Kernel panic - not syncing: ")

// consoleSigns are what a kernel prints on the console when it cannot run the init, each with the
// panic it ends in and the option whose absence it shows. A sign counts only before or on that
// panic line, so that a line of the agent carrying the same words never does.
//
// The kernel lines come from init/main.c and init/do_mounts.c: error -8 is ENOEXEC, a kernel that
// does not run ELF binaries; a root it cannot mount is a kernel without an initramfs, which falls
// back to mounting a root device cove never gives. The Go lines come from the runtime, which
// prints them and dies when a system call it needs returns ENOSYS, 38 on both architectures. It
// creates its epoll and eventfd lazily, at the first descriptor or timer, so the init may have
// printed lines before. Without futexes the runtime spins rather than dies, and its line comes
// only at the first wake of a parked thread, perhaps never: CheckKernelFile is what refuses it.
var consoleSigns = []struct {
	panic, sign *regexp.Regexp
	option      string
}{
	{
		regexp.MustCompile(`^(No working init found|Requested init )`),
		regexp.MustCompile(`Requested init \S+ failed \(error -8\)|Failed to execute \S+ \(error -8\)`),
		"CONFIG_BINFMT_ELF",
	},
	{
		regexp.MustCompile(`^VFS: Unable to mount root fs on `),
		regexp.MustCompile(`VFS: Unable to mount root fs on `),
		"CONFIG_BLK_DEV_INITRD",
	},
	{
		regexp.MustCompile(`^Attempted to kill init!`),
		regexp.MustCompile(`runtime: epollcreate failed with -?38\b`),
		"CONFIG_EPOLL",
	},
	{
		regexp.MustCompile(`^Attempted to kill init!`),
		regexp.MustCompile(`runtime: eventfd failed with -?38\b`),
		"CONFIG_EVENTFD",
	},
	{
		regexp.MustCompile(`^Attempted to kill init!`),
		regexp.MustCompile(`futexwakeup addr=\S+ returned -38\b`),
		"CONFIG_FUTEX",
	},
}

// FromConsole reads the console of a run that ended in a kernel panic with neither a refusal nor
// a result from the init, and returns a *MissingError naming the options whose absence the
// console shows, up to the first panic. It is the fallback for a kernel CheckKernelFile could not
// read: a kernel that cannot run the init is refused by the host, since the init never runs to
// refuse it. It returns nil when there is no panic, or when the panic shows none of them.
func FromConsole(console []byte) error {
	at := bytes.Index(console, panicMark)
	if at < 0 {
		return nil
	}
	reason := console[at+len(panicMark):]
	if end := bytes.IndexByte(reason, '\n'); end >= 0 {
		reason = reason[:end]
	}
	upToPanic := console[:at+len(panicMark)+len(reason)]
	var missing []string
	for _, s := range consoleSigns {
		if s.panic.Match(reason) && s.sign.Match(upToPanic) {
			missing = append(missing, s.option)
		}
	}
	if len(missing) > 0 {
		return &MissingError{Options: missing}
	}
	return nil
}
