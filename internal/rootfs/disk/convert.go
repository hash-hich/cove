package disk

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
)

// Convert reads the already decompressed tar archive of one layer and hands out the entries it
// holds, in the order of the archive. It returns what it counted, whatever the outcome.
//
// It stops on the end of the archive and reads nothing beyond it: the padding a layer may carry
// after the two zero blocks belongs to the caller, whose hash of the stream needs it.
//
// A path that climbs above the root of the image is bounded to it, said on Log and counted; no
// option turns that off. An entry no disk image can hold refuses the layer with
// ErrImpossibleEntry, naming the layer, the entry and the cause.
func (c *Converter) Convert(r io.Reader, out Sink) (Counts, error) {
	cv := &conversion{Converter: c, out: out, index: map[string]Kind{root: Dir}}
	archive := tar.NewReader(r)
	for {
		hdr, err := archive.Next()
		// A name that is not local is what this loop is here to bound, and the header that comes
		// with the error is the one to convert. The error is the belt of tarinsecurepath=0: it
		// costs nothing, and a name slipping through untouched would raise it instead.
		if errors.Is(err, tar.ErrInsecurePath) {
			err = nil
		}
		if errors.Is(err, io.EOF) {
			return cv.counts, nil
		}
		if err != nil {
			return cv.counts, fmt.Errorf("layer %s: read the archive: %w", cv.Layer, err)
		}
		if err := cv.entry(hdr, archive); err != nil {
			return cv.counts, err
		}
	}
}

// entry converts one header of the archive and the body the reader is positioned on.
func (cv *conversion) entry(hdr *tar.Header, body io.Reader) error {
	marked, mark := whiteoutOf(cv.entryPath(hdr.Name))
	switch mark {
	case opaque:
		return cv.opaque(hdr.Name, marked)
	case ignored:
		cv.logf("%s: %s: ignored, aufs bookkeeping that overlayfs does not read", cv.Layer, hdr.Name)
		return nil
	case deletion:
		return cv.emit(hdr, deleted(hdr, marked))
	case notAMarker:
	}
	e, err := cv.plain(hdr, marked, body)
	if err != nil {
		return err
	}
	if e.Kind == 0 {
		cv.logf("%s: %s: ignored, no entry of a disk image holds a type %q", cv.Layer, hdr.Name, hdr.Typeflag)
		return nil
	}
	return cv.emit(hdr, e)
}

// plain builds the entry a header that is not a whiteout asks for. An entry of no kind is a type
// of the archive a disk image has no place for, the tar extensions the reader already consumed
// included; the caller drops it.
func (cv *conversion) plain(hdr *tar.Header, p string, body io.Reader) (Entry, error) {
	e := Entry{
		Path:    p,
		Mode:    hdr.Mode & modeBits,
		UID:     hdr.Uid,
		GID:     hdr.Gid,
		ModTime: hdr.ModTime,
	}
	switch hdr.Typeflag {
	case tar.TypeReg, tar.TypeCont:
		e.Kind, e.Size, e.Body = Regular, hdr.Size, body
	case tar.TypeDir:
		e.Kind = Dir
	case tar.TypeSymlink:
		// Written as declared, never resolved: what it points at is the business of the guest,
		// which reads it under a root that is not the one of the host.
		e.Kind, e.Target = Symlink, hdr.Linkname
	case tar.TypeLink:
		target := cv.linkPath(hdr.Name, hdr.Linkname)
		if _, ok := cv.index[target]; !ok {
			return Entry{}, fmt.Errorf("layer %s: %s: %w: its target %s was not written by this layer",
				cv.Layer, hdr.Name, ErrImpossibleEntry, target)
		}
		e.Kind, e.Target = HardLink, target
	case tar.TypeChar:
		e.Kind, e.Major, e.Minor = CharDevice, hdr.Devmajor, hdr.Devminor
	case tar.TypeBlock:
		e.Kind, e.Major, e.Minor = BlockDevice, hdr.Devmajor, hdr.Devminor
	case tar.TypeFifo:
		e.Kind = Fifo
	}
	return e, nil
}

// emit records the entry, writes it and sets what the archive hangs on it.
func (cv *conversion) emit(hdr *tar.Header, e Entry) error {
	if err := cv.place(hdr.Name, e.Path, e.Kind); err != nil {
		return err
	}
	if err := cv.out.Add(e); err != nil {
		return fmt.Errorf("layer %s: %s: write the entry: %w", cv.Layer, hdr.Name, err)
	}
	return cv.xattrs(hdr, e.Path)
}
