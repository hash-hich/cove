package layer

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
)

// dir writes the directory of hdr at p. A directory declared again keeps what it holds and takes
// the new attributes; the root, which always exists, is such a directory.
func (c *applying) dir(hdr *tar.Header, p string) error {
	if n := c.index.lookup(p); n != nil && n.mode.IsDir() {
		if err := c.w.Setattr(p, attrOf(hdr, fs.ModeDir)); err != nil {
			return c.refuse(hdr, err)
		}
		return c.xattrs(hdr, p)
	}
	return c.place(hdr, p, fs.ModeDir, func(a Attr) error { return c.w.Mkdir(p, a) })
}

// file writes the regular file of hdr at p with the body the archive serves for it. A writer
// that leaves part of the body unread would produce a blob that mounts with a truncated file, so
// the loop checks that the body was taken whole.
func (c *applying) file(hdr *tar.Header, p string, body io.Reader) error {
	content := &io.LimitedReader{R: body, N: hdr.Size}
	if err := c.place(hdr, p, 0, func(a Attr) error { return c.w.WriteFile(p, a, hdr.Size, content) }); err != nil {
		return err
	}
	if content.N != 0 {
		return c.refuse(hdr, fmt.Errorf("the writer took %d of the %d bytes of the content", hdr.Size-content.N, hdr.Size))
	}
	return nil
}

// device writes the device node of hdr at p, typ saying character or block.
func (c *applying) device(hdr *tar.Header, p string, typ fs.FileMode) error {
	return c.place(hdr, p, typ, func(a Attr) error { return c.w.Mknod(p, a, hdr.Devmajor, hdr.Devminor) })
}

// link writes at p a second name of the target of hdr, which is relative to the root of the
// archive and must have been written earlier in the layer, the layer being refused otherwise.
// The new name takes the type of
// its target in the index, since both are one inode.
func (c *applying) link(hdr *tar.Header, p string) error {
	target := c.normalize(hdr, "hard link target", hdr.Linkname)
	n := c.index.lookup(target)
	switch {
	case n == nil:
		return c.refuse(hdr, fmt.Errorf("hard link target %s was not written earlier in the layer", target))
	case target == p:
		return c.refuse(hdr, fmt.Errorf("hard link target %s is the entry itself", target))
	case n.mode.IsDir():
		return c.refuse(hdr, fmt.Errorf("hard link target %s is a directory", target))
	}
	return c.place(hdr, p, n.mode, func(_ Attr) error { return c.w.Link(p, target) })
}

// place writes at p, as an entry of type typ, what emit creates with the attributes of hdr, after
// the checks every entry passes: the root stays a directory, the parents are directories or
// nothing, and what stood at p is removed first, the last of two entries of one name winning.
// The entry is then recorded and given its extended attributes.
func (c *applying) place(hdr *tar.Header, p string, typ fs.FileMode, emit func(Attr) error) error {
	if p == "/" && !typ.IsDir() {
		return c.refuse(hdr, errors.New("the root of the layer must be a directory"))
	}
	if err := c.parents(hdr, p); err != nil {
		return err
	}
	if c.index.lookup(p) != nil {
		if err := c.w.RemoveAll(p); err != nil {
			return c.refuse(hdr, err)
		}
	}
	if err := emit(attrOf(hdr, typ)); err != nil {
		return c.refuse(hdr, err)
	}
	c.index.put(p, typ)
	return c.xattrs(hdr, p)
}

// parents refuses the entry of hdr when an ancestor of p was written as something other than a
// directory: a symbolic link there would make the entry land wherever the link points. The
// message names the layer, the entry and the parent in the way.
func (c *applying) parents(hdr *tar.Header, p string) error {
	if parent, n := c.index.obstacle(p); n != nil {
		return c.refuse(hdr, fmt.Errorf("parent %s is %s, not a directory", parent, describe(n.mode)))
	}
	return nil
}

// attrOf returns the attributes of hdr for an entry of type typ: the permission bits with setuid,
// setgid and sticky as the archive declares them, the type as the loop decided it.
func attrOf(hdr *tar.Header, typ fs.FileMode) Attr {
	return Attr{
		Mode:    hdr.FileInfo().Mode()&^fs.ModeType | typ,
		UID:     hdr.Uid,
		GID:     hdr.Gid,
		ModTime: hdr.ModTime,
	}
}

// describe names in a message what an entry of type mode is.
func describe(mode fs.FileMode) string {
	switch {
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeSymlink != 0:
		return "a symbolic link"
	case mode&fs.ModeCharDevice != 0:
		return "a character device"
	case mode&fs.ModeDevice != 0:
		return "a block device"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	default:
		return "a regular file"
	}
}
