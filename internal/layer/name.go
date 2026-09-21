package layer

import (
	"archive/tar"
	"path"
	"strings"
)

// normalize returns raw, a name of the archive, as the absolute clean path it is written at, and
// says so when it climbed above the root, counting the bounding. No option of cove turns the
// bounding off: an entry that climbs is written, said and counted, never refused. subject says
// what raw is
// to the entry of hdr: its name, its hard link target, or the target of its whiteout.
func (c *applying) normalize(hdr *tar.Header, subject, raw string) string {
	p, bounded := normalize(raw)
	if bounded {
		c.counts.NormalizedEntries++
		c.logf("entry %q: %s %q climbs above the root, bounded to %s", hdr.Name, subject, raw, p)
	}
	return p
}

// normalize returns name as an absolute clean path under the root, and whether it climbed above
// the root and was bounded to it: a leading ./ or / is dropped, then path.Clean resolves .
// and .., and what would go above the root stays at the root. An entry bounded there aims at
// nothing on the host any more and gains nothing the image does not already hold, which is why it
// is bounded rather than refused, as containerd and umoci do.
func normalize(name string) (string, bool) {
	rel := path.Clean(strings.TrimLeft(name, "/"))
	bounded := rel == ".." || strings.HasPrefix(rel, "../")
	return path.Clean("/" + rel), bounded
}
