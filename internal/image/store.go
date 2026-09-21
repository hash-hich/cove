package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// ErrCorrupt reports a blob whose bytes do not hash to the digest that names it.
var ErrCorrupt = errors.New("the content does not match its digest")

// RefName is the annotation of an index entry that carries the reference the user wrote, next to
// the blobs filed by hash: the name of the OCI image layout specification.
const RefName = "org.opencontainers.image.ref.name"

// The directories of the store under its root, and the files of the layout.
const (
	layoutDir  = "images"
	tmpDir     = "tmp"
	blobsDir   = "blobs"
	indexFile  = "index.json"
	layoutFile = "oci-layout"
	lockSuffix = ".lock"
	sha256Algo = "sha256"
)

// ociLayout is the content of the oci-layout file: the version of the format.
const ociLayout = "{\n    \"imageLayoutVersion\": \"1.0.0\"\n}\n"

// Store is where pulled images live. images/ is an OCI layout, the directory format skopeo, crane
// and podman read, and holds nothing the format does not name; tmp/ next to it holds the
// downloads in progress and the locks, on the same file system so that a rename into the layout
// is atomic.
type Store struct {
	root string
}

// DefaultRoot is where the store lives unless told otherwise: cove under $XDG_CACHE_HOME, or under
// ~/.cache when it is not set.
func DefaultRoot() (string, error) {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "cove"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate the cache directory: %w", err)
	}
	return filepath.Join(home, ".cache", "cove"), nil
}

// Open prepares the store under root, creating what is missing, and sweeps tmp/ of the data files
// nobody holds. A directory that cannot be written is named in the error.
func Open(root string) (*Store, error) {
	s := &Store{root: root}
	for _, dir := range []string{s.inLayout(blobsDir, sha256Algo), s.inTmp()} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create the store: %w", err)
		}
	}
	if err := s.initLayout(); err != nil {
		return nil, fmt.Errorf("initialize the store: %w", err)
	}
	if err := s.sweep(); err != nil {
		return nil, fmt.Errorf("sweep the store: %w", err)
	}
	return s, nil
}

// Layout returns the path of the OCI layout: what to give skopeo or crane.
func (s *Store) Layout() string {
	return s.inLayout()
}

// Has reports whether the blob named h is in the store. What is there was verified on the way in.
func (s *Store) Has(h v1.Hash) (bool, error) {
	_, err := os.Stat(s.blobPath(h))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look for %s: %w", h, err)
	}
	return true, nil
}

// Blob opens the blob named h, which the caller closes. What the store holds was verified against
// its digest on the way in and a blob is never written again, so what comes out is what its name
// says.
func (s *Store) Blob(h v1.Hash) (io.ReadCloser, error) {
	f, err := os.Open(s.blobPath(h))
	if err != nil {
		return nil, fmt.Errorf("open the blob %s: %w", h, err)
	}
	return f, nil
}

// Progress is what Put says on the way. OnWait is called once when another pull holds the blob,
// OnRead for each chunk received. Either may be nil.
type Progress struct {
	OnWait func()
	OnRead func(n int64)
}

// Put brings the blob named h into the store from the bytes open returns, unless it is there
// already, and reports whether it downloaded it. The bytes go to tmp/ and are hashed on the way;
// they are renamed into the layout only when the hash is the one of h, so that blobs/ never holds
// a file whose content is not what its name says. Nothing of a failed download stays, and a
// mismatch is ErrCorrupt.
//
// The lock of h is held from before open until the rename, so that two pulls wanting the same
// blob at the same time download it once: the second waits for the first, then finds the blob
// there. A lock per image would let a base layer shared by two images come down twice, and one
// lock for the store would make the second pull wait to the end of the first.
func (s *Store) Put(ctx context.Context, h v1.Hash, open func() (io.ReadCloser, error), p Progress) (bool, error) {
	if h.Algorithm != sha256Algo {
		return false, fmt.Errorf("blob %s: only sha256 digests are supported", h)
	}
	if ok, err := s.Has(h); err != nil || ok {
		return false, err
	}
	l, err := lock(ctx, s.lockPath(h.Hex), p.OnWait)
	if err != nil {
		return false, fmt.Errorf("blob %s: %w", h, err)
	}
	defer l.unlock()
	// Whoever waited was waiting for a download of this very blob, which may be there now.
	if ok, err := s.Has(h); err != nil || ok {
		return false, err
	}
	if err := s.download(h, open, p); err != nil {
		return false, fmt.Errorf("blob %s: %w", h, err)
	}
	return true, nil
}

// download brings the blob named h into the layout: see Put. The caller holds the lock of h.
func (s *Store) download(h v1.Hash, open func() (io.ReadCloser, error), p Progress) error {
	rc, err := open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	tmp := s.partPath(h.Hex)
	//nolint:gosec // G304: tmp is built from the root of the store and a hex digest, never from user input.
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create the download: %w", err)
	}
	// Anything short of the rename removes the file: a download is never resumed, so a truncated
	// blob has no value, and one that carries a valid name would poison every run to come.
	kept := false
	defer func() {
		if !kept {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, hasher, counter(p.OnRead)), rc); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != h.Hex {
		return fmt.Errorf("%w (the content hashes to %s:%s)", ErrCorrupt, sha256Algo, got)
	}
	// The bytes reach the disk before the name does: a power cut after the rename must not leave
	// an empty file under a valid name, which some file systems would do without this.
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync the download: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close the download: %w", err)
	}
	if err := os.Rename(tmp, s.blobPath(h)); err != nil {
		return fmt.Errorf("file the blob: %w", err)
	}
	kept = true
	return nil
}

// Record puts desc in index.json in the place of any entry carrying the same reference, the
// RefName annotation desc must carry: a tag that moved keeps one entry, the new one, since two
// entries under one reference would no longer say which image it designates. The update is
// serialized with the other pulls by its own lock, and the index is read once the lock is held,
// so that two pulls ending together keep each other's entry.
func (s *Store) Record(ctx context.Context, desc v1.Descriptor) error {
	ref := desc.Annotations[RefName]
	if ref == "" {
		return errors.New("record the image: the descriptor carries no reference")
	}
	l, err := lock(ctx, s.lockPath(indexFile), nil)
	if err != nil {
		return fmt.Errorf("record the image: %w", err)
	}
	defer l.unlock()
	index, err := s.readIndex()
	if err != nil {
		return fmt.Errorf("record the image: %w", err)
	}
	index.Manifests = slices.DeleteFunc(index.Manifests, func(d v1.Descriptor) bool {
		return d.Annotations[RefName] == ref
	})
	index.Manifests = append(index.Manifests, desc)
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("record the image: %w", err)
	}
	if err := s.writeFile(indexFile, append(data, '\n')); err != nil {
		return fmt.Errorf("record the image: %w", err)
	}
	return nil
}

// Index returns the index of the layout: the images of the store, one entry per reference.
func (s *Store) Index() (*v1.IndexManifest, error) {
	return s.readIndex()
}

// readIndex parses index.json.
func (s *Store) readIndex() (*v1.IndexManifest, error) {
	data, err := os.ReadFile(s.inLayout(indexFile))
	if err != nil {
		return nil, fmt.Errorf("read the index: %w", err)
	}
	var index v1.IndexManifest
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("read the index: %w", err)
	}
	return &index, nil
}

// initLayout writes the files the format wants at the root of the layout when they are not there
// yet: oci-layout, and an index with no image. It takes the lock of the index so that a pull
// recording an image does not race a store being opened.
func (s *Store) initLayout() error {
	l, err := lock(context.Background(), s.lockPath(indexFile), nil)
	if err != nil {
		return err
	}
	defer l.unlock()
	empty := v1.IndexManifest{SchemaVersion: 2, MediaType: types.OCIImageIndex, Manifests: []v1.Descriptor{}}
	data, err := json.MarshalIndent(empty, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the empty index: %w", err)
	}
	for name, content := range map[string][]byte{layoutFile: []byte(ociLayout), indexFile: append(data, '\n')} {
		if _, err := os.Stat(s.inLayout(name)); err == nil {
			continue
		}
		if err := s.writeFile(name, content); err != nil {
			return err
		}
	}
	return nil
}

// writeFile puts data at name in the layout through tmp/, an fsync and a rename: an interruption
// leaves the previous file intact, never a half-written one. index.json is worth that much: it
// is read for every image of the store, so truncated it would break the reading of all of them.
// The caller holds the lock of name.
func (s *Store) writeFile(name string, data []byte) error {
	tmp := s.partPath(name)
	//nolint:gosec // G304: tmp is built from the root of the store and a file name of the format, never from user input.
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err == nil {
		err = f.Close()
	}
	if err == nil {
		err = os.Rename(tmp, s.inLayout(name))
	}
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// sweep removes from tmp/ the data files nobody holds: what a kill -9 or a power cut left behind,
// which has no value since a download is never resumed. Whether a pull is on a file is told by
// the lock of its blob, not by the pid in its name, which another program may have been given
// since. Lock files are never removed: a lock removed between the look of a sweep and the flock
// of a pull would let a third pull recreate the name and lock another inode, and the two would
// download the same layer at once.
func (s *Store) sweep() error {
	entries, err := os.ReadDir(s.inTmp())
	if err != nil {
		return fmt.Errorf("list the downloads: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		dot := strings.LastIndexByte(name, '.')
		if e.IsDir() || strings.HasSuffix(name, lockSuffix) || dot < 0 {
			continue
		}
		l, ok, err := tryLock(s.lockPath(name[:dot]))
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		err = os.Remove(s.inTmp(name))
		l.unlock()
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove a stale download: %w", err)
		}
	}
	return nil
}

// inLayout returns the path of elem under the OCI layout.
func (s *Store) inLayout(elem ...string) string {
	return filepath.Join(append([]string{s.root, layoutDir}, elem...)...)
}

// inTmp returns the path of elem under the directory of the downloads in progress.
func (s *Store) inTmp(elem ...string) string {
	return filepath.Join(append([]string{s.root, tmpDir}, elem...)...)
}

// blobPath returns where the blob named h lives in the layout.
func (s *Store) blobPath(h v1.Hash) string {
	return s.inLayout(blobsDir, h.Algorithm, h.Hex)
}

// partPath returns where stem is written while in progress. The pid says who writes, nothing more.
func (s *Store) partPath(stem string) string {
	return s.inTmp(stem + "." + strconv.Itoa(os.Getpid()))
}

// lockPath returns the lock of stem, the same name for every process.
func (s *Store) lockPath(stem string) string {
	return s.inTmp(stem + lockSuffix)
}

// counter is a writer that tells f how many bytes went through, for the progress of a download.
type counter func(n int64)

func (c counter) Write(p []byte) (int, error) {
	if c != nil {
		c(int64(len(p)))
	}
	return len(p), nil
}
