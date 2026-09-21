package disk

import (
	"fmt"
	"iter"
	"path"
	"strings"
)

// root is the path of the root of the image, and where everything that climbs too high lands.
const root = "/"

// rooted turns the name an archive gives an entry into the path it takes in the image, and says
// whether it had to be bounded to the root.
//
// Bounded, not refused: such an entry no longer aims at the host, it gains nothing the image
// does not already hold, and containerd and umoci bound too. That an image carries one stays
// worth knowing, hence the count and the line on Log.
func rooted(name string) (string, bool) {
	clean := name
	for {
		if after, found := strings.CutPrefix(clean, "/"); found {
			clean = after
			continue
		}
		if after, found := strings.CutPrefix(clean, "./"); found {
			clean = after
			continue
		}
		break
	}
	clean = path.Clean(clean)
	bounded := false
	for clean == ".." || strings.HasPrefix(clean, "../") {
		bounded = true
		clean = strings.TrimPrefix(strings.TrimPrefix(clean, ".."), "/")
		if clean == "" {
			clean = "."
		}
	}
	if clean == "." {
		return root, bounded
	}
	return root + clean, bounded
}

// entryPath is rooted on the name of an entry, counting and saying the bounding.
func (cv *conversion) entryPath(name string) string {
	p, bounded := rooted(name)
	if bounded {
		cv.counts.NormalizedEntries++
		cv.logf("%s: %s: bounded to %s", cv.Layer, name, p)
	}
	return p
}

// linkPath is rooted on the target of a hard link, which the archive gives from its own root
// like any other path, and which is counted and said like any other.
func (cv *conversion) linkPath(name, target string) string {
	p, bounded := rooted(target)
	if bounded {
		cv.counts.NormalizedEntries++
		cv.logf("%s: %s: hard link target %s: bounded to %s", cv.Layer, name, target, p)
	}
	return p
}

// place records that kind goes at p, once the image is able to hold it there. The last entry of
// a name wins, its kind with it.
func (cv *conversion) place(name, p string, kind Kind) error {
	if p == root && kind != Dir {
		return fmt.Errorf("layer %s: %s: %w: the root of the image is a directory, not a %s",
			cv.Layer, name, ErrImpossibleEntry, kind)
	}
	if err := cv.hangsFromDirs(name, p); err != nil {
		return err
	}
	cv.index[p] = kind
	return nil
}

// hangsFromDirs refuses a path whose parents are, in what the layer has written so far, anything
// but a directory. A parent nothing was written at is left alone: the archive is free to hold a
// child before its directory, and the writer makes the directory.
func (cv *conversion) hangsFromDirs(name, p string) error {
	for parent := range ancestors(p) {
		if at, written := cv.index[parent]; written && at != Dir {
			return fmt.Errorf("layer %s: %s: %w: its parent %s is a %s, not a directory",
				cv.Layer, name, ErrImpossibleEntry, parent, at)
		}
	}
	return nil
}

// ancestors yields the directories a path hangs from, closest to the root first. The root is not
// one of them: it is a directory by construction.
func ancestors(p string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for i := 1; i < len(p); i++ {
			if p[i] == '/' && !yield(p[:i]) {
				return
			}
		}
	}
}
