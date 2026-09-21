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
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"gitlab.com/hich-hich/cove/internal/unpack"
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

	// live is what the pull in flight says while its layers come down, nil at any other time: a
	// warning of the library goes through it rather than straight to Log, which would leave the
	// cursor off the board.
	live atomic.Pointer[report]
}

// Warnings returns where the warnings of the registry library must be sent while p pulls: it
// retries a request on its own and says so, on the same stream as the board of the downloads. A
// line written straight to that stream, from the goroutine of any layer, would leave the cursor
// inside the block, and the next redraw would then erase the wrong lines. Through this writer a
// warning is one more line of facts above the block.
func (p *Puller) Warnings() io.Writer { return warnings{p: p} }

// warnings turns what the library logs into the lines of facts of a pull.
type warnings struct {
	p *Puller
}

// Write says each line of b, above the board when layers are coming down.
func (w warnings) Write(b []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if live := w.p.live.Load(); live != nil {
			live.say("%s", line)
			continue
		}
		w.p.logf("%s", line)
	}
	return len(b), nil
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
	// LayersTotal counts the blobs the layers of the manifest name, a layer listed twice counting
	// once, LayersFetched those that were downloaded. The store holds one blob per digest, so
	// that is the unit a pull brings, and the two counts must be of the same unit to be compared.
	LayersTotal   int
	LayersFetched int
	// Bytes counts what was downloaded, layers, config and manifest.
	Bytes int64
	// Cached says that nothing was downloaded: the image was in the store already.
	Cached bool
	// Entries counts what the layers hold, every layer of the manifest read whether its blob
	// came down or was in the store already.
	Entries int
	// Unpacked counts what reading those layers found and could not write as the archive
	// declared it: the names bounded to the root, and the extended attributes EROFS will not
	// read in the guest.
	Unpacked unpack.Counts

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
	start := time.Now()
	platform := HostPlatform()
	img, digest, err := p.resolve(ctx, ref, platform)
	if err != nil {
		return Result{}, err
	}
	res := Result{Ref: ref.Name(), Digest: digest, Platform: platform, repository: ref.Context()}
	if err := p.fetch(ctx, ref, img, &res); err != nil {
		return Result{}, err
	}
	p.summarize(res, time.Since(start))
	return res, nil
}

// summarize ends a pull the way docker ends one, a label to a line: what was pulled, then what
// became of it, or that the image was there already, since a pull that downloads nothing would
// otherwise end on the line that said nothing was missing and look like one that gave up. The
// digest is said again although the line of the manifest carried it: it belongs next to the
// outcome, where the eye lands, and stdout keeps the reference by digest to itself. What the
// layers held comes last, and on a pull that downloaded nothing as well: every layer of the
// manifest is read, whether its blob came down or was in the store already.
func (p *Puller) summarize(res Result, took time.Duration) {
	p.logf("Digest: %s", res.Digest)
	if res.Cached {
		p.logf("Status: up to date, nothing downloaded")
	} else {
		p.logf("Status: downloaded %d of %d layers, %s in %s", res.LayersFetched, res.LayersTotal,
			formatSize(res.Bytes), took.Round(10*time.Millisecond))
	}
	p.logf("Unpacked: %d layers, %d entries, %d names bounded, %d attributes EROFS will not read",
		res.LayersTotal, res.Entries, res.Unpacked.NormalizedEntries, res.Unpacked.UnknownXattrPrefixes)
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
		return nil, v1.Hash{}, fmt.Errorf("%w: %s is built for %s, not %s", ErrPlatform, ref.Name(),
			platformName(&built), platform)
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
	entry, offered := pick(manifest.Manifests, platform)
	if entry == nil {
		return nil, v1.Hash{}, noPlatform(ref, offered, platform)
	}
	img, err := idx.Image(entry.Digest)
	if err != nil {
		return nil, v1.Hash{}, fmt.Errorf("read the manifest %s: %w", entry.Digest, err)
	}
	return img, entry.Digest, nil
}

// pick returns the entry of an index serving platform, nil when none does, and the platforms the
// other entries offer, each named for a message. An attestation of buildkit, which sits in the
// index under unknown/unknown and is not an image, is left out of both.
func pick(entries []v1.Descriptor, platform v1.Platform) (*v1.Descriptor, []string) {
	var offered []string
	for i, m := range entries {
		if m.Platform != nil {
			if m.Platform.OS == unknown || m.Platform.Architecture == unknown {
				continue
			}
			if matches(*m.Platform, platform) {
				return &entries[i], nil
			}
		}
		// The platform is optional in an entry of an index: such an entry cannot serve the host,
		// and it is counted among what the index offers so that the message never hides it.
		offered = append(offered, platformName(m.Platform))
	}
	return nil, offered
}

// noPlatform says that the index of ref has nothing for platform, naming the platforms it offers
// instead, or saying that it holds no image at all when attestations are all there is.
func noPlatform(ref name.Reference, offered []string, platform v1.Platform) error {
	if len(offered) == 0 {
		return fmt.Errorf("%w: %s is an index that holds no image, only attestations, so none for %s",
			ErrPlatform, ref.Name(), platform)
	}
	return fmt.Errorf("%w: %s is published for %s, not %s", ErrPlatform, ref.Name(),
		strings.Join(offered, ", "), platform)
}

// platformName names in a message the platform of a manifest. An entry of an index may leave it
// out, and a config may leave the OS out, either of which makes String write nothing at all,
// which would leave the message with a hole where a platform belongs.
func platformName(p *v1.Platform) string {
	if p == nil || p.String() == "" {
		return "an unstated platform"
	}
	return p.String()
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
	res.LayersTotal = distinct(manifest.Layers)
	if err := p.fetchLayers(ctx, ref, img, manifest.Layers, res); err != nil {
		return err
	}
	if err := p.fetchDescription(ctx, ref, img, manifest.Config.Digest, res); err != nil {
		return err
	}
	res.Cached = res.Bytes == 0
	return p.record(ctx, ref, img, res)
}

// downloadsAtOnce bounds how many layers come down at the same time. Three is the figure docker
// and the CRI plugin of containerd both settled on: a registry limits the rate of one client, and
// past a few connections the gain goes while the risk of being throttled stays. It is a constant
// and not an option, since the setting docker exposes lives in its daemon and cove has none; a
// measurement may move it later.
const downloadsAtOnce = 3

// fetchLayers brings the layers of descs the store lacks and reads all of them, and logs how many
// there are to bring and their size before the first. Nothing waits for the layer under it: each
// layer is a blob of its own, and the order they arrive in does not matter to what is written.
func (p *Puller) fetchLayers(ctx context.Context, ref name.Reference, img v1.Image, descs []v1.Descriptor,
	res *Result,
) error {
	layers, missing, size, err := p.plan(ref, img, descs)
	if err != nil {
		return err
	}
	// The line that opens a pull waits for the manifest: what is being pulled and what it costs
	// belong together, and only the registry can tell the second. The digest of the manifest is
	// not in it, since the pull ends on it.
	p.logf("pulling %s for %s: %d layers, %d missing (%s)", ref.Name(), res.Platform, res.LayersTotal,
		missing, formatSize(size))
	rep := p.report(res)
	p.live.Store(rep)
	defer p.live.Store(nil)
	return rep.run(ctx, layers)
}

// layer is one blob of the manifest and what the pull does with it: bring it down when the store
// lacks it, then read every entry it holds.
type layer struct {
	desc v1.Descriptor
	// diffID names the layer wherever it is spoken of: it is what the rootfs the layer unpacks
	// to is keyed by, where its digest only names the bytes on the wire.
	diffID v1.Hash
	// open serves the bytes of the registry, nil when the store holds the blob already.
	open func() (io.ReadCloser, error)
	// stored is closed once the blob is in the store: what the reading of the layer waits for.
	stored chan struct{}
}

// plan returns one entry per blob the layers of descs name, in the order of the manifest, how
// many of them the store lacks and their size added up. A manifest may list the same layer twice,
// which is legal and happens: the store holds one blob for it and it unpacks to the same rootfs,
// so it is brought down once and read once.
func (p *Puller) plan(ref name.Reference, img v1.Image, descs []v1.Descriptor) ([]layer, int, int64, error) {
	ids, err := diffIDs(ref, img, descs)
	if err != nil {
		return nil, 0, 0, err
	}
	layers := make([]layer, 0, len(descs))
	var missing int
	var size int64
	seen := make(map[v1.Hash]bool, len(descs))
	for i, desc := range descs {
		if seen[desc.Digest] {
			continue
		}
		seen[desc.Digest] = true
		l := layer{desc: desc, diffID: ids[i], stored: make(chan struct{})}
		has, err := p.Store.Has(desc.Digest)
		if err != nil {
			return nil, 0, 0, err
		}
		if has {
			close(l.stored)
		} else {
			// The layers are taken from the manifest before the first download starts, so that a
			// goroutine has nothing left to read of img.
			blob, err := img.LayerByDigest(desc.Digest)
			if err != nil {
				return nil, 0, 0, registryError(ref, err)
			}
			l.open = blob.Compressed
			missing++
			size += desc.Size
		}
		layers = append(layers, l)
	}
	return layers, missing, size, nil
}

// diffIDs returns the diff id of each layer of descs, in the order of the manifest: the digest of
// the layer once decompressed, which the config of the image lists. An image whose config does
// not name one per layer is refused, since a layer would then be spoken of under the name of
// another.
func diffIDs(ref name.Reference, img v1.Image, descs []v1.Descriptor) ([]v1.Hash, error) {
	config, err := img.ConfigFile()
	if err != nil {
		return nil, registryError(ref, err)
	}
	if len(config.RootFS.DiffIDs) != len(descs) {
		return nil, fmt.Errorf("%s: the config names %d diff ids for %d layers", ref.Name(),
			len(config.RootFS.DiffIDs), len(descs))
	}
	return config.RootFS.DiffIDs, nil
}

// errOtherLayer ends the downloads still to come once one layer has failed. The failure itself is
// not used as the cause: a layer stopped by it reports what ended its wait, and the cause of a
// blob that is fine must not be the corruption of another one.
var errOtherLayer = errors.New("another layer failed")

// run brings down the layers the store lacks and reads every layer of the manifest, the two
// stages going at once: a layer is read as soon as its blob is in, while the others are still
// coming down. The first failure of either stage ends what has not run yet, and the error given
// back is the one of the layer that comes first in the manifest, not the one that failed first:
// two pulls of the same broken image say the same thing.
func (r *report) run(ctx context.Context, layers []layer) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	downloads, unpacks := make([]error, len(layers)), make([]error, len(layers))
	tokens := make(chan struct{}, unpacksAtOnce)
	var wg sync.WaitGroup
	wg.Go(func() { r.download(ctx, cancel, layers, downloads) })
	for i, l := range layers {
		wg.Go(func() {
			if err := r.unpack(ctx, l, tokens); err != nil {
				unpacks[i] = err
				cancel(errOtherLayer)
			}
		})
	}
	wg.Wait()
	r.done()
	for i := range layers {
		// A layer that only stopped because another one failed is not that failure: the layer that
		// failed has its own entry, and it is the one to name.
		for _, err := range []error{downloads[i], unpacks[i]} {
			if err != nil && !errors.Is(err, errOtherLayer) {
				return err
			}
		}
	}
	// Nothing failed and both stages are over: only the caller can have ended the context, and a
	// layer that never started must not pass for a layer that was brought in.
	if ctx.Err() != nil {
		return fmt.Errorf("pull the layers: %w", context.Cause(ctx))
	}
	return nil
}

// download brings the layers of layers the store lacks, no more than downloadsAtOnce at a time,
// and tells each of them that its blob is in. The first failure ends the downloads still to come
// and is left in errs, at the place of its layer.
func (r *report) download(ctx context.Context, cancel context.CancelCauseFunc, layers []layer, errs []error) {
	tokens := make(chan struct{}, downloadsAtOnce)
	var wg sync.WaitGroup
	for i, l := range layers {
		if l.open == nil {
			continue
		}
		select {
		case tokens <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Go(func() {
			defer func() { <-tokens }()
			if err := r.fetchLayer(ctx, l.desc, l.open); err != nil {
				errs[i] = err
				cancel(errOtherLayer)
				return
			}
			close(l.stored)
		})
	}
	wg.Wait()
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
		fetched, err := p.Store.Put(ctx, blob.digest, bytesReader(blob.raw), p.waiting(blob.digest))
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

// distinct counts the blobs the layers of descs name, a layer listed twice counting once.
func distinct(descs []v1.Descriptor) int {
	seen := make(map[v1.Hash]bool, len(descs))
	for _, desc := range descs {
		seen[desc.Digest] = true
	}
	return len(seen)
}

// report is what the layers coming down together say: the board that draws them, the lines of
// facts and the counts of the result. One lock covers the three, so that a line never lands in
// the middle of another, two layers never draw at once, and the counts hold.
type report struct {
	p     *Puller
	res   *Result
	board *board
	mu    sync.Mutex
}

// report returns what a pull says on the way.
func (p *Puller) report(res *Result) *report {
	return &report{p: p, res: res, board: p.board()}
}

// fetchLayer brings one layer into the store, a line of the board following it down, and says
// once it is in what it took: the layer, the size and the time.
func (r *report) fetchLayer(ctx context.Context, desc v1.Descriptor, open func() (io.ReadCloser, error)) error {
	start := time.Now()
	g := r.start(desc)
	fetched, err := r.p.Store.Put(ctx, desc.Digest, open, Progress{
		OnWait: func() { r.say("%s: waiting for another pull that downloads it", short(desc.Digest)) },
		OnRead: func(n int64) { r.advance(g, n) },
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case err != nil:
		r.board.drop(g)
		return err
	case !fetched:
		r.board.finish(g, "%s: found in the store while waiting", short(desc.Digest))
		return nil
	}
	r.res.LayersFetched++
	r.res.Bytes += desc.Size
	r.board.finish(g, "%s: %s in %s", short(desc.Digest), formatSize(desc.Size),
		time.Since(start).Round(10*time.Millisecond))
	return nil
}

// start puts the line of the layer desc on the board.
func (r *report) start(desc v1.Descriptor) *gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.board.start(short(desc.Digest), desc.Size)
}

// advance counts n more bytes of the layer g follows.
func (r *report) advance(g *gauge, n int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.board.advance(g, n)
}

// say writes one line of facts above the lines of what is coming down.
func (r *report) say(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.board.say(format, args...)
}

// done takes the board off the screen: the downloads are over, whether they all came in or not.
func (r *report) done() {
	r.board.clear()
}

// waiting returns what Put tells of a blob that is already in hand: the wait for another pull
// that holds it, and no line on the board, since nothing of it comes down the network.
func (p *Puller) waiting(digest v1.Hash) Progress {
	return Progress{OnWait: func() { p.logf("%s: waiting for another pull that downloads it", short(digest)) }}
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
