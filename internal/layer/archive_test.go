package layer_test

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
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

// whiteout returns the entry docker writes for a whiteout: an empty regular file of mode 0.
func whiteout(name string) entry {
	return entry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: name}}
}

// xattrs returns e with the extended attributes attrs, as PAX records.
func xattrs(e entry, attrs map[string]string) entry {
	e.hdr.PAXRecords = map[string]string{}
	for name, value := range attrs {
		e.hdr.PAXRecords["SCHILY.xattr."+name] = value
	}
	return e
}

// archive returns the tar stream of entries, in that order.
func archive(t testing.TB, entries ...entry) io.Reader {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		require.NoError(t, tw.WriteHeader(&e.hdr))
		_, err := io.WriteString(tw, e.content)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return &buf
}
