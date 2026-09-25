package erofs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"time"

	goerofs "github.com/erofs/go-erofs"

	"gitlab.com/hich-hich/cove/internal/layer"
)

// ErrDiffID reports a layer whose decompressed stream does not hash to the diff id the config of
// the image names for it.
var ErrDiffID = errors.New("the layer does not match its diff id")

// Layer is one layer to convert: where its bytes come from, and the two names it answers to.
type Layer struct {
	// DiffID is the hash of the decompressed layer, which the config of the image carries. It
	// names the blob in the cache and is checked against the stream on the way. It is empty for
	// an image whose config names none, and the conversion then keys the blob by Digest and
	// records that nothing was checked.
	DiffID string
	// Digest names the compressed blob the archive came from, which meta.json records and which
	// keys the blob when the image names no diff id.
	Digest string
	// Open serves the archive of the layer, decompressed: the caller owns what a layer is
	// compressed with, since that is where the blob of the registry is held.
	Open func() (io.ReadCloser, error)
}

// Name is how messages speak of the layer: its diff id, which is what the cache and the report
// call it, falling back to the digest of the compressed blob when the image names no diff id.
func (l Layer) Name() string {
	if l.DiffID != "" {
		return l.DiffID
	}
	return l.Digest
}

// Convert writes the blob of l and files it in the cache, unless the cache holds it already, and
// returns it either way. Every name bounded to the root and every extended attribute EROFS will
// not read in the guest is said on log, one line each, and counted in the meta of the blob; a nil
// log keeps quiet. onWait, when not nil, is called once if another process is converting the same
// layer and this one has to wait for it.
//
// The archive is read as a stream, hashed on the way to the reader of the tar, and what it
// declares goes straight into the EROFS image. No unpacked tree exists at any point, on the host
// or anywhere else.
//
// The conversion fails, and nothing is filed, when the stream does not hash to DiffID, when the
// archive holds an entry that cannot be written as declared, or when ctx ends first.
func (c *Cache) Convert(ctx context.Context, l Layer, log io.Writer, onWait func()) (Blob, error) {
	key, err := keyOf(l)
	if err != nil {
		return Blob{}, err
	}
	b, err := c.build(ctx, c.layerEntry(key), onWait, func(dir string) (Meta, error) {
		return c.writeBlob(ctx, l, dir, log)
	})
	if err != nil {
		return Blob{}, err
	}
	b.DiffID = l.DiffID
	return b, nil
}

// Lookup returns the blob of the layer diffID when the cache holds it, and false when it does not:
// the layer was never converted, or the cache was emptied since.
func (c *Cache) Lookup(diffID string) (Blob, bool, error) {
	key, err := keyOf(Layer{DiffID: diffID})
	if err != nil {
		return Blob{}, false, err
	}
	b, ok, err := lookup(c.layerEntry(key))
	if !ok || err != nil {
		return Blob{}, false, err
	}
	b.DiffID = diffID
	return b, true, nil
}

// keyOf returns the key of the blob of l: the diff id of the layer, known before a byte is read, so
// that the same layer compressed twice over is converted once. Without a diff id it falls back to
// the digest of the compressed blob, which names those bytes and nothing more.
func keyOf(l Layer) (string, error) {
	if l.DiffID != "" {
		return hexOf(l.DiffID)
	}
	return hexOf(l.Digest)
}

// writeBlob writes the blob of l in dir and returns what it counted. The caller holds the lock of
// the layer and takes dir away if this fails.
func (c *Cache) writeBlob(ctx context.Context, l Layer, dir string, log io.Writer) (Meta, error) {
	archive, err := l.Open()
	if err != nil {
		return Meta{}, fmt.Errorf("layer %s: read the blob: %w", l.Name(), err)
	}
	defer func() { _ = archive.Close() }()
	//nolint:gosec // G304: dir is built by the cache from its own root, never from user input.
	f, err := os.OpenFile(filepath.Join(dir, blobFile), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return Meta{}, fmt.Errorf("layer %s: create the blob: %w", l.Name(), err)
	}
	defer func() { _ = f.Close() }()

	// The hash covers the archive whole, the padding at its end included, so it is taken on the
	// way to the reader of the tar and never after it.
	sum := sha256.New()
	counts, err := apply(l.Name(), io.TeeReader(halted{ctx: ctx, r: archive}, sum), c.newWriter(f), log)
	if err != nil {
		return Meta{}, err
	}
	if err := verify(l, sum); err != nil {
		return Meta{}, err
	}
	fingerprint, err := seal(f)
	if err != nil {
		return Meta{}, fmt.Errorf("layer %s: %w", l.Name(), err)
	}
	return Meta{
		BlobSHA256:           fingerprint,
		ConvertedAt:          time.Now().UTC(),
		SourceDigest:         l.Digest,
		Entries:              counts.entries,
		NormalizedEntries:    counts.NormalizedEntries,
		UnknownXattrPrefixes: counts.UnknownXattrPrefixes,
		DiffIDVerified:       l.DiffID != "",
	}, nil
}

// newWriter returns the EROFS writer that fills out with the parameters of the cache. The spool
// of the writer, the one file it makes of its own, goes to tmp/, where the sweep of a later run
// finds it if this process does not get to unlink it.
func (c *Cache) newWriter(out io.WriteSeeker) *goerofs.Writer {
	return goerofs.Create(out,
		goerofs.WithBlockSize(blockSize),
		//nolint:gosec // G115: the epoch is refused unless it is a positive number of seconds.
		goerofs.WithBuildTime(uint64(c.epoch), 0),
		goerofs.WithTempDir(c.tmp),
	)
}

// counted is what one conversion counted: what the archive made the loop do, and what the writer
// created out of it.
type counted struct {
	layer.Counts
	entries int
}

// apply reads the archive and writes its entries through the EROFS writer, then closes it, which
// is what lays the image out. A panic of the writer becomes a layer refused: an archive of a
// registry is not trusted enough to take cove down with it.
//
//nolint:nonamedreturns // The recover has nothing else to put the refusal in.
func apply(name string, archive io.Reader, w *goerofs.Writer, log io.Writer) (c counted, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("layer %s: the writer of the blob panicked: %v", name, p)
		}
	}()
	b := &writer{w: w}
	c.Counts, err = layer.Apply(name, archive, b, log)
	if err != nil {
		return counted{}, err //nolint:wrapcheck // The loop names the layer and what it refused.
	}
	// The reader of the tar stops at the two empty blocks that end it; what follows is read here,
	// so that the hash covers the archive whole and whatever decompresses it sees its own end.
	if _, err := io.Copy(io.Discard, archive); err != nil {
		return counted{}, fmt.Errorf("layer %s: read the archive to its end: %w", name, err)
	}
	if err := w.Close(); err != nil {
		return counted{}, fmt.Errorf("layer %s: write the blob: %w", name, err)
	}
	c.entries = b.entries
	return c, nil
}

// verify compares the hash of the decompressed stream to the diff id the image names for l.
// Nothing mountable comes out of a layer that is not what the image says it is.
func verify(l Layer, sum hash.Hash) error {
	if l.DiffID == "" {
		return nil
	}
	got := sha256Algo + ":" + hex.EncodeToString(sum.Sum(nil))
	if got != l.DiffID {
		return fmt.Errorf("%w: the blob %s decompresses to %s, where the image names %s",
			ErrDiffID, l.Digest, got, l.DiffID)
	}
	return nil
}

// seal gets the blob to the disk and returns its fingerprint, read back from the file that was
// just written: what the guest will mount, computed once and kept in meta.json.
func seal(f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("read the blob back: %w", err)
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", fmt.Errorf("read the blob back: %w", err)
	}
	// The bytes reach the disk before the directory is moved into the cache: a power cut after
	// the rename must not leave a blob that a later run trusts on the strength of its name.
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync the blob: %w", err)
	}
	return sha256Algo + ":" + hex.EncodeToString(sum.Sum(nil)), nil
}

// halted serves the archive of a layer until the work it belongs to is over: the loop reads an
// archive to its end, and nothing else would stop it once another layer has failed.
type halted struct {
	ctx context.Context
	r   io.Reader
}

// Read gives up with the cause the work ended on, if it ended.
func (h halted) Read(p []byte) (int, error) {
	if h.ctx.Err() != nil {
		//nolint:wrapcheck // The cause ends the work; the caller says which layer stopped on it.
		return 0, context.Cause(h.ctx)
	}
	return h.r.Read(p) //nolint:wrapcheck // The reader is the blob: the caller says which layer failed.
}
