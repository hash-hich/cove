package erofs_test

import (
	"archive/tar"
	"bytes"
	"io"
	"io/fs"
	"testing"

	goerofs "github.com/erofs/go-erofs"
	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/erofs"
)

// refusing is an output a blob cannot be written to, and that says so the one way an error cannot
// be returned from. It stands in for the writer of the library failing on bytes a registry chose.
type refusing struct{}

func (refusing) Write([]byte) (int, error)      { panic("the writer gave up") }
func (refusing) Seek(int64, int) (int64, error) { return 0, nil }

func TestAPanicOfTheWriterRefusesTheLayerInsteadOfEndingCove(t *testing.T) {
	t.Parallel()

	archive := bytes.NewReader(tarball(t, file("a", "x")))

	_, err := erofs.Apply("sha256:0123", archive, goerofs.Create(refusing{}), nil)

	require.ErrorContains(t, err, "layer sha256:0123")
	require.ErrorContains(t, err, "panicked")
}

// buffer is an image held in memory: what the fuzzing writes blobs to, so that a run costs no
// file and the result is read back at once.
type buffer struct {
	data []byte
	at   int64
}

func (b *buffer) Write(p []byte) (int, error) {
	if grow := b.at + int64(len(p)) - int64(len(b.data)); grow > 0 {
		b.data = append(b.data, make([]byte, grow)...)
	}
	n := copy(b.data[b.at:], p)
	b.at += int64(n)
	return n, nil
}

func (b *buffer) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		b.at = offset
	case io.SeekCurrent:
		b.at += offset
	default:
		b.at = int64(len(b.data)) + offset
	}
	return b.at, nil
}

func (b *buffer) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	return copy(p, b.data[off:]), nil
}

// FuzzWriteBlob runs the conversion of a layer on archives nobody wrote on purpose: the loop of
// internal/layer, which is fuzzed there on its own, and what the EROFS writer makes of the clean
// paths it hands over, which is fuzzed here and nowhere else. A refusal is an outcome; the
// failures are a panic escaping the conversion, and a blob that was written and cannot be read
// back.
func FuzzWriteBlob(f *testing.F) {
	for _, seed := range hostileArchives(f) {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		out := &buffer{}
		w := goerofs.Create(out, goerofs.WithBlockSize(erofs.BlockSize))
		if _, err := erofs.Apply("fuzz", bytes.NewReader(data), w, io.Discard); err != nil {
			return
		}
		img, err := goerofs.Open(out)
		if err != nil {
			t.Fatalf("the blob written cannot be read back: %v", err)
		}
		if err := fs.WalkDir(img, ".", func(_ string, _ fs.DirEntry, err error) error { return err }); err != nil {
			t.Fatalf("the blob written cannot be walked: %v", err)
		}
	})
}

// hostileArchives returns the archives the fuzzing starts from: what a layer holds that the
// writer has to place somewhere, and a few streams that are not archives at all.
func hostileArchives(f *testing.F) [][]byte {
	layers := [][]entry{
		{dir("a"), owned(file("a/x", "x"), 0o4755, 1000, 1000)},
		{symlink("l", "../.."), symlink("m", "")},
		{device("d", 1<<40, -1), {hdr: tar.Header{Typeflag: tar.TypeFifo, Name: "p"}}},
		{file("f", ""), link("g", "f"), link("h", "f")},
		{file("../escape", ""), dir("o"), whiteout("o/.wh..wh..opq"), whiteout(".wh.x"), file("x", "")},
		{xattrs(file("attr", ""), map[string]string{"user.raw": "a\x00b\xff", "foo.bar": "v"})},
	}
	seeds := make([][]byte, 0, len(layers)+3)
	seeds = append(seeds, []byte("not a tar"), make([]byte, 1024), []byte{})
	for _, entries := range layers {
		seeds = append(seeds, tarball(f, entries...))
	}
	return seeds
}
