package erofs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// The first bytes of the compressions an OCI layer is published under. What the descriptor of a
// layer claims is not read: the bytes decide, since a registry that mislabels a layer would
// otherwise make cove fail on an image every other client reads.
var (
	gzipMagic = []byte{0x1f, 0x8b}
	zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}
)

// decompress returns the archive of the layer whose compressed bytes r serves, decompressed as a
// stream, which the caller closes. A blob that is a bare tar is served as it is.
//
// The bytes go through here once and once only: the compressed blob was verified against the
// digest that names it on its way into the store, and the decompressed stream is hashed against
// the diff id on its way to the reader of the archive, so nothing is gained by a pass that reads
// the blob only to hash it.
func decompress(r io.Reader) (io.ReadCloser, error) {
	buffered := bufio.NewReader(r)
	// A blob shorter than the longest magic is not one of the compressions: it is served as it
	// stands and the reader of the archive says what it makes of it.
	magic, err := buffered.Peek(len(zstdMagic))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read the first bytes: %w", err)
	}
	switch {
	case bytes.HasPrefix(magic, gzipMagic):
		zr, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, fmt.Errorf("read the gzip stream: %w", err)
		}
		return zr, nil
	case bytes.HasPrefix(magic, zstdMagic):
		zr, err := zstd.NewReader(buffered)
		if err != nil {
			return nil, fmt.Errorf("read the zstd stream: %w", err)
		}
		return zr.IOReadCloser(), nil
	default:
		return io.NopCloser(buffered), nil
	}
}
