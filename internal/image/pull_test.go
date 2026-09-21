package image_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-containerregistry/pkg/v1/validate"
	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/image"
)

// The credentials the private registry of the tests accepts.
const (
	user     = "cove"
	password = "secret"
)

// hook is what a test puts in front of the registry: it reports true when it answered the request
// itself.
type hook func(w http.ResponseWriter, r *http.Request) bool

// testRegistry serves images from memory on the loopback, where the tests reach a registry without
// going out on the internet. A hook set after the images are published shapes the answers.
type testRegistry struct {
	host string
	hook atomic.Pointer[hook]
}

// serve starts a registry and returns it.
func serve(t *testing.T) *testRegistry {
	reg := &testRegistry{}
	inner := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := reg.hook.Load(); h != nil && (*h)(w, r) {
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	reg.host = strings.TrimPrefix(srv.URL, "http://")
	return reg
}

// ref returns the reference of repo in the registry, with its tag or digest.
func (reg *testRegistry) ref(t *testing.T, repo string) name.Reference {
	parsed, err := image.Parse(reg.host + "/" + repo)
	require.NoError(t, err)
	return parsed
}

// publish pushes img as ref and returns the digest of its manifest.
func publish(t *testing.T, ref name.Reference, img v1.Image) v1.Hash {
	require.NoError(t, remote.Write(ref, img))
	digest, err := img.Digest()
	require.NoError(t, err)
	return digest
}

// publishIndex pushes an index of imgs as ref, each under its platform and an attestation of
// buildkit next to them, and returns its digest.
func publishIndex(t *testing.T, ref name.Reference, imgs ...v1.Image) v1.Hash {
	attestation, err := random.Image(64, 1)
	require.NoError(t, err)
	idx := mutate.AppendManifests(empty.Index, mutate.IndexAddendum{
		Add:        attestation,
		Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"}},
	})
	for _, img := range imgs {
		cf, err := img.ConfigFile()
		require.NoError(t, err)
		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
			Add:        img,
			Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: cf.OS, Architecture: cf.Architecture}},
		})
	}
	require.NoError(t, remote.WriteIndex(ref, idx))
	digest, err := idx.Digest()
	require.NoError(t, err)
	return digest
}

// publishIndexWithoutPlatforms pushes an index of imgs as ref where no entry carries a platform,
// which the spec allows since the field is optional, and returns its digest.
func publishIndexWithoutPlatforms(t *testing.T, ref name.Reference, imgs ...v1.Image) v1.Hash {
	idx := v1.ImageIndex(empty.Index)
	for _, img := range imgs {
		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{Add: img})
	}
	require.NoError(t, remote.WriteIndex(ref, idx))
	digest, err := idx.Digest()
	require.NoError(t, err)
	return digest
}

// guard answers 401 to any request that does not carry the credentials, with a Basic challenge.
func (reg *testRegistry) guard() {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	h := hook(func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") == want {
			return false
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="cove"`)
		w.WriteHeader(http.StatusUnauthorized)
		return true
	})
	reg.hook.Store(&h)
}

// onBlob puts f in front of the downloads of the blob named h.
func (reg *testRegistry) onBlob(h v1.Hash, f func(w http.ResponseWriter, r *http.Request)) {
	hk := hook(func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/blobs/"+h.String()) {
			return false
		}
		f(w, r)
		return true
	})
	reg.hook.Store(&hk)
}

// counting counts the downloads of each blob.
func (reg *testRegistry) counting() *sync.Map {
	var counts sync.Map
	hk := hook(func(_ http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet {
			if _, digest, found := strings.Cut(r.URL.Path, "/blobs/"); found {
				n, _ := counts.LoadOrStore(digest, new(atomic.Int64))
				n.(*atomic.Int64).Add(1) //nolint:forcetypeassert // the map only holds counters.
			}
		}
		return false
	})
	reg.hook.Store(&hk)
	return &counts
}

// other is a platform that is not the one of the host.
func other() v1.Platform {
	p := v1.Platform{OS: "linux", Architecture: "amd64"}
	if image.HostPlatform().Architecture == "amd64" {
		p.Architecture = "arm64"
	}
	return p
}

// build returns a random image of layers layers, built for platform.
func build(t *testing.T, platform v1.Platform, layers int64) v1.Image {
	img, err := random.Image(1024, layers)
	require.NoError(t, err)
	cf, err := img.ConfigFile()
	require.NoError(t, err)
	cf = cf.DeepCopy()
	cf.OS, cf.Architecture = platform.OS, platform.Architecture
	img, err = mutate.ConfigFile(img, cf)
	require.NoError(t, err)
	return img
}

// layers returns the digests of the layers of img.
func layers(t *testing.T, img v1.Image) []v1.Hash {
	manifest, err := img.Manifest()
	require.NoError(t, err)
	digests := make([]v1.Hash, 0, len(manifest.Layers))
	for _, l := range manifest.Layers {
		digests = append(digests, l.Digest)
	}
	return digests
}

// puller returns a puller on a store at root, anonymous unless keychain says otherwise, writing
// its facts to facts.
func puller(t *testing.T, root string, facts io.Writer, keychain authn.Keychain) *image.Puller {
	store, err := image.Open(root)
	require.NoError(t, err)
	if keychain == nil {
		keychain = authn.NewMultiKeychain()
	}
	return &image.Puller{Store: store, Log: facts, Keychain: keychain}
}

// pull pulls ref into a store at root and returns the result and the facts written.
func pull(t *testing.T, root string, ref name.Reference) (image.Result, string, error) {
	var facts bytes.Buffer
	res, err := puller(t, root, &facts, nil).Pull(t.Context(), ref)
	return res, facts.String(), err
}

// blobs returns the digests of the blobs the store at root holds.
func blobs(t *testing.T, root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "images", "blobs", "sha256"))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, "sha256:"+e.Name())
	}
	return names
}

// requireComplete checks that the store at root holds img whole, as a reader of the layout does.
func requireComplete(t *testing.T, root string, img v1.Image) {
	digest, err := img.Digest()
	require.NoError(t, err)
	stored, err := layout.Path(filepath.Join(root, "images")).Image(digest)
	require.NoError(t, err)
	require.NoError(t, validate.Image(stored))
}

func TestPullPicksThePlatformOfTheHost(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	host, foreign := build(t, image.HostPlatform(), 2), build(t, other(), 2)
	index := publishIndex(t, ref, foreign, host)
	want, err := host.Digest()
	require.NoError(t, err)
	root := t.TempDir()

	res, facts, err := pull(t, root, ref)

	require.NoError(t, err)
	require.Equal(t, want, res.Digest)
	require.NotEqual(t, index, res.Digest)
	require.Equal(t, ref.Context().Digest(want.String()).Name(), res.Pinned())
	require.Equal(t, image.HostPlatform(), res.Platform)
	require.Equal(t, ref.Name(), res.Ref)
	require.Equal(t, 2, res.LayersTotal)
	require.Equal(t, 2, res.LayersFetched)
	require.Positive(t, res.Bytes)
	require.False(t, res.Cached)
	// What is being pulled and what it costs open the pull on one line.
	require.True(t, strings.HasPrefix(facts, "pulling "+ref.Name()+" for "+image.HostPlatform().String()+
		": 2 layers, 2 missing ("), facts)
	// The pull ends on what it brought, a label to a line, not on the last layer.
	require.Regexp(t, "Digest: "+want.String()+
		`\nStatus: downloaded 2 of 2 layers, [0-9.]+ kB in [0-9a-z.]+\n\z`, facts)
	requireComplete(t, root, host)
	require.Len(t, blobs(t, root), 4)
	idx, err := layout.ImageIndexFromPath(filepath.Join(root, "images"))
	require.NoError(t, err)
	require.NoError(t, validate.Index(idx))
	manifest, err := idx.IndexManifest()
	require.NoError(t, err)
	require.Len(t, manifest.Manifests, 1)
	require.Equal(t, ref.Name(), manifest.Manifests[0].Annotations[image.RefName])
	require.Equal(t, image.HostPlatform(), *manifest.Manifests[0].Platform)
}

func TestPullRefusesAnotherPlatform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		publish func(t *testing.T, reg *testRegistry, ref name.Reference) name.Reference
	}{
		{
			name: "index without the platform of the host",
			publish: func(t *testing.T, reg *testRegistry, ref name.Reference) name.Reference {
				publishIndex(t, ref, build(t, other(), 1))
				return ref
			},
		},
		{
			name: "manifest alone",
			publish: func(t *testing.T, reg *testRegistry, ref name.Reference) name.Reference {
				publish(t, ref, build(t, other(), 1))
				return ref
			},
		},
		{
			name: "by digest",
			publish: func(t *testing.T, reg *testRegistry, ref name.Reference) name.Reference {
				digest := publish(t, ref, build(t, other(), 1))
				return ref.Context().Digest(digest.String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reg := serve(t)
			ref := tt.publish(t, reg, reg.ref(t, "org/repo:tag"))
			root := t.TempDir()

			_, _, err := pull(t, root, ref)

			require.ErrorIs(t, err, image.ErrPlatform)
			require.ErrorContains(t, err, image.HostPlatform().String())
			require.ErrorContains(t, err, other().String())
			// The registry answered: it is not what the message is about, nor is an attestation.
			require.NotContains(t, err.Error(), reg.host+": ")
			require.NotContains(t, err.Error(), "unknown")
			require.Empty(t, blobs(t, root))
		})
	}
}

func TestPullAcceptsAManifestAlone(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	img := build(t, image.HostPlatform(), 1)
	want := publish(t, ref, img)
	root := t.TempDir()

	res, _, err := pull(t, root, ref)

	require.NoError(t, err)
	require.Equal(t, want, res.Digest)
	requireComplete(t, root, img)
}

func TestPullRefusesAnImageThatStatesNoPlatform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		publish func(t *testing.T, ref name.Reference)
	}{
		{
			name: "index whose entries leave the platform out",
			publish: func(t *testing.T, ref name.Reference) {
				publishIndexWithoutPlatforms(t, ref, build(t, other(), 1))
			},
		},
		{
			name:    "index holding attestations alone",
			publish: func(t *testing.T, ref name.Reference) { publishIndex(t, ref) },
		},
		{
			name: "manifest whose config leaves the OS out",
			publish: func(t *testing.T, ref name.Reference) {
				publish(t, ref, build(t, v1.Platform{Architecture: image.HostPlatform().Architecture}, 1))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reg := serve(t)
			ref := reg.ref(t, "org/repo:tag")
			tt.publish(t, ref)
			root := t.TempDir()

			_, _, err := pull(t, root, ref)

			require.ErrorIs(t, err, image.ErrPlatform)
			require.ErrorContains(t, err, image.HostPlatform().String())
			// What the image leaves out is said in words: the message never leaves a hole where
			// a platform belongs.
			require.NotContains(t, err.Error(), "for , ")
			require.NotContains(t, err.Error(), "unknown")
			require.Empty(t, blobs(t, root))
		})
	}
}

func TestPullByDigest(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	tag := reg.ref(t, "org/repo:tag")
	host := build(t, image.HostPlatform(), 1)
	index := publishIndex(t, tag, build(t, other(), 1), host)
	manifest, err := host.Digest()
	require.NoError(t, err)

	t.Run("of the manifest", func(t *testing.T) {
		t.Parallel()

		ref := tag.Context().Digest(manifest.String())

		res, _, err := pull(t, t.TempDir(), ref)

		require.NoError(t, err)
		require.Equal(t, ref.Name(), res.Pinned())
	})

	t.Run("of the index", func(t *testing.T) {
		t.Parallel()

		ref := tag.Context().Digest(index.String())

		res, _, err := pull(t, t.TempDir(), ref)

		require.NoError(t, err)
		require.Equal(t, manifest, res.Digest)
		require.NotEqual(t, ref.Name(), res.Pinned())
	})
}

func TestPullDownloadsOnlyWhatTheStoreLacks(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	base := build(t, image.HostPlatform(), 2)
	publish(t, ref, base)
	root := t.TempDir()
	first, _, err := pull(t, root, ref)
	require.NoError(t, err)

	// The same tag again: the registry is asked, nothing comes down.
	again, facts, err := pull(t, root, ref)

	require.NoError(t, err)
	require.Equal(t, first.Pinned(), again.Pinned())
	require.True(t, again.Cached)
	require.Zero(t, again.LayersFetched)
	require.Zero(t, again.Bytes)
	require.Contains(t, facts, "2 layers, 0 missing")
	require.Contains(t, facts, "Status: up to date, nothing downloaded")

	// The tag moves to an image with one more layer: only that one comes down.
	extra, err := random.Layer(1024, types.DockerLayer)
	require.NoError(t, err)
	grown, err := mutate.AppendLayers(base, extra)
	require.NoError(t, err)
	moved := publish(t, ref, grown)

	res, facts, err := pull(t, root, ref)

	require.NoError(t, err)
	require.Equal(t, moved, res.Digest)
	require.Equal(t, 3, res.LayersTotal)
	require.Equal(t, 1, res.LayersFetched)
	require.False(t, res.Cached)
	require.Contains(t, facts, "3 layers, 1 missing")
	requireComplete(t, root, grown)
	index, err := layout.ImageIndexFromPath(filepath.Join(root, "images"))
	require.NoError(t, err)
	manifest, err := index.IndexManifest()
	require.NoError(t, err)
	require.Len(t, manifest.Manifests, 1)
	require.Equal(t, moved, manifest.Manifests[0].Digest)
}

func TestPullAsksTheRegistryEvenWhenCached(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	tag := reg.ref(t, "org/repo:tag")
	digest := publish(t, tag, build(t, image.HostPlatform(), 1))
	root := t.TempDir()
	_, _, err := pull(t, root, tag)
	require.NoError(t, err)
	// The registry goes away: what is in the store does not answer for it.
	down := hook(func(w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(http.StatusBadGateway)
		return true
	})
	reg.hook.Store(&down)

	for _, ref := range []name.Reference{tag, tag.Context().Digest(digest.String())} {
		_, _, err := pull(t, root, ref)

		require.Error(t, err)
		require.ErrorContains(t, err, reg.host)
	}
}

func TestPullKeepsNothingOfACorruptLayer(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	img := build(t, image.HostPlatform(), 2)
	publish(t, ref, img)
	bad := layers(t, img)[1]
	reg.onBlob(bad, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 1024))
	})
	root := t.TempDir()

	_, _, err := pull(t, root, ref)

	require.Error(t, err)
	require.ErrorContains(t, err, bad.String())
	require.NotContains(t, blobs(t, root), bad.String())
	require.Empty(t, dataFiles(t, filepath.Join(root, "tmp")))
}

func TestPullInterrupted(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	img := build(t, image.HostPlatform(), 2)
	publish(t, ref, img)
	first, second := layers(t, img)[0], layers(t, img)[1]
	root := t.TempDir()
	// The second layer comes half way, then the download hangs until the pull is interrupted.
	// The interrupt waits for the other layer to be filed, so that every run keeps the same
	// layer and drops the same one although the two come down together.
	ctx, cancel := context.WithCancelCause(t.Context())
	interrupted := errors.New("interrupt received")
	reg.onBlob(second, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("half of the layer"))
		_ = http.NewResponseController(w).Flush()
		for !stored(root, first) {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
		cancel(interrupted)
		<-r.Context().Done()
	})

	_, err := puller(t, root, io.Discard, nil).Pull(ctx, ref)

	require.ErrorIs(t, err, interrupted)
	require.Equal(t, []string{first.String()}, blobs(t, root))
	require.Empty(t, dataFiles(t, filepath.Join(root, "tmp")))

	// The next pull downloads the layer whole and only that one.
	reg.hook.Store(nil)
	res, _, err := pull(t, root, ref)

	require.NoError(t, err)
	require.Equal(t, 1, res.LayersFetched)
	requireComplete(t, root, img)
}

// stored reports whether the blob named h is filed in the store at root.
func stored(root string, h v1.Hash) bool {
	_, err := os.Stat(filepath.Join(root, "images", "blobs", "sha256", h.Hex))
	return err == nil
}

// holdBlobs holds every download of a blob for d before the registry serves it, and returns the
// highest number of them the registry had in flight at the same time.
func (reg *testRegistry) holdBlobs(d time.Duration) func() int64 {
	var live, peak atomic.Int64
	hk := hook(func(_ http.ResponseWriter, r *http.Request) bool {
		if r.Method != http.MethodGet || !strings.Contains(r.URL.Path, "/blobs/sha256:") {
			return false
		}
		n := live.Add(1)
		for p := peak.Load(); n > p; p = peak.Load() {
			if peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(d)
		live.Add(-1)
		return false
	})
	reg.hook.Store(&hk)
	return peak.Load
}

func TestPullDownloadsSeveralLayersAtOnceUnderABound(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	img := build(t, image.HostPlatform(), 5)
	publish(t, ref, img)
	peak := reg.holdBlobs(50 * time.Millisecond)
	var facts bytes.Buffer
	p := puller(t, t.TempDir(), &facts, nil)
	p.Terminal = true

	res, err := p.Pull(t.Context(), ref)

	require.NoError(t, err)
	require.Equal(t, 5, res.LayersFetched)
	// Three at a time: never a fourth, and three at least once.
	require.Equal(t, int64(3), peak())
	// A line of its own per layer, as docker draws them, and a line of facts once it is in.
	for _, digest := range layers(t, img) {
		require.Contains(t, facts.String(), digest.Hex[:12]+": 0 B / ", facts.String())
		require.Regexp(t, digest.Hex[:12]+`: [0-9.]+ kB in `, facts.String())
	}
	require.NotContains(t, facts.String(), "sha256:"+layers(t, img)[0].Hex)
}

func TestPullReportsTheFirstFailureOfTheManifest(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	img := build(t, image.HostPlatform(), 2)
	publish(t, ref, img)
	// The lower layer of the manifest fails first in time; the higher one fails after it.
	higher, lower := layers(t, img)[0], layers(t, img)[1]
	breakThem := func() {
		failed := make(chan struct{})
		hk := hook(func(w http.ResponseWriter, r *http.Request) bool {
			switch {
			case strings.HasSuffix(r.URL.Path, "/blobs/"+lower.String()):
				_, _ = w.Write(bytes.Repeat([]byte("x"), 1024))
				close(failed)
			case strings.HasSuffix(r.URL.Path, "/blobs/"+higher.String()):
				select {
				case <-failed:
				case <-r.Context().Done():
				}
				_, _ = w.Write(bytes.Repeat([]byte("y"), 1024))
			default:
				return false
			}
			return true
		})
		reg.hook.Store(&hk)
	}

	// Played twice: what a broken image says does not depend on which failure arrived first.
	for range 2 {
		breakThem()

		_, _, err := pull(t, t.TempDir(), ref)

		require.Error(t, err)
		require.ErrorContains(t, err, higher.String())
		require.NotContains(t, err.Error(), lower.String())
	}
}

func TestPullDownloadsALayerListedTwiceOnce(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	img := build(t, image.HostPlatform(), 2)
	twice, err := img.LayerByDigest(layers(t, img)[0])
	require.NoError(t, err)
	img, err = mutate.AppendLayers(img, twice)
	require.NoError(t, err)
	publish(t, ref, img)
	counts := reg.counting()
	manifest, err := img.Manifest()
	require.NoError(t, err)

	res, _, err := pull(t, t.TempDir(), ref)

	require.NoError(t, err)
	// The manifest lists three layers for two blobs, and the count is of blobs: a pull that
	// brought everything must not end on "2 of 3".
	require.Equal(t, 2, res.LayersTotal)
	require.Equal(t, 2, res.LayersFetched)
	require.Equal(t, downloaded(t, img, 0), res.Bytes)
	n, ok := counts.Load(manifest.Layers[0].Digest.String())
	require.True(t, ok)
	require.Equal(t, int64(1), n.(*atomic.Int64).Load()) //nolint:forcetypeassert // see counting.
}

// downloaded returns what a pull of img brings down when its first held layers are in the store
// already: the layers after them, the config and the manifest.
func downloaded(t *testing.T, img v1.Image, held int) int64 {
	manifest, err := img.Manifest()
	require.NoError(t, err)
	size := manifest.Config.Size
	raw, err := img.RawManifest()
	require.NoError(t, err)
	size += int64(len(raw))
	seen := make(map[v1.Hash]bool)
	for _, l := range manifest.Layers[held:] {
		if !seen[l.Digest] {
			seen[l.Digest] = true
			size += l.Size
		}
	}
	return size
}

func TestPullCountsWhatItBroughtDown(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	base := build(t, image.HostPlatform(), 3)
	publish(t, ref, base)
	root := t.TempDir()
	_, _, err := pull(t, root, ref)
	require.NoError(t, err)
	grown := base
	for range 7 {
		layer, err := random.Layer(1024, types.DockerLayer)
		require.NoError(t, err)
		grown, err = mutate.AppendLayers(grown, layer)
		require.NoError(t, err)
	}
	publish(t, ref, grown)

	res, _, err := pull(t, root, ref)

	require.NoError(t, err)
	require.Equal(t, 10, res.LayersTotal)
	require.Equal(t, 7, res.LayersFetched)
	require.Equal(t, downloaded(t, grown, 3), res.Bytes)
	require.False(t, res.Cached)
	requireComplete(t, root, grown)
}

func TestPullConcurrently(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	shared := build(t, image.HostPlatform(), 2)
	extra, err := random.Layer(1024, types.DockerLayer)
	require.NoError(t, err)
	grown, err := mutate.AppendLayers(shared, extra)
	require.NoError(t, err)
	refs := []name.Reference{reg.ref(t, "org/a:tag"), reg.ref(t, "org/a:tag"), reg.ref(t, "org/b:tag")}
	imgs := []v1.Image{shared, shared, grown}
	publish(t, refs[0], shared)
	publish(t, refs[2], grown)
	counts := reg.counting()
	root := t.TempDir()
	results := make([]image.Result, len(refs))
	errs := make([]error, len(refs))
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Go(func() {
			results[i], _, errs[i] = pull(t, root, ref)
		})
	}
	wg.Wait()

	require.NoError(t, errors.Join(errs...))
	require.Equal(t, results[0].Pinned(), results[1].Pinned())
	require.NotEqual(t, results[0].Pinned(), results[2].Pinned())
	for _, img := range imgs {
		requireComplete(t, root, img)
	}
	for _, digest := range layers(t, grown) {
		n, ok := counts.Load(digest.String())
		require.True(t, ok, digest)
		require.Equal(t, int64(1), n.(*atomic.Int64).Load(), digest) //nolint:forcetypeassert // see counting.
	}
	index, err := layout.ImageIndexFromPath(filepath.Join(root, "images"))
	require.NoError(t, err)
	manifest, err := index.IndexManifest()
	require.NoError(t, err)
	require.Len(t, manifest.Manifests, 2)
}

func TestPullDrawsProgressOnATerminalOnly(t *testing.T) {
	t.Parallel()

	reg := serve(t)
	ref := reg.ref(t, "org/repo:tag")
	publish(t, ref, build(t, image.HostPlatform(), 3))

	for _, terminal := range []bool{true, false} {
		var facts bytes.Buffer
		p := puller(t, t.TempDir(), &facts, nil)
		p.Terminal = terminal

		_, err := p.Pull(t.Context(), ref)

		require.NoError(t, err)
		require.Equal(t, terminal, strings.Contains(facts.String(), "\x1b["), facts.String())
		if terminal {
			// The block of the downloads is erased before the lines of facts take its place.
			require.Contains(t, facts.String(), "\x1b[3A\r\x1b[J")
		}
		// Nothing of the progress is left behind: what the pull wrote ends in a complete line,
		// and the reference stdout carries does not land at the end of a meter.
		require.True(t, strings.HasSuffix(facts.String(), "\n"), "%q", facts.String())
	}

	// Quiet: a puller without a log writes nowhere, meter included.
	quiet := puller(t, t.TempDir(), nil, nil)
	quiet.Terminal = true

	_, err := quiet.Pull(t.Context(), ref)

	require.NoError(t, err)
}

// dockerConfig returns the JSON of a docker config that knows the registry at host, or no
// registry when host is empty.
func dockerConfig(host string) []byte {
	if host == "" {
		return []byte(`{"auths": {}}`)
	}
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	return []byte(`{"auths": {"` + host + `": {"auth": "` + auth + `"}}}`)
}

// writeAuth writes the config that knows the registry at host to path.
func writeAuth(t *testing.T, path string, host string) {
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, dockerConfig(host), 0o600))
}

// keychainEnv names the reference the child process of a credential case pulls, with the
// credentials of the environment it inherits.
const keychainEnv = "COVE_TEST_KEYCHAIN"

// private serves an image that only the credentials reach, and returns its reference.
func private(t *testing.T) (*testRegistry, name.Reference) {
	reg := serve(t)
	ref := reg.ref(t, "org/private:tag")
	publish(t, ref, build(t, image.HostPlatform(), 1))
	reg.guard()
	return reg, ref
}

func TestPullDeniedWithoutCredentials(t *testing.T) {
	t.Parallel()

	reg, ref := private(t)
	root := t.TempDir()

	_, _, err := pull(t, root, ref)

	require.Error(t, err)
	require.ErrorContains(t, err, reg.host+" denied the access")
	require.ErrorContains(t, err, "401")
	require.Empty(t, blobs(t, root))
}

func TestPullReadsTheCredentialsTheUserConfigured(t *testing.T) {
	// Each case sets the environment: none of them is parallel, and the home is a fresh one so
	// that the docker config of the developer running the tests plays no part. The pull itself
	// runs in a process of its own, because docker computes its configuration directory once per
	// process: in a single one, every case after the first would read the home of the first.
	tests := []struct {
		name string
		// setup writes the credentials of the registry at host where the case wants them.
		setup func(t *testing.T, home string, host string)
		want  bool
	}{
		{
			name: "docker config",
			setup: func(t *testing.T, home string, host string) {
				writeAuth(t, filepath.Join(home, ".docker", "config.json"), host)
			},
			want: true,
		},
		{
			name: "DOCKER_CONFIG",
			setup: func(t *testing.T, _ string, host string) {
				dir := t.TempDir()
				writeAuth(t, filepath.Join(dir, "config.json"), host)
				t.Setenv("DOCKER_CONFIG", dir)
			},
			want: true,
		},
		{
			name: "REGISTRY_AUTH_FILE",
			setup: func(t *testing.T, _ string, host string) {
				path := filepath.Join(t.TempDir(), "auth.json")
				writeAuth(t, path, host)
				t.Setenv("REGISTRY_AUTH_FILE", path)
			},
			want: true,
		},
		{
			name: "containers/auth.json under XDG_RUNTIME_DIR",
			setup: func(t *testing.T, _ string, host string) {
				dir := t.TempDir()
				writeAuth(t, filepath.Join(dir, "containers", "auth.json"), host)
				t.Setenv("XDG_RUNTIME_DIR", dir)
			},
			want: true,
		},
		{
			name: "a docker config that does not know the registry hides containers/auth.json",
			setup: func(t *testing.T, home string, host string) {
				writeAuth(t, filepath.Join(home, ".docker", "config.json"), "")
				dir := t.TempDir()
				writeAuth(t, filepath.Join(dir, "containers", "auth.json"), host)
				t.Setenv("XDG_RUNTIME_DIR", dir)
			},
		},
		{
			name: "DOCKER_CONFIG replaces the home directory, it does not add to it",
			setup: func(t *testing.T, home string, host string) {
				writeAuth(t, filepath.Join(home, ".docker", "config.json"), host)
				t.Setenv("DOCKER_CONFIG", t.TempDir())
			},
		},
		{name: "nothing configured", setup: func(*testing.T, string, string) {}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			for _, v := range []string{"DOCKER_CONFIG", "REGISTRY_AUTH_FILE", "XDG_RUNTIME_DIR", "XDG_CONFIG_HOME"} {
				t.Setenv(v, "")
			}
			reg, ref := private(t)
			tt.setup(t, home, reg.host)

			// The default keychain is the one the user configured, through these files.
			out, err := pullWithTheKeychain(t, ref)

			if tt.want {
				require.NoError(t, err, out)
			} else {
				require.Error(t, err)
				require.Contains(t, out, "denied the access")
			}
		})
	}
}

// pullWithTheKeychain runs the test below in a child process, which inherits the environment the
// case has set, and returns what that process wrote.
func pullWithTheKeychain(t *testing.T, ref name.Reference) (string, error) {
	//nolint:gosec // G204: the program is the test binary itself and the arguments are fixed.
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=TestPullWithTheKeychainOfItsEnvironment")
	cmd.Env = append(os.Environ(), keychainEnv+"="+ref.Name())
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestPullWithTheKeychainOfItsEnvironment is the process a credential case starts, not a test of
// its own: it pulls the reference its environment names with the keychain of the user, and fails
// when the registry refuses. It does nothing in a plain run of the suite.
func TestPullWithTheKeychainOfItsEnvironment(t *testing.T) {
	t.Parallel()

	ref := os.Getenv(keychainEnv)
	if ref == "" {
		t.Skip("this process was not started by TestPullReadsTheCredentialsTheUserConfigured")
	}
	parsed, err := image.Parse(ref)
	require.NoError(t, err)

	_, err = puller(t, t.TempDir(), io.Discard, authn.DefaultKeychain).Pull(t.Context(), parsed)

	require.NoError(t, err)
}

func TestPullFormatsSizesForAHuman(t *testing.T) {
	t.Parallel()

	require.Equal(t, "78 B", image.FormatSize(78))
	require.Equal(t, "1.0 kB", image.FormatSize(1000))
	require.Equal(t, "12.3 MB", image.FormatSize(12_345_678))
	require.Equal(t, "4.5 GB", image.FormatSize(4_500_000_000))
}
