package image

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"gitlab.com/hich-hich/cove/internal/erofs"
)

// convertsAtOnce bounds how many layers are converted at the same time. It is a bound of its own
// and not downloadsAtOnce because the two stages hold different resources: a download holds a
// connection to the registry, which limits the rate of one client, while a conversion holds a
// processor and the spool the EROFS writer keeps its file bodies in, which is a budget of the
// disk and not of the network.
const convertsAtOnce = 3

// convert turns the layer of b into the blob a VM mounts, once its blob is in the store and no
// more than convertsAtOnce layers at a time, and adds what it did to the result. The bytes are
// taken from the store and never from the registry, which would download them a second time; the
// blob is opened by its digest, since the manifest enters the store after the layers and the
// image of the layout does not exist yet while a pull is on. What a layer is compressed with is
// answered here, where the blob of the registry is held, and the conversion is given the archive.
func (r *report) convert(ctx context.Context, b blob, tokens chan struct{}) error {
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
	start := time.Now()
	// The opener sniffs the first bytes of the blob, which tells gzip, zstd and a bare tar apart
	// whatever the media type of the descriptor claims, and decompresses as a stream.
	stored, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return r.p.Store.Blob(b.desc.Digest) })
	if err != nil {
		return fmt.Errorf("layer %s: read the blob: %w", name, err)
	}
	converted, err := r.p.Rootfs.Convert(ctx, erofs.Layer{
		DiffID: name,
		Digest: b.desc.Digest.String(),
		Open:   stored.Uncompressed,
	}, r.p.Warnings(), func() {
		r.say("%s: waiting for another pull that converts it", short(b.diffID))
	})
	if err != nil {
		return err //nolint:wrapcheck // The conversion names the layer and what it refused.
	}
	r.converted(converted, start)
	return nil
}

// converted adds to the result what one layer of the manifest gave. The span the conversions took
// is stretched rather than added up: they run at the same time and with the downloads, so the
// figure the report gives is the wall clock from the first of them to the last.
func (r *report) converted(b erofs.Blob, start time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b.Converted {
		r.res.BlobsConverted++
	}
	r.res.Entries += b.Meta.Entries
	r.res.NormalizedEntries += b.Meta.NormalizedEntries
	r.res.UnknownXattrPrefixes += b.Meta.UnknownXattrPrefixes
	if r.converting.IsZero() || start.Before(r.converting) {
		r.converting = start
	}
	r.res.Converted = time.Since(r.converting)
}
