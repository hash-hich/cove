package image_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/image"
)

// tagged is the reference the store tests record.
const tagged = "ghcr.io/org/repo:tag"

// content is what the store tests file as a blob.
var content = []byte("a layer of the image")

// digestOf returns the hash that names content.
func digestOf(content []byte) v1.Hash {
	sum := sha256.Sum256(content)
	return v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(sum[:])}
}

// bytesOf returns an open function serving content.
func bytesOf(content []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(content)), nil }
}

// never fails the test when called: the blob was not to be downloaded.
func never(t *testing.T) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		t.Error("the blob was downloaded")
		return nil, errors.New("downloaded")
	}
}

// tmpOf returns where the store at root keeps its downloads.
func tmpOf(root string) string {
	return filepath.Join(root, "tmp")
}

// blobOf returns where the store at root files the blob named h.
func blobOf(root string, h v1.Hash) string {
	return filepath.Join(root, "images", "blobs", "sha256", h.Hex)
}

// hold takes the lock of the blob named h in the store at root, as another process would, and
// returns the file that holds it: closing it releases the lock, as the death of that process does.
func hold(t *testing.T, root string, h v1.Hash) *os.File {
	f, err := os.OpenFile(filepath.Join(tmpOf(root), h.Hex+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX))
	return f
}

// dataFiles returns the names of the files of dir that are not locks.
func dataFiles(t *testing.T, dir string) []string {
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") {
			names = append(names, e.Name())
		}
	}
	return names
}

// entry returns a descriptor of a manifest recorded under ref.
func entry(ref string, h v1.Hash) v1.Descriptor {
	return v1.Descriptor{
		MediaType:   types.OCIManifestSchema1,
		Size:        1,
		Digest:      h,
		Annotations: map[string]string{image.RefName: ref},
	}
}

func TestOpenCreatesTheLayout(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "missing", "cove")

	store, err := image.Open(root)

	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "images"), store.Layout())
	require.DirExists(t, filepath.Join(root, "tmp"))
	require.FileExists(t, filepath.Join(root, "images", "oci-layout"))
	idx, err := layout.ImageIndexFromPath(store.Layout())
	require.NoError(t, err)
	manifest, err := idx.IndexManifest()
	require.NoError(t, err)
	require.Empty(t, manifest.Manifests)
	// The layout holds what the format names and nothing else.
	entries, err := os.ReadDir(store.Layout())
	require.NoError(t, err)
	require.Len(t, entries, 3)
}

func TestOpenRefusesAnUnwritableRoot(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	// The parent is unwritable for the test and writable again for the cleanup of TempDir.
	require.NoError(t, os.Chmod(parent, 0o500))                       //nolint:gosec // G302: the point of the test.
	t.Cleanup(func() { require.NoError(t, os.Chmod(parent, 0o700)) }) //nolint:gosec // G302: see above.
	root := filepath.Join(parent, "cove")

	_, err := image.Open(root)

	require.Error(t, err)
	require.ErrorContains(t, err, root)
}

func TestPutFilesAVerifiedBlob(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := image.Open(root)
	require.NoError(t, err)
	h := digestOf(content)
	var read int64

	fetched, err := store.Put(t.Context(), h, bytesOf(content), image.Progress{OnRead: func(n int64) { read += n }})

	require.NoError(t, err)
	require.True(t, fetched)
	require.Equal(t, int64(len(content)), read)
	has, err := store.Has(h)
	require.NoError(t, err)
	require.True(t, has)
	got, err := layout.Path(store.Layout()).Bytes(h)
	require.NoError(t, err)
	require.Equal(t, content, got)
	require.Empty(t, dataFiles(t, tmpOf(root)))

	// What is there is not downloaded a second time.
	fetched, err = store.Put(t.Context(), h, never(t), image.Progress{})

	require.NoError(t, err)
	require.False(t, fetched)
}

func TestPutRefusesAnAlgorithmOtherThanSha256(t *testing.T) {
	t.Parallel()

	store, err := image.Open(t.TempDir())
	require.NoError(t, err)

	_, err = store.Put(t.Context(), v1.Hash{Algorithm: "sha512", Hex: "00"}, never(t), image.Progress{})

	require.ErrorContains(t, err, "only sha256")
}

func TestPutKeepsNothingOfAFailedDownload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		open func() (io.ReadCloser, error)
		want error
	}{
		{name: "content does not match", open: bytesOf([]byte("not the layer")), want: image.ErrCorrupt},
		{name: "truncated", open: bytesOf(content[:5]), want: image.ErrCorrupt},
		{
			name: "read error",
			open: func() (io.ReadCloser, error) {
				return io.NopCloser(io.MultiReader(bytes.NewReader(content[:5]), iotestErr{})), nil
			},
			want: errReader,
		},
		{name: "open error", open: func() (io.ReadCloser, error) { return nil, errReader }, want: errReader},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			store, err := image.Open(root)
			require.NoError(t, err)
			h := digestOf(content)

			_, err = store.Put(t.Context(), h, tt.open, image.Progress{})

			require.ErrorIs(t, err, tt.want)
			require.ErrorContains(t, err, h.String())
			require.NoFileExists(t, blobOf(root, h))
			require.Empty(t, dataFiles(t, tmpOf(root)))
		})
	}
}

// errReader is what iotestErr fails with.
var errReader = errors.New("the network went away")

// iotestErr is a reader that fails at once.
type iotestErr struct{}

func (iotestErr) Read([]byte) (int, error) { return 0, errReader }

func TestPutDownloadsASharedBlobOnce(t *testing.T) {
	t.Parallel()

	store, err := image.Open(t.TempDir())
	require.NoError(t, err)
	h := digestOf(content)
	started := make(chan struct{})
	release := make(chan struct{})
	waited := make(chan struct{})
	// The first pull holds the lock of the blob once its download opens, and keeps it until it is
	// let go.
	first := func() (io.ReadCloser, error) { //nolint:unparam // the signature is the one Put takes.
		close(started)
		<-release
		return io.NopCloser(bytes.NewReader(content)), nil
	}
	var wg sync.WaitGroup
	var fetchedFirst, fetchedSecond bool
	var errFirst, errSecond error
	wg.Go(func() {
		fetchedFirst, errFirst = store.Put(t.Context(), h, first, image.Progress{})
	})
	<-started
	wg.Go(func() {
		fetchedSecond, errSecond = store.Put(t.Context(), h, never(t), image.Progress{
			OnWait: func() { close(waited) },
		})
	})

	// The second pull announces its wait before the first has finished; then the first is let go.
	<-waited
	close(release)
	wg.Wait()

	require.NoError(t, errFirst)
	require.NoError(t, errSecond)
	require.True(t, fetchedFirst)
	require.False(t, fetchedSecond)
}

func TestPutTakesOverFromAHolderThatDied(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := image.Open(root)
	require.NoError(t, err)
	h := digestOf(content)
	// Another process holds the lock of the blob and dies with it: the kernel releases it.
	f := hold(t, root, h)
	waited := make(chan struct{})
	var wg sync.WaitGroup
	var fetched bool
	wg.Go(func() {
		fetched, err = store.Put(t.Context(), h, bytesOf(content), image.Progress{OnWait: func() { close(waited) }})
	})

	<-waited
	require.NoError(t, f.Close())
	wg.Wait()

	require.NoError(t, err)
	require.True(t, fetched)
}

func TestPutStopsWaitingWhenInterrupted(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := image.Open(root)
	require.NoError(t, err)
	h := digestOf(content)
	f := hold(t, root, h)
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	ctx, cancel := context.WithCancelCause(t.Context())
	interrupted := errors.New("interrupt received")

	_, err = store.Put(ctx, h, never(t), image.Progress{OnWait: func() { cancel(interrupted) }})

	require.ErrorIs(t, err, interrupted)
}

func TestOpenSweepsWhatNobodyHolds(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, err := image.Open(root)
	require.NoError(t, err)
	tmp := tmpOf(root)
	held := digestOf([]byte("held"))
	// Two pulls died on two images; a third one is alive, on its lock, with its download.
	for _, name := range []string{
		digestOf([]byte("a")).Hex + ".123", digestOf([]byte("b")).Hex + ".456", "index.json.789", held.Hex + ".1",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(tmp, name), []byte("partial"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "orphan.lock"), nil, 0o600))
	f := hold(t, root, held)
	t.Cleanup(func() { require.NoError(t, f.Close()) })

	_, err = image.Open(root)

	require.NoError(t, err)
	require.Equal(t, []string{held.Hex + ".1"}, dataFiles(t, tmp))
	require.FileExists(t, filepath.Join(tmp, "orphan.lock"))
	require.FileExists(t, filepath.Join(tmp, "index.json.lock"))
}

func TestRecordKeepsOneEntryPerReference(t *testing.T) {
	t.Parallel()

	store, err := image.Open(t.TempDir())
	require.NoError(t, err)
	old, moved, other := digestOf([]byte("old")), digestOf([]byte("moved")), digestOf([]byte("other"))
	require.NoError(t, store.Record(t.Context(), entry(tagged, old)))
	require.NoError(t, store.Record(t.Context(), entry("ghcr.io/org/other:tag", other)))

	require.NoError(t, store.Record(t.Context(), entry(tagged, moved)))

	idx, err := layout.ImageIndexFromPath(store.Layout())
	require.NoError(t, err)
	manifest, err := idx.IndexManifest()
	require.NoError(t, err)
	require.Len(t, manifest.Manifests, 2)
	require.Equal(t, other, manifest.Manifests[0].Digest)
	require.Equal(t, moved, manifest.Manifests[1].Digest)
	require.Equal(t, tagged, manifest.Manifests[1].Annotations[image.RefName])
}

func TestRecordRefusesAnEntryWithoutReference(t *testing.T) {
	t.Parallel()

	store, err := image.Open(t.TempDir())
	require.NoError(t, err)

	err = store.Record(t.Context(), v1.Descriptor{Digest: digestOf(content)})

	require.ErrorContains(t, err, "no reference")
}

func TestRecordKeepsTheEntriesOfConcurrentPulls(t *testing.T) {
	t.Parallel()

	store, err := image.Open(t.TempDir())
	require.NoError(t, err)
	const pulls = 8
	var wg sync.WaitGroup
	errs := make([]error, pulls)
	for i := range pulls {
		wg.Go(func() {
			ref := "ghcr.io/org/repo:v" + strconv.Itoa(i)
			errs[i] = store.Record(t.Context(), entry(ref, digestOf([]byte(ref))))
		})
	}
	wg.Wait()

	require.NoError(t, errors.Join(errs...))
	index, err := store.Index()
	require.NoError(t, err)
	require.Len(t, index.Manifests, pulls)
}

func TestRecordLeavesTheIndexIntactWhenAWriteWasKilled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := image.Open(root)
	require.NoError(t, err)
	h := digestOf(content)
	require.NoError(t, store.Record(t.Context(), entry(tagged, h)))
	// A pull died in the middle of writing the index: its half is in tmp/, the index is untouched.
	require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "index.json.42"), []byte(`{"schemaV`), 0o600))

	store, err = image.Open(root)

	require.NoError(t, err)
	index, err := store.Index()
	require.NoError(t, err)
	require.Len(t, index.Manifests, 1)
	require.Equal(t, h, index.Manifests[0].Digest)
	require.Empty(t, dataFiles(t, tmpOf(root)))
}

func TestDefaultRoot(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	root, err := image.DefaultRoot()

	require.NoError(t, err)
	require.Equal(t, filepath.Join(cache, "cove"), root)
}

func TestDefaultRootWithoutXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", home)

	root, err := image.DefaultRoot()

	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, ".cache", "cove"), root)
}
