// Package kernelcheck says whether a kernel carries what cove requires of it, read from the
// configuration the kernel embeds. The kernel is replaceable: cove's own is the default, and a
// user may bring one. The requirement is a closed list, so that a kernel that lacks an option is
// refused with its name instead of failing in the middle of a run.
//
// The list is checked on the host, in the file of the kernel, before the VM exists
// (CheckKernelFile). A kernel whose configuration cannot be read there, a compressed image, is
// checked in the guest instead (CheckRunning), and what the guest cannot report, a kernel that
// never runs the init, is read on the console (FromConsole).
//
// The list says nothing of what the agent may do: an option compiled in is what the kernel
// permits, and a container engine the image brings checks its own needs.
package kernelcheck

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ConfigPath is where a kernel built with IKCONFIG_PROC serves its configuration.
const ConfigPath = "/proc/config.gz"

// procFS is what a kernel without /proc lacks, named by the guest check and by the init's mount.
const procFS = "CONFIG_PROC_FS"

// required is what the init and the disks of a run need on every kernel, whatever the backend.
// It equals what the init does unconditionally: a file system it mounts, a device it opens, a
// system call its runtime makes before main. Whatever the init mounts only when present, a cgroup2
// hierarchy for instance, is not here, and the init tolerates its absence. Each option must be
// built in, since a module would have to come from an image the init has not mounted yet.
// kernel/config/cove.config is where cove's own kernel meets the list, and the two move together.
var required = []string{
	// The read only disks of the layers and their stacking. The three attribute options carry the
	// extended attributes, ACLs and file capabilities of a layer, which a kernel without them drops
	// without a word.
	"CONFIG_VIRTIO_BLK",
	"CONFIG_EROFS_FS",
	"CONFIG_EROFS_FS_XATTR",
	"CONFIG_EROFS_FS_POSIX_ACL",
	"CONFIG_EROFS_FS_SECURITY",
	"CONFIG_OVERLAY_FS",
	// The write layer and the volumes, which cove writes as ext4.
	"CONFIG_EXT4_FS",
	"CONFIG_EXT4_FS_POSIX_ACL",
	"CONFIG_EXT4_FS_SECURITY",
	// The channel to the host and the network card, over the one transport both backends use.
	"CONFIG_VSOCKETS",
	"CONFIG_VIRTIO_VSOCKETS",
	"CONFIG_VIRTIO_NET",
	"CONFIG_VIRTIO_MMIO",
	// The init in its initramfs, an ELF whose Go runtime needs futexes, epoll and eventfd.
	"CONFIG_BLK_DEV_INITRD",
	"CONFIG_BINFMT_ELF",
	"CONFIG_FUTEX",
	"CONFIG_EPOLL",
	"CONFIG_EVENTFD",
	// What the init mounts: /proc, /sys, /dev and its disks, /dev/pts for the terminal of an
	// attached turn, tmpfs for /dev/shm.
	procFS,
	"CONFIG_SYSFS",
	"CONFIG_DEVTMPFS",
	"CONFIG_UNIX98_PTYS",
	"CONFIG_TMPFS",
	// What this check reads in the guest. IKCONFIG_PROC is a requirement of the check itself: a
	// kernel without it is refused, though it might have worked, since nothing else says what it
	// carries once the host could not read it.
	"CONFIG_IKCONFIG",
	"CONFIG_IKCONFIG_PROC",
}

// consoleOptions maps the console the command line names to what the kernel needs to have it:
// the refusal of the init goes to /dev/console, and a kernel that carries another console than
// the one the backend gives writes it to a port that is not there. On arm64 an 8250 is found in
// the device tree.
var consoleOptions = []struct {
	prefix, goarch string
	options        []string
}{
	{"hvc", "", []string{"CONFIG_VIRTIO_CONSOLE"}},
	{"ttyS", "arm64", []string{"CONFIG_SERIAL_8250_CONSOLE", "CONFIG_SERIAL_OF_PLATFORM"}},
	{"ttyS", "", []string{"CONFIG_SERIAL_8250_CONSOLE"}},
	{"ttyAMA", "", []string{"CONFIG_SERIAL_AMBA_PL011_CONSOLE"}},
}

// requirementsOf returns the requirements of a kernel booted with cmdline on goarch: the list,
// the console the command line names, and the parsing of the virtio devices it declares.
func requirementsOf(cmdline, goarch string) ([]string, error) {
	var console string
	mmioOnCmdline := false
	for _, param := range strings.Fields(cmdline) {
		if v, ok := strings.CutPrefix(param, "console="); ok {
			// The last console named is /dev/console.
			console = v
		}
		if strings.HasPrefix(param, "virtio_mmio.device=") {
			mmioOnCmdline = true
		}
	}
	reqs := append([]string(nil), required...)
	if mmioOnCmdline {
		reqs = append(reqs, "CONFIG_VIRTIO_MMIO_CMDLINE_DEVICES")
	}
	for _, c := range consoleOptions {
		if console != "" && strings.HasPrefix(console, c.prefix) && (c.goarch == "" || c.goarch == goarch) {
			return append(reqs, c.options...), nil
		}
	}
	return nil, fmt.Errorf("the kernel command line names no console cove knows, hvc, ttyS or ttyAMA: %q", cmdline)
}

// MissingError names the requirements a kernel does not meet.
type MissingError struct {
	// Options holds the options not built in, in the order of the list, each with CONFIG_ first.
	Options []string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("the kernel does not build in %s, which cove requires", strings.Join(e.Options, ", "))
}

// gzipMagic opens every gzip stream, and /proc/config.gz is one.
var gzipMagic = []byte{0x1f, 0x8b}

// Check reads a kernel configuration, compressed as /proc/config.gz serves it or plain as a
// .config, and returns a *MissingError naming every option a kernel booted with cmdline on the
// architecture of the running program needs and does not build in. An option set to m counts as
// missing.
func Check(r io.Reader, cmdline string) error {
	return checkArch(r, cmdline, runtime.GOARCH)
}

// CheckRunning checks the kernel the guest runs, from ConfigPath and the command line it was
// booted with. A kernel without /proc is refused as missing PROC_FS, one that does not serve its
// configuration as missing IKCONFIG_PROC.
func CheckRunning() error {
	return checkRunningAt("/", runtime.GOARCH)
}

// checkRunningAt checks the kernel whose /proc is mounted under root.
func checkRunningAt(root, goarch string) error {
	// Without /proc every file below is missing, and IKCONFIG_PROC would take the blame.
	if _, err := os.Stat(filepath.Join(root, "proc/self")); errors.Is(err, fs.ErrNotExist) {
		return &MissingError{Options: []string{procFS}}
	}
	//nolint:gosec // G304: root is / in the guest, a directory the tests lay out otherwise.
	cmdline, err := os.ReadFile(filepath.Join(root, "proc/cmdline"))
	if err != nil {
		return fmt.Errorf("read the kernel command line: %w", err)
	}
	//nolint:gosec // G304: root is / in the guest, a directory the tests lay out otherwise.
	f, err := os.Open(filepath.Join(root, ConfigPath))
	if errors.Is(err, fs.ErrNotExist) {
		return &MissingError{Options: []string{"CONFIG_IKCONFIG_PROC"}}
	}
	if err != nil {
		return fmt.Errorf("open the kernel configuration: %w", err)
	}
	defer func() { _ = f.Close() }()
	return checkArch(f, string(cmdline), goarch)
}

func checkArch(r io.Reader, cmdline, goarch string) error {
	reqs, err := requirementsOf(cmdline, goarch)
	if err != nil {
		return err
	}
	builtIn, err := readBuiltIn(r)
	if err != nil {
		return err
	}
	var missing []string
	for _, opt := range reqs {
		if !builtIn[opt] {
			missing = append(missing, opt)
		}
	}
	if len(missing) > 0 {
		return &MissingError{Options: missing}
	}
	return nil
}

// readBuiltIn returns the options a configuration sets to y, decompressing it first when it is
// a gzip stream. The stream is read as one member, which lets it be followed by other bytes, as
// it is inside the file of a kernel.
func readBuiltIn(r io.Reader) (map[string]bool, error) {
	br := bufio.NewReader(r)
	head, err := br.Peek(len(gzipMagic))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read the kernel configuration: %w", err)
	}
	var src io.Reader = br
	if bytes.Equal(head, gzipMagic) {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("decompress the kernel configuration: %w", err)
		}
		zr.Multistream(false)
		defer func() { _ = zr.Close() }()
		src = zr
	}

	builtIn := make(map[string]bool)
	sc := bufio.NewScanner(src)
	for sc.Scan() {
		name, value, ok := strings.Cut(sc.Text(), "=")
		if ok && value == "y" {
			builtIn[name] = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read the kernel configuration: %w", err)
	}
	return builtIn, nil
}
