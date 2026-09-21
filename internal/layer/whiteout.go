package layer

import (
	"archive/tar"
	"io/fs"
	"path"
)

// The names the OCI image spec gives whiteouts, and what overlayfs reads instead.
const (
	// whiteoutPrefix heads an entry that deletes the name that follows it in the layers below.
	whiteoutPrefix = ".wh."
	// reservedPrefix heads the whiteouts the OCI image spec reserves. The opaque marker is the
	// only one that means something; the others are ignored, as containerd does.
	reservedPrefix = ".wh..wh."
	// opaqueMarker, in a directory, hides everything the layers below hold under it.
	opaqueMarker = ".wh..wh..opq"
	// opaqueXattr and opaqueValue are the attribute overlayfs reads on an opaque directory: it
	// hides what the layers below hold under that directory.
	opaqueXattr = "trusted.overlay.opaque"
	opaqueValue = "y"
)

// whiteout writes the whiteout of name in dir as overlayfs reads it: a character device 0:0 of
// that name, with the attributes of the entry. The name goes through the same normalization as
// any other: it is a base name, so only . and .. can move it, and .. from the root lands
// on the root, which no whiteout can be.
func (c *applying) whiteout(hdr *tar.Header, dir, name string) error {
	p := c.normalize(hdr, "whiteout target", dir[1:]+"/"+name)
	return c.place(hdr, p, fs.ModeDevice|fs.ModeCharDevice, func(a Attr) error { return c.w.Mknod(p, a, 0, 0) })
}

// opaque marks the directory of the marker at p as opaque, the attribute overlayfs reads;
// the marker itself is nothing to write. The directory may come later in the archive, or never:
// the writer makes it then, and the index records it as it records the parent of an entry.
func (c *applying) opaque(hdr *tar.Header, p string) error {
	if err := c.parents(hdr, p); err != nil {
		return err
	}
	dir := path.Dir(p)
	if err := c.w.Setxattr(dir, opaqueXattr, opaqueValue); err != nil {
		return c.refuse(hdr, err)
	}
	c.index.put(dir, fs.ModeDir)
	return nil
}
