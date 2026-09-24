// Package initramfs writes the initramfs a sandbox boots with: the init of cove and the
// description of the run, as archives in the newc format of cpio, the one the kernel unpacks.
//
// The kernel unpacks archives laid end to end, one after the other, into the same root. So the
// initramfs of a run is two of them: the base, which holds the init and depends on the version of
// cove only, then the description of that run. The base is written once and each run appends a
// few hundred bytes to it.
//
// The archives are uncompressed, since the guest kernel builds in no decompressor, and
// reproducible: every entry is owned by root and dated zero, and inode numbers follow the order
// of the entries.
package initramfs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"gitlab.com/hich-hich/cove/internal/vminit/spec"
)

// The types of an entry, as the mode of a newc header carries them.
const (
	typeDir     = 0o040000
	typeRegular = 0o100000
	typeChar    = 0o020000
)

// trailer is the name of the entry that ends an archive.
const trailer = "TRAILER!!!"

// WriteBase writes the base archive: the init, from the Linux executable init, and what the kernel
// needs before the init runs. /dev/console is there because the kernel opens it for the standard
// streams of the init before any /dev is mounted, and without it the init starts with none.
func WriteBase(w io.Writer, init []byte) error {
	a := NewWriter(w)
	for _, d := range []string{"dev", "proc", "sys", path.Dir(strings.TrimPrefix(spec.Path, "/"))} {
		if err := a.Dir(d, 0o755); err != nil {
			return err
		}
	}
	// The console is character device 5:1 on every architecture.
	if err := a.CharDevice("dev/console", 0o600, 5, 1); err != nil {
		return err
	}
	if err := a.File("init", 0o755, init); err != nil {
		return err
	}
	return a.Close()
}

// WriteRun writes the archive that follows the base: the description of one run, at
// spec.Path.
func WriteRun(w io.Writer, s *spec.Run) error {
	var buf bytes.Buffer
	if err := s.Encode(&buf); err != nil {
		return err //nolint:wrapcheck // Encode says it failed to write the description.
	}
	a := NewWriter(w)
	if err := a.File(strings.TrimPrefix(spec.Path, "/"), 0o644, buf.Bytes()); err != nil {
		return err
	}
	return a.Close()
}

// Writer writes one archive in the newc format.
type Writer struct {
	w   io.Writer
	ino uint32
}

// NewWriter returns a Writer of an archive on w, which Close ends.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// Dir writes the directory name, a path relative to the root without a leading slash.
func (a *Writer) Dir(name string, perm fs.FileMode) error {
	return a.entry(name, typeDir|uint32(perm.Perm()), 2, 0, 0, nil)
}

// File writes the regular file name holding data.
func (a *Writer) File(name string, perm fs.FileMode, data []byte) error {
	return a.entry(name, typeRegular|uint32(perm.Perm()), 1, 0, 0, data)
}

// CharDevice writes the character device name, of numbers major and minor.
func (a *Writer) CharDevice(name string, perm fs.FileMode, major, minor uint32) error {
	return a.entry(name, typeChar|uint32(perm.Perm()), 1, major, minor, nil)
}

// Close writes the trailer that ends the archive. It does not close the underlying writer.
func (a *Writer) Close() error {
	return a.write(trailer, 0, 1, 0, 0, nil)
}

func (a *Writer) entry(name string, mode, nlink, major, minor uint32, data []byte) error {
	if name == "" || name == trailer || path.IsAbs(name) || path.Clean(name) != name ||
		name == ".." || strings.HasPrefix(name, "../") {
		return fmt.Errorf("the initramfs cannot hold an entry named %q", name)
	}
	return a.write(name, mode, nlink, major, minor, data)
}

// write writes one header, its name and its data, each padded to four bytes as newc demands. The
// header is thirteen fields of eight hexadecimal digits after the magic: inode, mode, owner, group,
// links, date, size, the device holding the entry, the device the entry is, the size of the name
// with its NUL, and a checksum newc leaves at zero.
func (a *Writer) write(name string, mode, nlink, major, minor uint32, data []byte) error {
	if uint64(len(data)) > 0xffffffff {
		return errors.New("the initramfs cannot hold a file of 4 GiB or more")
	}
	a.ino++
	var buf bytes.Buffer
	_, _ = fmt.Fprintf(&buf, "070701%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x",
		a.ino, mode, 0, 0, nlink, 0, len(data), 0, 0, major, minor, len(name)+1, 0)
	_, _ = buf.WriteString(name)
	_ = buf.WriteByte(0)
	pad(&buf, buf.Len())
	_, _ = buf.Write(data)
	pad(&buf, len(data))
	if _, err := a.w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write the initramfs: %w", err)
	}
	return nil
}

// pad appends to buf the zeros that round n up to a multiple of four.
func pad(buf *bytes.Buffer, n int) {
	_, _ = buf.Write(make([]byte, (4-n%4)%4))
}
