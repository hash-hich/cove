package erofs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gitlab.com/hich-hich/cove/internal/filelock"
)

// The directories and files of the cache under the root of the cache of cove:
//
//	rootfs/v1/<diff id>/                   one layer
//	    layer.erofs
//	    meta.json
//	rootfs/v1/merge/<hash of the members>/ one group
//	    group.erofs
//	    meta.json
//	rootfs/v1/tmp/<key>.<pid>/             a conversion in progress
//	rootfs/v1/tmp/<key>.lock               its lock
//	rootfs/v1/tmp/erofs-mkfs-*             the spool of a writer, unlinked as soon as it is made
//
// A directory of the cache exists whole or not at all: it is built under tmp/ and moved in by one
// rename. The version is a directory of its own so that a writer that would produce other bytes
// starts again beside the one before, which stays readable and is removed in one step.
const (
	rootfsDir = "rootfs"
	tmpDir    = "tmp"
	mergeDir  = "merge"
	blobFile  = "layer.erofs"
	groupFile = "group.erofs"
	spoolGlob = "erofs-mkfs-*"

	lockSuffix = ".lock"
	sha256Algo = "sha256"
)

// sourceDateEpoch is the variable of the reproducible builds convention: it dates everything the
// archive of a layer leaves undated, the superblock included.
const sourceDateEpoch = "SOURCE_DATE_EPOCH"

// Cache holds the blobs of the converted layers, one directory per layer, shared by every image
// that names it.
type Cache struct {
	root string
	tmp  string
	// epoch dates what the archive does not, and suffix marks the keys of the blobs written
	// with a value other than the default one, so that a conversion that runs without the
	// variable is never served a blob that was dated by it.
	epoch  int64
	suffix string
}

// Open prepares the cache under root, the root of the cache of cove, creating what is missing and
// sweeping the conversions no process holds any more. The value of SOURCE_DATE_EPOCH is read
// once, here: it dates the blobs this cache writes and marks their keys when it is not the
// default zero.
func Open(root string) (*Cache, error) {
	c := &Cache{root: filepath.Join(root, rootfsDir, cacheVersion)}
	c.tmp = filepath.Join(c.root, tmpDir)
	epoch, err := epochFromEnv()
	if err != nil {
		return nil, err
	}
	if epoch != 0 {
		c.epoch, c.suffix = epoch, "-sde"+strconv.FormatInt(epoch, 10)
	}
	for _, dir := range []string{c.tmp, filepath.Join(c.root, mergeDir)} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create the cache of the rootfs: %w", err)
		}
	}
	if err := c.sweep(); err != nil {
		return nil, fmt.Errorf("sweep the cache of the rootfs: %w", err)
	}
	return c, nil
}

// Epoch returns the date given to everything the archive of a layer leaves undated.
func (c *Cache) Epoch() int64 { return c.epoch }

// entry is where one blob of the cache lives: the directory it occupies, the file in it, and the
// stem the lock and the temporary directory of its conversion are named after.
type entry struct {
	dir  string
	file string
	stem string
}

// path returns the path of the blob itself.
func (e entry) path() string { return filepath.Join(e.dir, e.file) }

// layerEntry returns where the blob of the layer keyed by key lives. The key is the hex of a diff
// id, or of the digest of the compressed layer when no diff id is known of.
func (c *Cache) layerEntry(key string) entry {
	key += c.suffix
	return entry{dir: filepath.Join(c.root, key), file: blobFile, stem: key}
}

// Blob is a converted layer or group as it stands in the cache.
type Blob struct {
	// DiffID names the layer the blob holds, empty for a group, whose members Meta names
	// instead.
	DiffID string
	// Path is where the EROFS image is, what a VM attaches.
	Path string
	// Meta is what the conversion recorded next to it.
	Meta Meta
	// Converted says the call that returned it wrote it; false when the cache held it already.
	Converted bool
}

// build makes the blob of e by running write in a directory of tmp/, then moves that whole
// directory into the cache under one rename: no reader ever sees a directory that holds half of
// a conversion. A cache that already holds it is served as it stands and write is not called,
// even when the call had to wait for the conversion that put it there. Anything short of the
// rename takes the temporary away, so a diff id that does not match, an entry that cannot be
// written or a kill -9 leaves nothing behind that a later run would trust.
func (c *Cache) build(ctx context.Context, e entry, onWait func(), write func(dir string) (Meta, error),
) (Blob, error) {
	if b, ok, err := lookup(e); ok || err != nil {
		return b, err
	}
	l, err := filelock.Take(ctx, c.lockPath(e.stem), onWait)
	if err != nil {
		return Blob{}, fmt.Errorf("wait for the conversion of %s: %w", e.stem, err)
	}
	defer l.Release()
	if b, ok, err := lookup(e); ok || err != nil {
		return b, err
	}
	tmp := c.tmpPath(e.stem)
	kept := false
	defer func() {
		if !kept {
			_ = os.RemoveAll(tmp)
		}
	}()
	meta, err := c.run(tmp, write)
	if err != nil {
		return Blob{}, err
	}
	if err := os.Rename(tmp, e.dir); err != nil {
		return Blob{}, fmt.Errorf("file the conversion: %w", err)
	}
	kept = true
	return Blob{Path: e.path(), Meta: meta, Converted: true}, nil
}

// run empties the temporary directory tmp, runs write in it and writes there what the conversion
// recorded. The caller holds the lock of the key and takes tmp away if this fails.
func (c *Cache) run(tmp string, write func(dir string) (Meta, error)) (Meta, error) {
	if err := os.RemoveAll(tmp); err != nil {
		return Meta{}, fmt.Errorf("clear the conversion: %w", err)
	}
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return Meta{}, fmt.Errorf("start the conversion: %w", err)
	}
	meta, err := write(tmp)
	if err != nil {
		return Meta{}, err
	}
	meta.WriterVersion = cacheVersion
	meta.SourceDateEpoch = c.epoch
	if err := writeMeta(tmp, meta); err != nil {
		return Meta{}, err
	}
	return meta, nil
}

// lookup returns the blob of e when the cache holds it. A directory is there or it is not: what
// it holds was moved in whole.
func lookup(e entry) (Blob, bool, error) {
	if _, err := os.Stat(e.path()); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Blob{}, false, nil
		}
		return Blob{}, false, fmt.Errorf("look for %s: %w", e.path(), err)
	}
	meta, err := readMeta(e.dir)
	if err != nil {
		return Blob{}, false, err
	}
	return Blob{Path: e.path(), Meta: meta}, true, nil
}

// sweep removes from tmp/ what no process holds: the directory a conversion that was killed left
// behind, which has no value since a conversion starts again from the beginning, and a spool of
// the writer that outlived its process, which happens where a file open cannot be unlinked.
// Whether a conversion is on a directory is told by the lock of its key, not by the pid in its
// name, which another program may have been given since. Lock files are never removed; see
// filelock.Lock.Release.
func (c *Cache) sweep() error {
	entries, err := os.ReadDir(c.tmp)
	if err != nil {
		return fmt.Errorf("list the conversions: %w", err)
	}
	for _, e := range entries {
		if err := c.sweepOne(e.Name()); err != nil {
			return err
		}
	}
	return nil
}

// sweepOne removes the entry name of tmp/ when nothing holds it.
func (c *Cache) sweepOne(name string) error {
	if strings.HasSuffix(name, lockSuffix) {
		return nil
	}
	stem, _, found := strings.Cut(name, ".")
	if !found {
		// A spool is the only thing here that carries no key, so no lock says whether it is
		// in use; a process that holds one has unlinked it already, and what is still named
		// was left behind by one that is gone.
		if ok, _ := filepath.Match(spoolGlob, name); !ok {
			return nil
		}
		return c.remove(name)
	}
	l, ok, err := filelock.Try(c.lockPath(stem))
	if err != nil {
		return fmt.Errorf("look at the conversion of %s: %w", stem, err)
	}
	if !ok {
		return nil
	}
	defer l.Release()
	return c.remove(name)
}

// remove takes the entry name of tmp/ away.
func (c *Cache) remove(name string) error {
	if err := os.RemoveAll(filepath.Join(c.tmp, name)); err != nil {
		return fmt.Errorf("remove a stale conversion: %w", err)
	}
	return nil
}

// tmpPath returns where the conversion of stem is built. The pid says who writes, nothing more.
func (c *Cache) tmpPath(stem string) string {
	return filepath.Join(c.tmp, stem+"."+strconv.Itoa(os.Getpid()))
}

// lockPath returns the lock of stem, the same name for every process.
func (c *Cache) lockPath(stem string) string {
	return filepath.Join(c.tmp, stem+lockSuffix)
}

// epochFromEnv reads SOURCE_DATE_EPOCH, zero when it is unset or empty. A value that is not a
// number is refused rather than ignored: a conversion that silently dated the blobs otherwise
// would give fingerprints nothing explains.
func epochFromEnv() (int64, error) {
	raw := os.Getenv(sourceDateEpoch)
	if raw == "" {
		return 0, nil
	}
	epoch, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || epoch < 0 {
		return 0, fmt.Errorf("%s=%q is not a number of seconds since the epoch", sourceDateEpoch, raw)
	}
	return epoch, nil
}

// hexOf returns the hex of the digest h, written algorithm:hex, and refuses any algorithm other
// than the sha256 of the OCI image specification, which the rest of the cache assumes.
func hexOf(h string) (string, error) {
	algo, hex, found := strings.Cut(h, ":")
	if !found || algo != sha256Algo || hex == "" {
		return "", fmt.Errorf("%q is not a %s digest", h, sha256Algo)
	}
	return hex, nil
}
