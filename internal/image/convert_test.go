//go:debug tarinsecurepath=0
package image_test

import (
	"archive/tar"
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/compression"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/image"
)

// junk is what a layer holds when the test wants an archive that cannot be read: more than the
// block a tar reader asks for, and nothing a header could be found in.
var junk = bytes.Repeat([]byte("this is not a tar archive; "), 100)

// file returns the header of an empty regular file named name.
func file(name string) tar.Header {
	return tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644}
}

// climbing returns the header of a file whose name climbs above the root of the layer: what the
// unpacking bounds, counts and says.
func climbing(name string) tar.Header {
	return file("../" + name)
}

// attributed returns hdr with the extended attribute attr, as a PAX record.
func attributed(hdr tar.Header, attr, value string) tar.Header {
	hdr.PAXRecords = map[string]string{"SCHILY.xattr." + attr: value}
	return hdr
}

// tarOf returns the bytes of the archive of hdrs, whole.
func tarOf(t *testing.T, hdrs ...tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, hdr := range hdrs {
		require.NoError(t, tw.WriteHeader(&hdr))
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

// brokenAfter returns the bytes of an archive holding hdrs and then junk where the next header
// belongs: the layer is read up to that point, then refused.
func brokenAfter(t *testing.T, hdrs ...tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, hdr := range hdrs {
		require.NoError(t, tw.WriteHeader(&hdr))
	}
	require.NoError(t, tw.Flush())
	_, _ = buf.Write(junk)
	return buf.Bytes()
}

// gzipLayer returns a layer whose blob is raw gzipped, as registries hold most layers.
func gzipLayer(t *testing.T, raw []byte) v1.Layer {
	t.Helper()
	return layerOf(t, raw)
}

// zstdLayer returns a layer whose blob is raw compressed with zstd, which the OCI specification
// allows and some registries hold.
func zstdLayer(t *testing.T, raw []byte) v1.Layer {
	t.Helper()
	return layerOf(t, raw, tarball.WithCompression(compression.ZStd),
		tarball.WithMediaType(types.OCILayerZStd))
}

// layerOf returns the layer of the archive raw, compressed as opts say.
func layerOf(t *testing.T, raw []byte, opts ...tarball.LayerOption) v1.Layer {
	t.Helper()
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}, opts...)
	require.NoError(t, err)
	return l
}

// imageOf returns an image of the platform of the host holding ls, in that order.
func imageOf(t *testing.T, ls ...v1.Layer) v1.Image {
	t.Helper()
	img, err := mutate.AppendLayers(empty.Image, ls...)
	require.NoError(t, err)
	cf, err := img.ConfigFile()
	require.NoError(t, err)
	cf = cf.DeepCopy()
	cf.OS, cf.Architecture = image.HostPlatform().OS, image.HostPlatform().Architecture
	img, err = mutate.ConfigFile(img, cf)
	require.NoError(t, err)
	return img
}

// diffIDs returns the diff ids of the layers of img, in the order of the manifest: how a pull
// names a layer.
func diffIDs(t *testing.T, img v1.Image) []v1.Hash {
	t.Helper()
	cf, err := img.ConfigFile()
	require.NoError(t, err)
	return cf.RootFS.DiffIDs
}

// syncBuffer is a log the test reads while the pull writes it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p) //nolint:wrapcheck // A buffer never fails.
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// heldTooLong bounds how long a blob is held for something that is meant to happen while it
// waits. It is not what a test measures, only what turns a pipeline that would wait for ever into
// a failure a reader can read.
const heldTooLong = 30 * time.Second

// holdBlob holds every download of the blob h until ready reports true, then lets the registry
// serve it. It returns whether the blob had to be served with ready still false, which is a
// pipeline that never did what the test waited for.
func (reg *testRegistry) holdBlob(h v1.Hash, ready func() bool) func() bool {
	var forced atomic.Bool
	hk := hook(func(_ http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/blobs/"+h.String()) {
			return false
		}
		giveUp := time.After(heldTooLong)
		for !ready() {
			select {
			case <-r.Context().Done():
				return false
			case <-giveUp:
				forced.Store(true)
				return false
			case <-time.After(time.Millisecond):
			}
		}
		return false
	})
	reg.hook.Store(&hk)
	return forced.Load
}

// saidOfLayer returns how many lines of facts the conversion of the layer named id wrote.
func saidOfLayer(facts string, id v1.Hash) int {
	return strings.Count(facts, "layer "+id.String()+": entry ")
}

func TestPullAccountsForEveryLayerOfTheManifest(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	root := t.TempDir()
	// Two of the five layers were converted by an earlier pull. They are not read again, so they
	// say nothing this time, and what they hold is counted all the same, out of the cache: each
	// layer holds one name that climbs above the root.
	held := make([]v1.Layer, 0, 5)
	held = append(held, gzipLayer(t, tarOf(t, climbing("a"))), gzipLayer(t, tarOf(t, climbing("b"))))
	base := reg.ref(t, "org/base:tag")
	publish(t, base, imageOf(t, held...))
	_, _, err := pull(t, root, base)
	require.NoError(t, err)
	img := imageOf(t, append(held, gzipLayer(t, tarOf(t, climbing("c"))),
		gzipLayer(t, tarOf(t, climbing("d"))), gzipLayer(t, tarOf(t, climbing("e"))))...)
	ref := reg.ref(t, "org/repo:tag")
	publish(t, ref, img)

	res, facts, err := pull(t, root, ref)

	require.NoError(t, err)
	require.Equal(t, 5, res.LayersTotal)
	require.Equal(t, 3, res.LayersFetched)
	require.Equal(t, 3, res.BlobsConverted)
	require.Equal(t, 5, res.Entries)
	require.Equal(t, 5, res.NormalizedEntries)
	ids := diffIDs(t, img)
	for _, id := range ids[:2] {
		require.Equal(t, 0, saidOfLayer(facts, id), facts)
	}
	for _, id := range ids[2:] {
		require.Equal(t, 1, saidOfLayer(facts, id), facts)
	}
	require.Empty(t, dataFiles(t, filepath.Join(root, "tmp")))
}

func TestPullConvertsALayerWhileTheOthersComeDown(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	// The first layer says one line as soon as it is read: a name that climbs above the root.
	img := imageOf(t, gzipLayer(t, tarOf(t, climbing("first"))), gzipLayer(t, tarOf(t, file("second"))),
		gzipLayer(t, tarOf(t, file("third"))), gzipLayer(t, tarOf(t, file("last"))))
	publish(t, ref, img)
	facts := &syncBuffer{}
	first := diffIDs(t, img)[0]
	// The registry holds the last layer until the first has been read. A pull that waited for
	// the last download before reading anything would never end.
	forced := reg.holdBlob(layers(t, img)[3], func() bool { return saidOfLayer(facts.String(), first) == 1 })

	res, err := puller(t, t.TempDir(), facts, nil).Pull(t.Context(), ref)

	require.NoError(t, err)
	require.False(t, forced(), "the first layer was read only after the last download")
	require.Equal(t, 4, res.LayersFetched)
	require.Equal(t, 4, res.Entries)
}

func TestPullSaysWhatALayerHoldsAndEROFSWillNotKeep(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	img := imageOf(t,
		gzipLayer(t, tarOf(t, climbing("escaped"))),
		gzipLayer(t, tarOf(t, attributed(file("attributed"), "btrfs.compression", "zstd"))))
	publish(t, ref, img)
	ids := diffIDs(t, img)

	res, facts, err := pull(t, t.TempDir(), ref)

	require.NoError(t, err)
	require.Equal(t, 1, res.NormalizedEntries)
	require.Equal(t, 1, res.UnknownXattrPrefixes)
	// Each line names its layer by its diff id, the key of the rootfs it unpacks to.
	require.Contains(t, facts, "layer "+ids[0].String()+": entry \"../escaped\": name \"../escaped\" "+
		"climbs above the root, bounded to /escaped")
	require.Contains(t, facts, "layer "+ids[1].String()+": entry \"attributed\": attribute "+
		"\"btrfs.compression\" is written but will not be readable in the guest")
	// What was counted is the result, not a line: the end of a pull says what came down.
	require.Equal(t, 2, res.Entries)
	require.NotContains(t, facts, "Unpacked:")
}

func TestPullConvertsALayerWhateverItIsCompressedWith(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	img := imageOf(t, gzipLayer(t, tarOf(t, climbing("gzipped"))),
		zstdLayer(t, tarOf(t, climbing("zstandard"))))
	publish(t, ref, img)
	ids := diffIDs(t, img)

	res, facts, err := pull(t, t.TempDir(), ref)

	require.NoError(t, err)
	require.Equal(t, 2, res.Entries)
	require.Equal(t, 2, res.NormalizedEntries)
	for _, id := range ids {
		require.Equal(t, 1, saidOfLayer(facts, id), facts)
	}
}

func TestPullFailsOnALayerItCannotRead(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	root := t.TempDir()
	held := gzipLayer(t, tarOf(t, file("held")))
	base := reg.ref(t, "org/base:tag")
	publish(t, base, imageOf(t, held))
	_, _, err := pull(t, root, base)
	require.NoError(t, err)
	img := imageOf(t, held, gzipLayer(t, junk))
	ref := reg.ref(t, "org/repo:tag")
	publish(t, ref, img)

	_, _, err = pull(t, root, ref)

	require.Error(t, err)
	require.ErrorContains(t, err, "layer "+diffIDs(t, img)[1].String())
	require.ErrorContains(t, err, "read the archive")
	// The blob that was in the store stays there, and nothing is left outside the layout.
	require.Contains(t, blobs(t, root), layerBlob(t, held).String())
	require.Empty(t, dataFiles(t, filepath.Join(root, "tmp")))
}

func TestPullReportsTheFirstUnreadableLayerOfTheManifest(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	// The higher layer of the manifest is read, says its line, then breaks; the lower one cannot
	// be read at all, and the registry holds it until the higher one has begun, so that the two
	// orders the failures may take are both played.
	higher := gzipLayer(t, brokenAfter(t, climbing("read")))
	img := imageOf(t, higher, gzipLayer(t, junk))
	publish(t, ref, img)
	ids := diffIDs(t, img)

	// Played twice: what a broken image says does not depend on which failure arrived first.
	for range 2 {
		facts := &syncBuffer{}
		_ = reg.holdBlob(layers(t, img)[1], func() bool { return saidOfLayer(facts.String(), ids[0]) == 1 })

		_, err := puller(t, t.TempDir(), facts, nil).Pull(t.Context(), ref)

		require.Error(t, err)
		require.ErrorContains(t, err, "layer "+ids[0].String())
		require.NotContains(t, err.Error(), ids[1].String())
	}
}

// layerBlob returns the digest of the blob of l.
func layerBlob(t *testing.T, l v1.Layer) v1.Hash {
	t.Helper()
	h, err := l.Digest()
	require.NoError(t, err)
	return h
}
