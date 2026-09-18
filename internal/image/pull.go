package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// ErrPlatform reports an image that is not published for the platform of the host.
var ErrPlatform = errors.New("no image for the platform of the host")

// unknown is the OS and the architecture buildkit gives the attestations it puts in an index.
const unknown = "unknown"

// HostPlatform is the platform an image must be published for: Linux, the OS of the VM whatever
// the host runs, on the architecture of the CPU of the host, since no backend translates
// instructions.
func HostPlatform() v1.Platform {
	return v1.Platform{OS: "linux", Architecture: runtime.GOARCH}
}

// Puller brings images into a store and says what it does on Log.
type Puller struct {
	Store *Store
	// Log receives the facts of the pull, one line each; nil keeps quiet.
	Log io.Writer
	// Terminal says that Log is a terminal, where the progress of a download is redrawn in place
	// and erased once the download is done. Off a terminal nothing is redrawn, so that a journal
	// or a pipe stays readable.
	Terminal bool
	// Keychain gives the credentials of the registry; nil is the default keychain of the library,
	// what docker login and podman login configured.
	Keychain authn.Keychain
}

// Result is what a pull found and did.
type Result struct {
	// Ref is the reference asked for, in its canonical form.
	Ref string
	// Digest names the manifest of the platform of the host, never an index: what ran must stay
	// unambiguous, the reference asked for being the one of the index or not.
	Digest v1.Hash
	// Platform is the one the manifest is for.
	Platform v1.Platform
	// LayersTotal counts the layers of the manifest, LayersFetched those that were downloaded.
	LayersTotal   int
	LayersFetched int
	// Bytes counts what was downloaded, layers, config and manifest.
	Bytes int64
	// Cached says that nothing was downloaded: the image was in the store already.
	Cached bool

	// repository is what Ref names, without tag or digest.
	repository name.Repository
}

// Pinned returns the reference by digest of the manifest pulled: what to give run.
func (r Result) Pinned() string {
	return r.repository.Digest(r.Digest.String()).Name()
}

// Pull brings the image ref names into the store for the platform of the host and returns what it
// did. The registry is asked what ref designates on every call, as docker pull does: a tag that
// moved brings the new image, and nothing is pulled without the registry, by digest included. Only
// what the store lacks is downloaded, each blob verified against the digest that names it, and the
// image is recorded in the index once every blob is in.
func (p *Puller) Pull(ctx context.Context, ref name.Reference) (Result, error) {
	platform := HostPlatform()
	p.logf("pulling %s for %s", ref.Name(), platform)
	img, digest, err := p.resolve(ctx, ref, platform)
	if err != nil {
		return Result{}, err
	}
	res := Result{Ref: ref.Name(), Digest: digest, Platform: platform, repository: ref.Context()}
	if err := p.fetch(ctx, ref, img, &res); err != nil {
		return Result{}, err
	}
	return res, nil
}

// resolve asks the registry what ref designates and returns the image of platform with the
// digest of its manifest. An index is searched for that platform; a manifest alone, which many
// private images are published as, is checked through its config, which carries its OS and
// architecture. Either way an image of another platform is refused before a byte enters the
// store, the message saying what was looked for and what the image offers.
func (p *Puller) resolve(ctx context.Context, ref name.Reference, platform v1.Platform) (v1.Image, v1.Hash, error) {
	keychain := p.Keychain
	if keychain == nil {
		keychain = authn.DefaultKeychain
	}
	desc, err := remote.Get(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(keychain),
		remote.WithPlatform(platform))
	if err != nil {
		return nil, v1.Hash{}, registryError(ref, err)
	}
	if desc.MediaType.IsIndex() {
		img, digest, err := fromIndex(desc, ref, platform)
		if err != nil && !errors.Is(err, ErrPlatform) {
			return nil, v1.Hash{}, registryError(ref, err)
		}
		return img, digest, err
	}
	img, err := desc.Image()
	if err != nil {
		return nil, v1.Hash{}, registryError(ref, err)
	}
	config, err := img.ConfigFile()
	if err != nil {
		return nil, v1.Hash{}, registryError(ref, err)
	}
	if built := (v1.Platform{OS: config.OS, Architecture: config.Architecture}); !matches(built, platform) {
		return nil, v1.Hash{}, fmt.Errorf("%w: %s is built for %s, not %s", ErrPlatform, ref.Name(), built, platform)
	}
	return img, desc.Digest, nil
}

// fromIndex picks in the index desc the manifest of platform. The error names the platforms the
// index offers when none matches.
func fromIndex(desc *remote.Descriptor, ref name.Reference, platform v1.Platform) (v1.Image, v1.Hash, error) {
	idx, err := desc.ImageIndex()
	if err != nil {
		return nil, v1.Hash{}, fmt.Errorf("read the index: %w", err)
	}
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, v1.Hash{}, fmt.Errorf("read the index: %w", err)
	}
	var offered []string
	for _, m := range manifest.Manifests {
		// An attestation of buildkit sits in the index under unknown/unknown: not an image.
		if m.Platform == nil || m.Platform.OS == unknown || m.Platform.Architecture == unknown {
			continue
		}
		if matches(*m.Platform, platform) {
			img, err := idx.Image(m.Digest)
			if err != nil {
				return nil, v1.Hash{}, fmt.Errorf("read the manifest %s: %w", m.Digest, err)
			}
			return img, m.Digest, nil
		}
		offered = append(offered, m.Platform.String())
	}
	return nil, v1.Hash{}, fmt.Errorf("%w: %s is published for %s, not %s", ErrPlatform, ref.Name(),
		strings.Join(offered, ", "), platform)
}

// matches reports whether built serves wanted: same OS and architecture. The variant is not
// compared: linux/arm64/v8 is the arm64 of every host cove runs on.
func matches(built, wanted v1.Platform) bool {
	return built.OS == wanted.OS && built.Architecture == wanted.Architecture
}

// fetch brings the blobs of img the store lacks, layers first and the manifest last, so that a
// manifest in the store always has its blobs, then records the image under the reference asked
// for. It fills res with what it did.
func (p *Puller) fetch(ctx context.Context, ref name.Reference, img v1.Image, res *Result) error {
	manifest, err := img.Manifest()
	if err != nil {
		return registryError(ref, err)
	}
	res.LayersTotal = len(manifest.Layers)
	if err := p.fetchLayers(ctx, ref, img, manifest.Layers, res); err != nil {
		return err
	}
	if err := p.fetchDescription(ctx, ref, img, manifest.Config.Digest, res); err != nil {
		return err
	}
	res.Cached = res.Bytes == 0
	return p.record(ctx, ref, img, res)
}

// fetchLayers brings the layers of descs the store lacks, one at a time, and logs how many there
// are to bring and their size before the first.
func (p *Puller) fetchLayers(ctx context.Context, ref name.Reference, img v1.Image, descs []v1.Descriptor,
	res *Result,
) error {
	missing, size, err := p.missing(descs)
	if err != nil {
		return err
	}
	p.logf("manifest %s: %d layers, %d missing (%s)", res.Digest, res.LayersTotal, len(missing), formatSize(size))
	for _, desc := range missing {
		layer, err := img.LayerByDigest(desc.Digest)
		if err != nil {
			return registryError(ref, err)
		}
		if err := p.fetchLayer(ctx, desc, layer.Compressed, res); err != nil {
			return err
		}
	}
	return nil
}

// fetchDescription brings the config and the manifest of img, both already read from the
// registry and small, in that order: the manifest comes last.
func (p *Puller) fetchDescription(ctx context.Context, ref name.Reference, img v1.Image, config v1.Hash,
	res *Result,
) error {
	rawConfig, err := img.RawConfigFile()
	if err != nil {
		return registryError(ref, err)
	}
	rawManifest, err := img.RawManifest()
	if err != nil {
		return registryError(ref, err)
	}
	for _, blob := range []struct {
		digest v1.Hash
		raw    []byte
	}{{config, rawConfig}, {res.Digest, rawManifest}} {
		size := int64(len(blob.raw))
		progress, _ := p.progress(blob.digest, size)
		fetched, err := p.Store.Put(ctx, blob.digest, bytesReader(blob.raw), progress)
		if err != nil {
			return err
		}
		if fetched {
			res.Bytes += size
		}
	}
	return nil
}

// record puts the image in the index of the store under the reference asked for, with the
// platform it is for.
func (p *Puller) record(ctx context.Context, ref name.Reference, img v1.Image, res *Result) error {
	mediaType, err := img.MediaType()
	if err != nil {
		return registryError(ref, err)
	}
	size, err := img.Size()
	if err != nil {
		return registryError(ref, err)
	}
	platform := res.Platform
	return p.Store.Record(ctx, v1.Descriptor{
		MediaType:   mediaType,
		Size:        size,
		Digest:      res.Digest,
		Platform:    &platform,
		Annotations: map[string]string{RefName: ref.Name()},
	})
}

// missing returns the layers of descs the store lacks, and their size added up.
func (p *Puller) missing(descs []v1.Descriptor) ([]v1.Descriptor, int64, error) {
	var missing []v1.Descriptor
	var size int64
	for _, desc := range descs {
		has, err := p.Store.Has(desc.Digest)
		if err != nil {
			return nil, 0, err
		}
		if !has {
			missing = append(missing, desc)
			size += desc.Size
		}
	}
	return missing, size, nil
}

// fetchLayer brings one layer into the store, its progress on Log, and logs the fact once done:
// the digest, the size and the time it took.
func (p *Puller) fetchLayer(ctx context.Context, desc v1.Descriptor, open func() (io.ReadCloser, error),
	res *Result,
) error {
	start := time.Now()
	progress, meter := p.progress(desc.Digest, desc.Size)
	fetched, err := p.Store.Put(ctx, desc.Digest, open, progress)
	meter.clear()
	if err != nil {
		return err
	}
	if !fetched {
		p.logf("%s: found in the store while waiting", desc.Digest)
		return nil
	}
	res.LayersFetched++
	res.Bytes += desc.Size
	p.logf("%s: %s in %s", desc.Digest, formatSize(desc.Size), time.Since(start).Round(10*time.Millisecond))
	return nil
}

// progress returns what Put tells of the blob digest, the wait for another pull on Log and the
// bytes received on the meter of the terminal, with that meter for the caller to clear.
func (p *Puller) progress(digest v1.Hash, size int64) (Progress, *meter) {
	meter := p.meter(digest, size)
	return Progress{
		OnWait: func() { p.logf("%s: waiting for another pull that downloads it", digest) },
		OnRead: meter.advance,
	}, meter
}

// bytesReader returns an open function serving raw.
func bytesReader(raw []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
}

// logf writes one line of facts to Log.
func (p *Puller) logf(format string, args ...any) {
	if p.Log != nil {
		_, _ = fmt.Fprintf(p.Log, format+"\n", args...)
	}
}

// registryError says which registry failed. For a refusal of the credentials it says where cove
// took them from, since the message of the library names the URL and the status, not the files
// it read: the first one found is the only one read, so a docker config that does not know the
// registry hides a containers/auth.json that does.
func registryError(ref name.Reference, err error) error {
	registry := ref.Context().RegistryStr()
	if terr, ok := errors.AsType[*transport.Error](err); ok &&
		(terr.StatusCode == http.StatusUnauthorized || terr.StatusCode == http.StatusForbidden) {
		return fmt.Errorf("%s denied the access to %s (%d %s); the credentials are read from the docker config "+
			"($DOCKER_CONFIG or ~/.docker), else the file $REGISTRY_AUTH_FILE names, else containers/auth.json "+
			"under $XDG_RUNTIME_DIR or $XDG_CONFIG_HOME, the first found only: %w",
			registry, ref.Context().RepositoryStr(), terr.StatusCode, http.StatusText(terr.StatusCode), err)
	}
	return fmt.Errorf("%s: %w", registry, err)
}
