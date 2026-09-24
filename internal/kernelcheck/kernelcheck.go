// Package kernelcheck says whether a kernel carries what cove requires of it, read from the
// configuration the kernel embeds. The kernel is replaceable: cove's own is the default, and a
// user may bring one. The requirement is a closed list, checked in the guest before the agent
// starts, so that a kernel that lacks an option is refused with its name instead of failing in
// the middle of a run.
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
	"runtime"
	"strings"
)

// ConfigPath is where a kernel built with IKCONFIG_PROC serves its configuration.
const ConfigPath = "/proc/config.gz"

// requirement is met when any one of its options is built in.
type requirement []string

func (r requirement) String() string { return strings.Join(r, " or ") }

func (r requirement) metBy(builtIn map[string]bool) bool {
	for _, opt := range r {
		if builtIn[opt] {
			return true
		}
	}
	return false
}

// required is everything without which the init or the disks of a run do not work, on every
// architecture. Each option must be built in, since a module would have to come from an image the
// init has not mounted yet. kernel/config/cove.config is where cove's own kernel meets it.
var required = []requirement{
	// The read only disks of the layers and their stacking. The three attribute options carry the
	// extended attributes, ACLs and file capabilities of a layer, which a kernel without them drops
	// without a word.
	{"CONFIG_VIRTIO_BLK"},
	{"CONFIG_EROFS_FS"},
	{"CONFIG_EROFS_FS_XATTR"},
	{"CONFIG_EROFS_FS_POSIX_ACL"},
	{"CONFIG_EROFS_FS_SECURITY"},
	{"CONFIG_OVERLAY_FS"},
	// The write layer and the volumes, which cove writes as ext4.
	{"CONFIG_EXT4_FS"},
	{"CONFIG_EXT4_FS_POSIX_ACL"},
	{"CONFIG_EXT4_FS_SECURITY"},
	// The channel to the host and the network card.
	{"CONFIG_VSOCKETS"},
	{"CONFIG_VIRTIO_VSOCKETS"},
	{"CONFIG_VIRTIO_NET"},
	// A transport for the devices, and a console for the refusal when the channel is missing.
	{"CONFIG_VIRTIO_MMIO", "CONFIG_VIRTIO_PCI"},
	{"CONFIG_VIRTIO_CONSOLE", "CONFIG_SERIAL_8250_CONSOLE", "CONFIG_SERIAL_AMBA_PL011_CONSOLE"},
	// The init in its initramfs, what it finds its disks with, and what it reads this check from.
	// IKCONFIG_PROC is a requirement of the check itself: a kernel without it is refused, though it
	// might have worked, since nothing else says what it carries.
	{"CONFIG_BLK_DEV_INITRD"},
	// The init and this check are Go programs: an ELF, and a runtime that needs futexes, epoll
	// and eventfd before main. A kernel without them is refused by FromConsole on the host, since
	// the init never runs far enough to refuse it.
	{"CONFIG_BINFMT_ELF"},
	{"CONFIG_FUTEX"},
	{"CONFIG_EPOLL"},
	{"CONFIG_EVENTFD"},
	{"CONFIG_PROC_FS"},
	{"CONFIG_DEVTMPFS", "CONFIG_SYSFS"},
	{"CONFIG_IKCONFIG"},
	{"CONFIG_IKCONFIG_PROC"},
}

// requiredOn adds what one architecture asks for. On x86_64, libkrun and Firecracker declare
// their virtio devices on the kernel command line, where arm64 has a device tree.
var requiredOn = map[string][]requirement{
	"amd64": {{"CONFIG_VIRTIO_MMIO_CMDLINE_DEVICES"}},
}

// MissingError names the requirements a kernel does not meet.
type MissingError struct {
	// Options holds the requirements not met in the order of the list, each an option with
	// CONFIG_ first, or its alternatives joined by "or".
	Options []string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("the kernel does not build in %s, which cove requires", strings.Join(e.Options, ", "))
}

// gzipMagic opens every gzip stream, and /proc/config.gz is one.
var gzipMagic = []byte{0x1f, 0x8b}

// CheckRunning checks the kernel the guest runs, from ConfigPath. A kernel that does not serve
// its configuration is refused as missing IKCONFIG_PROC.
func CheckRunning() error {
	return checkFile(ConfigPath, runtime.GOARCH)
}

// Check reads a kernel configuration, compressed as /proc/config.gz serves it or plain as a
// .config, and returns a *MissingError naming every requirement of the architecture of the
// running program that is not met. An option set to m counts as missing.
func Check(r io.Reader) error {
	return checkArch(r, runtime.GOARCH)
}

func checkFile(path, goarch string) error {
	//nolint:gosec // G304: path is ConfigPath, or a file the tests write.
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &MissingError{Options: []string{"CONFIG_IKCONFIG_PROC"}}
	}
	if err != nil {
		return fmt.Errorf("open the kernel configuration: %w", err)
	}
	defer func() { _ = f.Close() }()
	return checkArch(f, goarch)
}

// requirementsOf returns the requirements of goarch: those of every architecture, then its own.
func requirementsOf(goarch string) []requirement {
	reqs := make([]requirement, 0, len(required)+len(requiredOn[goarch]))
	return append(append(reqs, required...), requiredOn[goarch]...)
}

func checkArch(r io.Reader, goarch string) error {
	builtIn, err := readBuiltIn(r)
	if err != nil {
		return err
	}
	var missing []string
	for _, req := range requirementsOf(goarch) {
		if !req.metBy(builtIn) {
			missing = append(missing, req.String())
		}
	}
	if len(missing) > 0 {
		return &MissingError{Options: missing}
	}
	return nil
}

// readBuiltIn returns the options a configuration sets to y, decompressing it first when it is
// a gzip stream.
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
