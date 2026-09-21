package image

import (
	"context"
	"fmt"
	"io"

	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"gitlab.com/hich-hich/cove/internal/layer"
)

// unpacksAtOnce bounds how many layers are read at the same time. It is a bound of its own and not
// downloadsAtOnce because the two stages hold different resources: a download holds a connection
// to the registry, which limits the rate of one client, while an unpacking holds a processor and
// the writer it feeds. It becomes a budget of bytes once the EROFS writer spools what it is told.
const unpacksAtOnce = 3

// unpack reads every entry of the layer of b, once its blob is in the store and no more than
// unpacksAtOnce layers at a time, and adds what it counted to the result. The bytes are taken
// from the store and never from the registry, which would download them a second time; the blob
// is opened by its digest, since the manifest enters the store after the layers and the image of
// the layout does not exist yet while a pull is on.
func (r *report) unpack(ctx context.Context, b blob, tokens chan struct{}) error {
	name := b.diffID.String()
	select {
	case <-b.stored:
	case <-ctx.Done():
		return fmt.Errorf("layer %s: %w", name, context.Cause(ctx))
	}
	select {
	case tokens <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("layer %s: %w", name, context.Cause(ctx))
	}
	defer func() { <-tokens }()
	// The opener sniffs the first bytes of the blob, which tells gzip, zstd and a bare tar apart
	// whatever the media type of the descriptor claims, and decompresses as a stream.
	blob, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return r.p.Store.Blob(b.desc.Digest) })
	if err != nil {
		return fmt.Errorf("layer %s: read the blob: %w", name, err)
	}
	archive, err := blob.Uncompressed()
	if err != nil {
		return fmt.Errorf("layer %s: read the blob: %w", name, err)
	}
	defer func() { _ = archive.Close() }()
	w := &tally{}
	counts, err := layer.Apply(name, stopped{ctx: ctx, r: archive}, w, r.p.Warnings())
	if err != nil {
		return err //nolint:wrapcheck // The loop names the layer and what it refused.
	}
	r.unpacked(counts, w.entries)
	return nil
}

// unpacked adds to the result what the reading of one layer counted.
func (r *report) unpacked(counts layer.Counts, entries int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.res.Entries += entries
	r.res.Unpacked.NormalizedEntries += counts.NormalizedEntries
	r.res.Unpacked.UnknownXattrPrefixes += counts.UnknownXattrPrefixes
}

// stopped serves the bytes of a layer until the pull is over: the loop reads an archive to its
// end, and nothing else would stop it once another layer has failed.
type stopped struct {
	ctx context.Context
	r   io.Reader
}

// Read gives up with the cause the pull ended on, if it ended.
func (s stopped) Read(p []byte) (int, error) {
	if s.ctx.Err() != nil {
		//nolint:wrapcheck // The cause ends the pull; the loop says which layer stopped on it.
		return 0, context.Cause(s.ctx)
	}
	return s.r.Read(p) //nolint:wrapcheck // The reader is the blob: the loop says which layer failed.
}

// tally is where the entries of a layer go until the EROFS writer lands: it counts what it is
// told and keeps nothing. An entry counted is one the layer holds, so what is set on an entry
// written just before, its attributes and what a whiteout removes, is not counted again.
type tally struct {
	entries int
}

func (t *tally) Mkdir(string, layer.Attr) error {
	t.entries++
	return nil
}

func (*tally) Setattr(string, layer.Attr) error { return nil }

// WriteFile drains the content: the loop refuses a writer that leaves part of a body unread,
// since that would be a file truncated in the blob.
func (t *tally) WriteFile(_ string, _ layer.Attr, _ int64, content io.Reader) error {
	if _, err := io.Copy(io.Discard, content); err != nil {
		return fmt.Errorf("read the content: %w", err)
	}
	t.entries++
	return nil
}

func (t *tally) Symlink(string, layer.Attr, string) error {
	t.entries++
	return nil
}

func (t *tally) Link(string, string) error {
	t.entries++
	return nil
}

func (t *tally) Mknod(string, layer.Attr, int64, int64) error {
	t.entries++
	return nil
}

func (*tally) Setxattr(string, string, string) error { return nil }

func (*tally) RemoveAll(string) error { return nil }
