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
	"strings"
)

// required is what the init needs before any root of the image exists: the disks of the layers,
// their stacking, the channel to the host and the network card. Each must be built in, since a
// module would have to come from an image the init has not mounted yet. The three attribute
// options of EROFS are there because a kernel without them drops the extended attributes, ACLs
// and file capabilities of a layer without a word.
//
// Two options come before the check itself and are not on the list: BLK_DEV_INITRD, without
// which the init never runs, and IKCONFIG_PROC, without which there is nothing to read.
var required = []string{
	"CONFIG_VIRTIO_BLK",
	"CONFIG_EROFS_FS",
	"CONFIG_EROFS_FS_XATTR",
	"CONFIG_EROFS_FS_POSIX_ACL",
	"CONFIG_EROFS_FS_SECURITY",
	"CONFIG_OVERLAY_FS",
	"CONFIG_VSOCKETS",
	"CONFIG_VIRTIO_VSOCKETS",
	"CONFIG_VIRTIO_NET",
}

// MissingError names the required options a kernel does not build in.
type MissingError struct {
	// Options holds the missing options in the order of the list, each with CONFIG_ first.
	Options []string
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("the kernel does not build in %s, which cove requires", strings.Join(e.Options, ", "))
}

// gzipMagic opens every gzip stream, and /proc/config.gz is one.
var gzipMagic = []byte{0x1f, 0x8b}

// Check reads a kernel configuration, compressed as /proc/config.gz serves it or plain as a
// .config, and returns a *MissingError naming every required option that is not built in. An
// option set to m counts as missing.
func Check(r io.Reader) error {
	builtIn, err := readBuiltIn(r)
	if err != nil {
		return err
	}
	var missing []string
	for _, opt := range required {
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
