// Package unpack turns the archive of an OCI layer into the entries an EROFS writer writes.
//
// The loop reads a tar stream already decompressed and emits, entry by entry, what the blob of the
// layer holds, to a Writer the EROFS writer implements. It is the one part of the image path that
// decides anything: which name is bounded, which entry is impossible, what a whiteout becomes,
// what an extended attribute is worth. Nothing downstream corrects it, and its faults are silent:
// a blob that lost an opaque directory mounts and serves files that should have vanished. Each
// rule is stated where it is applied: what a name becomes in normalize, what a whiteout becomes in
// whiteout and opaque, what an extended attribute is worth in xattrs, and the checks every entry
// passes before it is written in place.
//
// Names are handled with path and strings only. path/filepath carries the rules of the host,
// Windows included, and a rule of the linter keeps it out of the package. In the binary, the
// //go:debug tarinsecurepath=0 of the main package makes archive/tar flag a name that is absolute
// or climbs above the root; the loop takes the header anyway, since its normalization is what
// decides, and the flag is a belt over it.
package unpack

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// Writer receives the entries of a layer in the order of its archive. Every name is absolute and
// clean, and its parents are directories: declared earlier, or made by the writer on the way with
// its own defaults, as extracting the archive would make them. What stood at a name is removed by
// RemoveAll before the name is written again, so that a call never finds its name taken; the one
// exception is a directory declared again, which keeps what it holds and gets Setattr.
type Writer interface {
	// Mkdir creates the directory name with attr.
	Mkdir(name string, attr Attr) error
	// Setattr sets attr on the directory name, which exists: the root, a directory declared
	// earlier, or one the writer made on the way.
	Setattr(name string, attr Attr) error
	// WriteFile creates the regular file name with attr and the size bytes of content, which it
	// reads whole.
	WriteFile(name string, attr Attr, size int64, content io.Reader) error
	// Symlink creates the symbolic link name with attr, pointing to target as the archive wrote
	// it: absolute, climbing above the root, or dangling. A link is never resolved and never
	// refused; where it lands is read in the guest, under the root of the mounted image.
	Symlink(name string, attr Attr, target string) error
	// Link gives the entry target, written earlier and not a directory, the second name name.
	Link(name, target string) error
	// Mknod creates the character device, block device or named pipe name, which the type of
	// attr.Mode tells apart, with the major and minor numbers of the device as the archive
	// declares them.
	Mknod(name string, attr Attr, major, minor int64) error
	// Setxattr sets the extended attribute xattr of the entry name to value, whose bytes are
	// those of the archive, NUL and non UTF-8 included. The marker of an opaque directory may
	// come before the directory in the archive, or without it: name is then a directory nothing
	// was written at, which the writer makes as it makes the parent of an entry.
	Setxattr(name, xattr, value string) error
	// RemoveAll removes the entry name and, for a directory, what it holds.
	RemoveAll(name string) error
}

// Attr is what an entry carries besides its content, as the archive declares it: what is not
// suspect is written as it stands, the owner, the mode, setuid and the numbers of a device
// included.
type Attr struct {
	// Mode holds the type of the entry and its permission bits, setuid, setgid and sticky
	// included.
	Mode fs.FileMode
	// UID and GID are the numeric owner. The names the archive may carry next to them are not
	// kept: EROFS stores numbers.
	UID int
	GID int
	// ModTime is the modification time, the one time EROFS stores.
	ModTime time.Time
}

// Counts is what the unpacking of a layer counted: what meta.json and the report carry.
type Counts struct {
	// NormalizedEntries counts the names bounded to the root, each said on the log. It is what
	// meta.json carries as normalized_entries.
	NormalizedEntries int
	// UnknownXattrPrefixes counts the extended attributes written under a name EROFS does not
	// read, each said on the log. It is what meta.json carries as unknown_xattr_prefixes.
	UnknownXattrPrefixes int
}

// EntryError is why a layer was refused: the layer, the entry as the archive names it, and the
// cause.
type EntryError struct {
	Layer string
	Entry string
	Err   error
}

func (e *EntryError) Error() string {
	return fmt.Sprintf("layer %s: entry %q: %v", e.Layer, e.Entry, e.Err)
}

func (e *EntryError) Unwrap() error {
	return e.Err
}

// Layer reads archive, the tar stream of the layer named layer already decompressed, and writes
// its entries to w in the order of the archive: each name normalized, each whiteout turned into
// what overlayfs reads, each extended attribute passed as the archive holds it. Every name bounded
// to the root and every extended attribute EROFS will not read is said on log, one line each, and
// counted in the result; a nil log keeps quiet. The layer is refused, the error naming it, the
// entry and the cause, when an entry cannot be written as declared: a parent that is not a
// directory, a hard link to what was not written earlier, a type no layer holds, an archive that
// cannot be read, or a writer that fails.
func Layer(layer string, archive io.Reader, w Writer, log io.Writer) (Counts, error) {
	c := &unpacking{layer: layer, w: w, log: log, index: newIndex()}
	tr := tar.NewReader(archive)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return c.counts, nil
		}
		// With the belt on, the reader flags a name that is absolute or climbs and hands the
		// header over with the flag. The normalization of the entry is what decides.
		if err != nil && !errors.Is(err, tar.ErrInsecurePath) {
			return Counts{}, fmt.Errorf("layer %s: read the archive: %w", layer, err)
		}
		if err := c.entry(hdr, tr); err != nil {
			return Counts{}, err
		}
	}
}

// unpacking is the state of one layer being unpacked.
type unpacking struct {
	layer  string
	w      Writer
	log    io.Writer
	counts Counts
	index  index
}

// entry writes the entry of hdr, whose body body serves, or skips what is nothing to write: the
// global header of a PAX archive and the reserved whiteouts.
func (c *unpacking) entry(hdr *tar.Header, body io.Reader) error {
	if hdr.Typeflag == tar.TypeXGlobalHeader {
		return nil
	}
	p := c.normalize(hdr, "name", hdr.Name)
	base := path.Base(p)
	switch {
	case base == opaqueMarker:
		return c.opaque(hdr, p)
	case strings.HasPrefix(base, reservedPrefix):
		return nil
	case strings.HasPrefix(base, whiteoutPrefix):
		return c.whiteout(hdr, path.Dir(p), strings.TrimPrefix(base, whiteoutPrefix))
	}
	return c.write(hdr, p, body)
}

// write writes the entry of hdr at p as the type the archive declares.
func (c *unpacking) write(hdr *tar.Header, p string, body io.Reader) error {
	switch hdr.Typeflag {
	case tar.TypeDir:
		return c.dir(hdr, p)
	case tar.TypeReg, tar.TypeGNUSparse, tar.TypeCont:
		return c.file(hdr, p, body)
	case tar.TypeSymlink:
		return c.place(hdr, p, fs.ModeSymlink, func(a Attr) error { return c.w.Symlink(p, a, hdr.Linkname) })
	case tar.TypeLink:
		return c.link(hdr, p)
	case tar.TypeChar:
		return c.device(hdr, p, fs.ModeDevice|fs.ModeCharDevice)
	case tar.TypeBlock:
		return c.device(hdr, p, fs.ModeDevice)
	case tar.TypeFifo:
		return c.place(hdr, p, fs.ModeNamedPipe, func(a Attr) error { return c.w.Mknod(p, a, 0, 0) })
	default:
		return c.refuse(hdr, fmt.Errorf("entry type %q is not one a layer holds", hdr.Typeflag))
	}
}

// refuse returns the error that names the layer, the entry of hdr and err, the cause.
func (c *unpacking) refuse(hdr *tar.Header, err error) error {
	return &EntryError{Layer: c.layer, Entry: hdr.Name, Err: err}
}

// logf writes one line about the layer to the log.
func (c *unpacking) logf(format string, args ...any) {
	if c.log != nil {
		_, _ = fmt.Fprintf(c.log, "layer %s: %s\n", c.layer, fmt.Sprintf(format, args...))
	}
}
