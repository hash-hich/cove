package erofs

import (
	"fmt"
	"io"
	"io/fs"

	goerofs "github.com/erofs/go-erofs"

	"gitlab.com/hich-hich/cove/internal/layer"
)

// The type and mode bits as the kernel writes them in an inode, which EROFS stores as they are.
// Go names the same things in fs.FileMode with bits of its own, so the writer translates rather
// than passes through.
const (
	modeFIFO   = 0o010000
	modeChar   = 0o020000
	modeDir    = 0o040000
	modeBlock  = 0o060000
	modeReg    = 0o100000
	modeLink   = 0o120000
	modeSock   = 0o140000
	modeSetuid = 0o4000
	modeSetgid = 0o2000
	modeSticky = 0o1000
)

// defaultDirMode is what a directory nothing was declared for is given: the mode go-erofs gives
// the parents it makes on the way, so that a directory the archive never named looks the same
// whether the writer made it or this package did.
const defaultDirMode fs.FileMode = 0o755

// writer turns the entries of one layer into an EROFS image. It is the Writer internal/layer
// hands each entry to, and it holds no rule of its own: what is written, and under which name,
// was decided there. Its one job besides calling the library is to carry an attribute of the
// archive to the field of the inode that holds it.
type writer struct {
	w *goerofs.Writer
	// entries counts what the layer holds: one per name created. What is set on an entry
	// afterwards is not one more entry.
	entries int
}

// Mkdir creates the directory name with attr.
func (b *writer) Mkdir(name string, attr layer.Attr) error {
	// Mkdir keeps the permission bits of the mode, setuid, setgid and sticky included, and
	// forces the type to directory.
	if err := b.w.Mkdir(name, attr.Mode); err != nil {
		return fmt.Errorf("create the directory: %w", err)
	}
	b.entries++
	return b.own(name, attr)
}

// Setattr sets attr on the entry name, which exists.
func (b *writer) Setattr(name string, attr layer.Attr) error {
	if err := b.w.Chmod(name, attr.Mode); err != nil {
		return fmt.Errorf("set the mode: %w", err)
	}
	return b.own(name, attr)
}

// WriteFile creates the regular file name with attr and the size bytes of content.
func (b *writer) WriteFile(name string, attr layer.Attr, _ int64, content io.Reader) error {
	f, err := b.w.Create(name)
	if err != nil {
		return fmt.Errorf("create the file: %w", err)
	}
	// ReadFrom, and not io.Copy, so that the buffer the writer already holds is the one used:
	// a copy of its own would allocate 32 KiB per file of the layer.
	if _, err := f.ReadFrom(content); err != nil {
		_ = f.Close()
		return fmt.Errorf("write the content: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close the file: %w", err)
	}
	b.entries++
	return b.Setattr(name, attr)
}

// Symlink creates the symbolic link name with attr, pointing to target as the archive wrote it.
func (b *writer) Symlink(name string, attr layer.Attr, target string) error {
	if err := b.w.Symlink(target, name); err != nil {
		return fmt.Errorf("create the symbolic link: %w", err)
	}
	b.entries++
	// The mode of the archive is not carried over: the kernel ignores the permissions of a
	// symbolic link, mkfs.erofs writes 0777 for every one of them, and following it keeps the
	// blob equal to what the oracle of the tests produces.
	return b.own(name, attr)
}

// Link gives the entry target, written earlier and not a directory, the second name name.
func (b *writer) Link(name, target string) error {
	if err := b.w.Link(target, name); err != nil {
		return fmt.Errorf("create the hard link: %w", err)
	}
	b.entries++
	return nil
}

// Mknod creates the character device, block device or named pipe name.
func (b *writer) Mknod(name string, attr layer.Attr, major, minor int64) error {
	if err := b.w.Mknod(name, unixMode(attr.Mode), encodeDev(major, minor)); err != nil {
		return fmt.Errorf("create the device node: %w", err)
	}
	b.entries++
	return b.own(name, attr)
}

// Setxattr sets the extended attribute xattr of the entry name to value. The marker of an opaque
// directory may come before the directory it marks, or without it, so a name nothing was written
// at becomes a directory here, as it would if an entry under it had been written first.
func (b *writer) Setxattr(name, xattr, value string) error {
	if _, err := b.w.Lstat(name); err != nil {
		if err := b.w.Mkdir(name, defaultDirMode); err != nil {
			return fmt.Errorf("create the directory: %w", err)
		}
	}
	if err := b.w.Setxattr(name, xattr, value); err != nil {
		return fmt.Errorf("set the extended attribute: %w", err)
	}
	return nil
}

// RemoveAll removes the entry name and, for a directory, what it holds.
func (b *writer) RemoveAll(name string) error {
	if err := b.w.RemoveAll(name); err != nil {
		return fmt.Errorf("remove what stood there: %w", err)
	}
	return nil
}

// own gives the entry name the owner and the time of attr. The access time is set to the
// modification time: EROFS stores the second only, and a value of the clock of the host would
// make the blob depend on when it was written.
func (b *writer) own(name string, attr layer.Attr) error {
	if err := b.w.Chown(name, attr.UID, attr.GID); err != nil {
		return fmt.Errorf("set the owner: %w", err)
	}
	if err := b.w.Chtimes(name, attr.ModTime, attr.ModTime); err != nil {
		return fmt.Errorf("set the time: %w", err)
	}
	return nil
}

// unixMode returns mode as the kernel writes it in an inode: the type, the permission bits, and
// setuid, setgid and sticky.
func unixMode(mode fs.FileMode) uint16 {
	//nolint:gosec // G115: the permission bits of a mode hold on nine bits.
	m := uint16(mode.Perm())
	// The cases are the types EROFS stores, which are the types a layer holds; the other bits of
	// a mode say nothing of the type and are read below.
	//nolint:exhaustive // The type of a mode is one value, and the default covers a regular file.
	switch mode.Type() {
	case fs.ModeDir:
		m |= modeDir
	case fs.ModeSymlink:
		m |= modeLink
	case fs.ModeDevice | fs.ModeCharDevice:
		m |= modeChar
	case fs.ModeDevice:
		m |= modeBlock
	case fs.ModeNamedPipe:
		m |= modeFIFO
	case fs.ModeSocket:
		m |= modeSock
	default:
		m |= modeReg
	}
	if mode&fs.ModeSetuid != 0 {
		m |= modeSetuid
	}
	if mode&fs.ModeSetgid != 0 {
		m |= modeSetgid
	}
	if mode&fs.ModeSticky != 0 {
		m |= modeSticky
	}
	return m
}

// encodeDev returns the device of major and minor as EROFS stores it, which the guest reads back
// with new_decode_dev: the low eight bits of the minor, the major above them, and what is left of
// the minor above that. The numbers are those the archive declared, and a major or a minor too
// large for the encoding loses its high bits here as it would anywhere else in Linux.
func encodeDev(major, minor int64) uint32 {
	return uint32(minor&0xff) | uint32(major&0xfff)<<8 | uint32(minor&0xfffff00)<<12
}
