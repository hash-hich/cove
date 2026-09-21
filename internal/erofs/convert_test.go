package erofs_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/erofs"
)

// opened returns a cache under root, or under a directory of its own when none is given.
func opened(t *testing.T, root ...string) *erofs.Cache {
	t.Helper()
	dir := t.TempDir()
	if len(root) == 1 {
		dir = root[0]
	}
	c, err := erofs.Open(dir)
	require.NoError(t, err)
	return c
}

// converted converts l and fails the test if it does not go through.
func converted(t *testing.T, c *erofs.Cache, l erofs.Layer, log io.Writer) erofs.Blob {
	t.Helper()
	b, err := c.Convert(t.Context(), l, log, nil)
	require.NoError(t, err)
	return b
}

func TestWritesWhatTheArchiveDeclared(t *testing.T) {
	t.Parallel()

	c := opened(t)
	l := layerOf(t,
		dir("etc"),
		owned(file("etc/shadow", "secret"), 0o4600, 1000, 2000),
		symlink("etc/localtime", "/usr/share/zoneinfo/UTC"),
		file("bin/busybox", "x"),
		link("bin/sh", "bin/busybox"),
		device("dev/null", 1, 3),
		xattrs(file("bin/ping", "x"), map[string]string{"security.capability": "\x00\x01raw"}),
	)

	b := converted(t, c, l, nil)

	img := mounted(t, b.Path)
	shadow := stat(t, img, "/etc/shadow")
	require.Equal(t, fs.FileMode(0o600)|fs.ModeSetuid, shadow.Mode, "the mode of the archive, setuid included")
	require.Equal(t, uint32(1000), shadow.UID)
	require.Equal(t, uint32(2000), shadow.GID)
	require.Equal(t, fs.ModeDevice|fs.ModeCharDevice|0o666, stat(t, img, "/dev/null").Mode)
	require.Equal(t, uint32(1<<8|3), stat(t, img, "/dev/null").Rdev, "the major and the minor of the archive")
	require.Equal(t, 2, stat(t, img, "/bin/sh").Nlink, "the two names of one inode")
	require.Equal(t, map[string]string{"security.capability": "\x00\x01raw"}, stat(t, img, "/bin/ping").Xattrs)

	target, err := fs.ReadLink(img, "etc/localtime")
	require.NoError(t, err)
	require.Equal(t, "/usr/share/zoneinfo/UTC", target)
	content, err := fs.ReadFile(img, "etc/shadow")
	require.NoError(t, err)
	require.Equal(t, "secret", string(content))
}

func TestWritesTheWhiteoutsOverlayfsReads(t *testing.T) {
	t.Parallel()

	c := opened(t)
	l := layerOf(t, dir("var"), whiteout("var/.wh.cache"), dir("opt"), whiteout("opt/.wh..wh..opq"),
		whiteout(".wh..wh.plnk"))

	b := converted(t, c, l, nil)

	img := mounted(t, b.Path)
	require.Equal(t, fs.ModeDevice|fs.ModeCharDevice, stat(t, img, "/var/cache").Mode&fs.ModeType)
	require.Equal(t, uint32(0), stat(t, img, "/var/cache").Rdev, "a whiteout is the device 0:0")
	require.Equal(t, "y", stat(t, img, "/opt").Xattrs["trusted.overlay.opaque"])
	require.Equal(t, []string{"opt", "var"}, names(t, img, "/"), "a reserved whiteout is nothing to write")
}

func TestSaysWhatItCouldNotWriteAsDeclared(t *testing.T) {
	t.Parallel()

	c := opened(t)
	l := layerOf(t, file("../escape", ""), xattrs(file("odd", ""), map[string]string{"btrfs.compression": "zstd"}))
	var log bytes.Buffer

	b := converted(t, c, l, &log)

	require.Equal(t, 1, b.Meta.NormalizedEntries)
	require.Equal(t, 1, b.Meta.UnknownXattrPrefixes)
	require.Equal(t, 2, b.Meta.Entries)
	require.Contains(t, log.String(), "climbs above the root, bounded to /escape")
	require.Contains(t, log.String(), "will not be readable in the guest")
	require.NotEmpty(t, stat(t, mounted(t, b.Path), "/escape"))
}

func TestFilesTheBlobUnderTheDiffIDAndSaysWhatItDid(t *testing.T) {
	t.Parallel()

	c := opened(t)
	l := layerOf(t, file("a", "x"))

	b := converted(t, c, l, nil)

	require.True(t, b.Converted)
	require.Equal(t, strings.TrimPrefix(l.DiffID, "sha256:"), filepath.Base(filepath.Dir(b.Path)))
	require.Equal(t, "layer.erofs", filepath.Base(b.Path))
	require.True(t, b.Meta.DiffIDVerified)
	require.Equal(t, l.Digest, b.Meta.SourceDigest)
	require.Equal(t, "v1", b.Meta.WriterVersion)
	require.Equal(t, int64(0), b.Meta.SourceDateEpoch)
	require.Equal(t, digest(read(t, b.Path)), b.Meta.BlobSHA256, "the fingerprint of the blob on the disk")
	require.FileExists(t, filepath.Join(filepath.Dir(b.Path), "meta.json"))
}

func TestServesABlobTheCacheHoldsWithoutConvertingItAgain(t *testing.T) {
	t.Parallel()

	c := opened(t)
	l := layerOf(t, file("a", "x"))
	first := converted(t, c, l, nil)

	// The bytes are not served a second time: a layer the cache holds is never read again.
	l.Open = func() (io.ReadCloser, error) { return nil, errors.New("the blob was read again") }
	again := converted(t, c, l, nil)

	require.False(t, again.Converted)
	require.Equal(t, first.Meta, again.Meta)
	require.Equal(t, first.Path, again.Path)
}

func TestRefusesALayerThatIsNotWhatTheImageSaysItIs(t *testing.T) {
	t.Parallel()

	c := opened(t)
	l := layerOf(t, file("a", "x"))
	l.DiffID = digest([]byte("something else"))

	_, err := c.Convert(t.Context(), l, nil, nil)

	require.ErrorIs(t, err, erofs.ErrDiffID)
	require.ErrorContains(t, err, l.Digest)
	require.ErrorContains(t, err, l.DiffID)
	require.Empty(t, left(t, c), "a refused layer leaves nothing behind")
}

func TestRefusesALayerHoldingAnEntryItCannotWrite(t *testing.T) {
	t.Parallel()

	c := opened(t)
	l := layerOf(t, symlink("l", "/"), file("l/traverse", ""))

	_, err := c.Convert(t.Context(), l, nil, nil)

	require.ErrorContains(t, err, "parent /l is a symbolic link, not a directory")
	require.Empty(t, left(t, c), "a refused layer leaves nothing behind")
}

func TestKeysABlobByItsDigestWhenTheImageNamesNoDiffID(t *testing.T) {
	t.Parallel()

	c := opened(t)
	l := layerOf(t, file("a", "x"))
	l.DiffID = ""

	b := converted(t, c, l, nil)

	require.False(t, b.Meta.DiffIDVerified)
	require.Equal(t, strings.TrimPrefix(l.Digest, "sha256:"), filepath.Base(filepath.Dir(b.Path)))
}

func TestGivesTheSameBlobTwice(t *testing.T) {
	t.Parallel()

	l := layerOf(t, dir("etc"), file("etc/hostname", "sandbox"), symlink("etc/mtab", "/proc/mounts"))

	first := converted(t, opened(t), l, nil)
	again := converted(t, opened(t), l, nil)

	require.Equal(t, first.Meta.BlobSHA256, again.Meta.BlobSHA256)
	require.Equal(t, read(t, first.Path), read(t, again.Path))
}

func TestDatesWhatTheArchiveDoesNotWithSourceDateEpoch(t *testing.T) {
	l := layerOf(t, file("a", "x"))
	plain := converted(t, opened(t), l, nil)

	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	c := opened(t)
	dated := converted(t, c, l, nil)

	require.NotEqual(t, plain.Meta.BlobSHA256, dated.Meta.BlobSHA256, "the date is written into the blob")
	require.Equal(t, int64(1700000000), dated.Meta.SourceDateEpoch)
	require.Contains(t, filepath.Base(filepath.Dir(dated.Path)), "-sde1700000000",
		"a blob dated otherwise is never served to a conversion without the variable")
}

func TestRefusesASourceDateEpochThatIsNotANumberOfSeconds(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "yesterday")

	_, err := erofs.Open(t.TempDir())

	require.ErrorContains(t, err, "SOURCE_DATE_EPOCH")
}

// read returns the bytes of the file at path.
func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path comes from the cache the test just wrote.
	require.NoError(t, err)
	return data
}

// left returns what the cache holds besides its own directories: what a conversion filed, and
// what a conversion that failed would have left behind.
func left(t *testing.T, c *erofs.Cache) []string {
	t.Helper()
	var got []string
	root := c.Root()
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		switch {
		case rel == "." || rel == "tmp" || rel == "merge":
			return nil
		case strings.HasSuffix(rel, ".lock"):
			return nil
		}
		got = append(got, rel)
		return nil
	}))
	return got
}
