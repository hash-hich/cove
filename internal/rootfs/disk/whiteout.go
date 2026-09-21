package disk

import (
	"archive/tar"
	"fmt"
	"path"
	"strings"
)

// What the overlay convention writes in an archive to say that something disappears, and what
// overlayfs reads in an image instead.
const (
	whiteoutPrefix = ".wh."
	whiteoutMeta   = ".wh..wh."
	opaqueMarker   = ".wh..wh..opq"
	opaqueXattr    = "trusted.overlay.opaque"
	opaqueValue    = "y"
)

// marker is what the name of an entry marks.
type marker uint8

const (
	// notAMarker is an entry whose name has nothing of a whiteout.
	notAMarker marker = iota
	// deletion is .wh.<name>: <name> goes, and a character device 0:0 of that name says so.
	deletion
	// opaque is .wh..wh..opq: the directory holding it hides everything the layers below put in
	// it, and an attribute on that directory says so.
	opaque
	// ignored is any other .wh..wh.*, the bookkeeping aufs left behind and overlayfs never
	// reads; containerd drops it and so does cove.
	ignored
)

// whiteoutOf reads the base name of p and returns what it marks, on the path it marks it: the
// name that goes for a deletion, the directory for an opaque marker, p itself for the rest.
//
// The name under the prefix went through the same normalization as p, since it is a part of it.
func whiteoutOf(p string) (string, marker) {
	dir, base := path.Split(p)
	switch {
	case base == opaqueMarker:
		return path.Clean(dir), opaque
	case strings.HasPrefix(base, whiteoutMeta):
		return p, ignored
	case strings.HasPrefix(base, whiteoutPrefix):
		// A bare ".wh." names nothing under the prefix, so it marks the directory holding it,
		// which is what containerd deletes when it reads one.
		return path.Join(dir, strings.TrimPrefix(base, whiteoutPrefix)), deletion
	}
	return p, notAMarker
}

// deleted turns a .wh.<name> marker into what overlayfs reads as a deletion: a character device
// 0:0 carrying the name that goes. What the marker declares of mode, owner and time is kept:
// overlayfs looks at the device number alone, and keeping the declaration keeps the entry a
// function of the archive.
func deleted(hdr *tar.Header, p string) Entry {
	return Entry{
		Path:    p,
		Kind:    CharDevice,
		Mode:    hdr.Mode & modeBits,
		UID:     hdr.Uid,
		GID:     hdr.Gid,
		ModTime: hdr.ModTime,
	}
}

// opaque puts on a directory the attribute overlayfs reads as "nothing below shows through".
// Nothing else of the marker is kept: it is a name in the archive, not an entry of the image.
func (cv *conversion) opaque(name, dir string) error {
	if err := cv.hangsFromDirs(name, path.Join(dir, opaqueMarker)); err != nil {
		return err
	}
	if err := cv.out.Setxattr(dir, opaqueXattr, []byte(opaqueValue)); err != nil {
		return fmt.Errorf("layer %s: %s: mark %s opaque: %w", cv.Layer, name, dir, err)
	}
	return nil
}
