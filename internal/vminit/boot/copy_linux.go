package boot

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

// copyTree copies the directory src into dst, which it creates, as the image holds it: owners,
// modes, extended attributes, dates, links both symbolic and hard, and special files. A symbolic
// link is copied as a link and never followed.
func copyTree(src, dst string) error {
	c := &copier{src: src, dst: dst, linked: make(map[inode]string)}
	if err := filepath.WalkDir(src, c.visit); err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	// The dates of a directory are set once its entries are written, since writing them moves it.
	for _, d := range slices.Backward(c.dirs) {
		if err := setTimes(d.path, &d.st); err != nil {
			return err
		}
	}
	return nil
}

type inode struct{ dev, ino uint64 }

type dir struct {
	path string
	st   unix.Stat_t
}

// copier is one copy of a tree: the files met with more than one link, by the name the first was
// copied under, and the directories written, whose dates are set last.
type copier struct {
	src, dst string
	linked   map[inode]string
	dirs     []dir
}

// link makes to a hard link to the copy of a file already met under another name, and reports
// whether it did. It remembers a file with several links the first time it meets it.
func (c *copier) link(to string, st *unix.Stat_t) (bool, error) {
	if st.Nlink < 2 || st.Mode&unix.S_IFMT == unix.S_IFDIR {
		return false, nil
	}
	key := inode{st.Dev, st.Ino}
	first, ok := c.linked[key]
	if !ok {
		c.linked[key] = to
		return false, nil
	}
	if err := os.Link(first, to); err != nil {
		return false, fmt.Errorf("link %s: %w", to, err)
	}
	return true, nil
}

func (c *copier) visit(p string, _ fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(c.src, p)
	if err != nil {
		return err //nolint:wrapcheck // WalkDir hands p under src, so it never fails.
	}
	to := filepath.Join(c.dst, rel)
	var st unix.Stat_t
	if err := unix.Lstat(p, &st); err != nil {
		return fmt.Errorf("read %s: %w", p, err)
	}
	isDir := st.Mode&unix.S_IFMT == unix.S_IFDIR
	if linked, err := c.link(to, &st); linked || err != nil {
		return err
	}
	if err := create(p, to, &st); err != nil {
		return err
	}
	if err := copyMeta(p, to, &st); err != nil {
		return err
	}
	if isDir {
		c.dirs = append(c.dirs, dir{to, st})
		return nil
	}
	return setTimes(to, &st)
}

// create creates at to an entry of the type of the one at from, with its contents.
func create(from, to string, st *unix.Stat_t) error {
	mode := st.Mode & 0o7777
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		if err := unix.Mkdir(to, mode); err != nil {
			return fmt.Errorf("create %s: %w", to, err)
		}
	case unix.S_IFREG:
		return copyFile(from, to, mode)
	case unix.S_IFLNK:
		target, err := os.Readlink(from)
		if err != nil {
			return fmt.Errorf("read %s: %w", from, err)
		}
		if err := os.Symlink(target, to); err != nil {
			return fmt.Errorf("create %s: %w", to, err)
		}
	default:
		if err := unix.Mknod(to, st.Mode, int(st.Rdev)); err != nil { //nolint:gosec // G115: a device number fits.
			return fmt.Errorf("create %s: %w", to, err)
		}
	}
	return nil
}

func copyFile(from, to string, mode uint32) error {
	//nolint:gosec // G304: from is a file of the image the init copies into a volume.
	in, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("read %s: %w", from, err)
	}
	defer func() { _ = in.Close() }()
	//nolint:gosec // G304: to is under the directory of a volume on the write disk.
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.FileMode(mode&0o777))
	if err != nil {
		return fmt.Errorf("create %s: %w", to, err)
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	return nil
}

// copyMeta gives to the owner, the mode and the extended attributes of from. The owner comes
// first, since a change of owner clears the set-id bits and the file capabilities.
func copyMeta(from, to string, st *unix.Stat_t) error {
	if err := unix.Lchown(to, int(st.Uid), int(st.Gid)); err != nil {
		return fmt.Errorf("give %s its owner: %w", to, err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFLNK {
		if err := unix.Chmod(to, st.Mode&0o7777); err != nil {
			return fmt.Errorf("give %s its mode: %w", to, err)
		}
	}
	return copyXattrs(from, to)
}

func copyXattrs(from, to string) error {
	names, err := xattrNames(from)
	if err != nil {
		return err
	}
	for _, n := range names {
		v, err := xattrValue(from, n)
		if err != nil {
			return err
		}
		if err := unix.Lsetxattr(to, n, v, 0); err != nil {
			return fmt.Errorf("give %s its attribute %s: %w", to, n, err)
		}
	}
	return nil
}

func xattrNames(p string) ([]string, error) {
	size, err := unix.Llistxattr(p, nil)
	if errors.Is(err, unix.ENOTSUP) {
		return nil, nil
	}
	if err != nil || size == 0 {
		return nil, errOf("list the attributes of", p, err)
	}
	buf := make([]byte, size)
	size, err = unix.Llistxattr(p, buf)
	if err != nil {
		return nil, errOf("list the attributes of", p, err)
	}
	var names []string
	for n := range bytes.SplitSeq(buf[:size], []byte{0}) {
		if len(n) > 0 {
			names = append(names, string(n))
		}
	}
	return names, nil
}

func xattrValue(p, name string) ([]byte, error) {
	size, err := unix.Lgetxattr(p, name, nil)
	if err != nil {
		return nil, errOf("read the attribute "+name+" of", p, err)
	}
	buf := make([]byte, size)
	size, err = unix.Lgetxattr(p, name, buf)
	if err != nil {
		return nil, errOf("read the attribute "+name+" of", p, err)
	}
	return buf[:size], nil
}

func errOf(what, p string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %s: %w", what, p, err)
}

func setTimes(p string, st *unix.Stat_t) error {
	ts := []unix.Timespec{st.Atim, st.Mtim}
	if err := unix.UtimesNanoAt(unix.AT_FDCWD, p, ts, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("date %s: %w", p, err)
	}
	return nil
}
