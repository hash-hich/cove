//go:debug tarinsecurepath=0
package erofs_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/erofs"
)

// entry is one entry of a test archive: its header and, for a regular file, its content.
type entry struct {
	hdr     tar.Header
	content string
}

// file returns a regular file of mode 0644 holding content.
func file(name, content string) entry {
	return entry{
		hdr:     tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: int64(len(content))},
		content: content,
	}
}

// dir returns a directory of mode 0755.
func dir(name string) entry {
	return entry{hdr: tar.Header{Typeflag: tar.TypeDir, Name: name, Mode: 0o755}}
}

// symlink returns a symbolic link to target.
func symlink(name, target string) entry {
	return entry{hdr: tar.Header{Typeflag: tar.TypeSymlink, Name: name, Linkname: target, Mode: 0o777}}
}

// link returns a hard link to target.
func link(name, target string) entry {
	return entry{hdr: tar.Header{Typeflag: tar.TypeLink, Name: name, Linkname: target}}
}

// device returns a character device with the numbers major and minor.
func device(name string, major, minor int64) entry {
	return entry{hdr: tar.Header{
		Typeflag: tar.TypeChar, Name: name, Mode: 0o666, Devmajor: major, Devminor: minor,
	}}
}

// whiteout returns the entry docker writes for a whiteout: an empty regular file of mode 0.
func whiteout(name string) entry {
	return entry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: name}}
}

// owned returns e owned by uid and gid, with the mode mode.
func owned(e entry, mode int64, uid, gid int) entry {
	e.hdr.Mode = mode
	e.hdr.Uid, e.hdr.Gid = uid, gid
	return e
}

// xattrs returns e with the extended attributes attrs, as PAX records.
func xattrs(e entry, attrs map[string]string) entry {
	e.hdr.PAXRecords = map[string]string{}
	for name, value := range attrs {
		e.hdr.PAXRecords["SCHILY.xattr."+name] = value
	}
	return e
}

// tarball returns the tar stream of entries, in that order.
func tarball(t testing.TB, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		require.NoError(t, tw.WriteHeader(&e.hdr))
		_, err := io.WriteString(tw, e.content)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

// compressed returns raw as gzip, the form a layer is published in.
func compressed(t testing.TB, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// digest returns the sha256 of raw, written as the OCI image specification writes it.
func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// layerOf returns the layer the entries make, gzipped, with the diff id and the digest an image
// would name it by.
func layerOf(t testing.TB, entries ...entry) erofs.Layer {
	t.Helper()
	raw := tarball(t, entries...)
	blob := compressed(t, raw)
	return erofs.Layer{
		DiffID: digest(raw),
		Digest: digest(blob),
		Open:   func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(blob)), nil },
	}
}
