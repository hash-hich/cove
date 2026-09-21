package erofs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	goerofs "github.com/erofs/go-erofs"
)

// The whiteouts overlayfs reads, which a blob carries and a group has to act on rather than
// stack: the conversion of a layer put them there, and the rule that says what they mean is
// stated with it, in internal/layer.
const (
	opaqueXattr = "trusted.overlay.opaque"
	opaqueValue = "y"
)

// groupEntry returns where the group keyed by key lives. Groups sit under a directory of their
// own so that whoever prunes the cache tells a blob shared between images from one composed out
// of blobs it already holds.
func (c *Cache) groupEntry(key string) entry {
	key += c.suffix
	return entry{dir: filepath.Join(c.root, mergeDir, key), file: groupFile, stem: mergeDir + "-" + key}
}

// Group composes the blobs of members, in the order of the manifest and bottom first, into one
// blob the VM attaches in their place, and files it in the cache unless the cache holds it
// already. onWait, when not nil, is called once if another process is composing the same group.
//
// What the composition does is what overlayfs would do of those layers, once and ahead of time:
// each member is copied over what is under it, and the whiteouts it carries take away what the
// members below put there. The markers themselves are kept, so that a group hides from the groups
// under it exactly what its members hid from the layers under them.
func (c *Cache) Group(ctx context.Context, members []Blob, onWait func()) (Blob, error) {
	if len(members) < 2 {
		return Blob{}, fmt.Errorf("a group composes at least two blobs, not %d", len(members))
	}
	ids := make([]string, 0, len(members))
	for _, m := range members {
		if m.DiffID == "" {
			return Blob{}, fmt.Errorf("the blob %s is not a layer: a group composes layers", m.Path)
		}
		ids = append(ids, m.DiffID)
	}
	return c.build(ctx, c.groupEntry(groupKey(ids)), onWait, func(dir string) (Meta, error) {
		return c.compose(members, ids, dir)
	})
}

// groupKey returns the key of the group of ids: the hash of the ordered list of its members, so
// that two images sharing those layers in that order share the group composed out of them.
func groupKey(ids []string) string {
	sum := sha256.New()
	for _, id := range ids {
		_, _ = fmt.Fprintln(sum, id)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// compose writes the group of members in dir. The caller holds the lock of the group and takes
// dir away if this fails.
func (c *Cache) compose(members []Blob, ids []string, dir string) (Meta, error) {
	//nolint:gosec // G304: dir is built by the cache from its own root, never from user input.
	out, err := os.OpenFile(filepath.Join(dir, groupFile), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return Meta{}, fmt.Errorf("create the group: %w", err)
	}
	defer func() { _ = out.Close() }()
	// The writer reads the content of a file when the image is laid out and not when it is
	// copied, so every member stays open until then.
	open := make([]*os.File, 0, len(members))
	defer func() {
		for _, f := range open {
			_ = f.Close()
		}
	}()
	w := c.newWriter(out)
	meta := Meta{ConvertedAt: time.Now().UTC(), Members: ids, DiffIDVerified: true}
	for _, m := range members {
		f, err := os.Open(m.Path)
		if err != nil {
			return Meta{}, fmt.Errorf("read the blob %s: %w", m.DiffID, err)
		}
		open = append(open, f)
		if err := stack(w, f, m.DiffID); err != nil {
			return Meta{}, err
		}
		meta.Entries += m.Meta.Entries
		meta.NormalizedEntries += m.Meta.NormalizedEntries
		meta.UnknownXattrPrefixes += m.Meta.UnknownXattrPrefixes
		meta.DiffIDVerified = meta.DiffIDVerified && m.Meta.DiffIDVerified
	}
	if err := w.Close(); err != nil {
		return Meta{}, fmt.Errorf("write the group: %w", err)
	}
	fingerprint, err := seal(out)
	if err != nil {
		return Meta{}, err
	}
	meta.BlobSHA256 = fingerprint
	return meta, nil
}

// stack copies the blob of the layer id, which f serves, over what w already holds, the whiteouts
// it carries applied first.
func stack(w *goerofs.Writer, f *os.File, id string) error {
	src, err := goerofs.Open(f)
	if err != nil {
		return fmt.Errorf("read the blob %s: %w", id, err)
	}
	if err := hide(w, src); err != nil {
		return fmt.Errorf("blob %s: %w", id, err)
	}
	// Merge() of the library is not what a group needs: it reads the whiteouts of an archive,
	// in the form the archive writes them and not the one a blob carries, and drops the markers
	// it acts on, which a group lower in the stack still has to hide behind.
	if err := w.CopyFrom(src); err != nil {
		return fmt.Errorf("copy the blob %s: %w", id, err)
	}
	return nil
}

// hide applies to w the whiteouts src carries: a character device 0:0 takes away what stands at
// its name, and an opaque directory takes away everything that stood under it. Both are applied
// before src is copied, so they reach what the members below left and nothing of src itself.
func hide(w *goerofs.Writer, src fs.FS) error {
	//nolint:wrapcheck // The callers name the blob the walk failed on.
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		sys, ok := info.Sys().(*goerofs.Stat)
		name := "/" + p
		if p == "." {
			name = "/"
		}
		switch {
		case !ok:
			return nil
		case info.Mode()&fs.ModeCharDevice != 0 && sys.Rdev == 0:
			return remove(w, name)
		case info.IsDir() && sys.Xattrs[opaqueXattr] == opaqueValue:
			return empty(w, name)
		}
		return nil
	})
}

// fsName returns the absolute name name as an fs.FS reads it: no leading slash, and a dot for
// the root.
func fsName(name string) string {
	if name == "/" {
		return "."
	}
	return strings.TrimPrefix(name, "/")
}

// remove takes the entry name away from w, whether it is there or not.
func remove(w *goerofs.Writer, name string) error {
	if err := w.RemoveAll(name); err != nil {
		return fmt.Errorf("hide %s: %w", name, err)
	}
	return nil
}

// empty takes away what the members below put under the directory name, the directory itself
// staying: an opaque directory hides what is under it, not itself.
func empty(w *goerofs.Writer, name string) error {
	entries, err := fs.ReadDir(w, fsName(name))
	if err != nil {
		// Nothing below put a directory there, so there is nothing to hide.
		return nil //nolint:nilerr // The absence of the directory is the answer, not a failure.
	}
	for _, e := range entries {
		if err := remove(w, path.Join(name, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
