package disk

import (
	"archive/tar"
	"fmt"
	"slices"
	"strings"
)

// schilyXattr heads the PAX record an archive carries an extended attribute in.
const schilyXattr = "SCHILY.xattr."

// The prefixes EROFS reads. An attribute under any other one is written into the image all the
// same, and the guest will not see it.
var erofsPrefixes = []string{"user.", "trusted.", "security.", "system.posix_acl_access", "system.posix_acl_default"}

// xattrs sets on p every extended attribute the header carries, with the raw bytes the archive
// gave, NUL and bytes that are not UTF-8 included. The names go in order, so that two
// conversions of the same layer call the writer the same way.
func (cv *conversion) xattrs(hdr *tar.Header, p string) error {
	names := make([]string, 0, len(hdr.PAXRecords))
	for record := range hdr.PAXRecords {
		if name, found := strings.CutPrefix(record, schilyXattr); found && name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		if !readInGuest(name) {
			cv.counts.UnknownXattrPrefixes++
			cv.logf("%s: %s: extended attribute %s: EROFS reads no such prefix, it is written "+
				"but the guest will not see it", cv.Layer, hdr.Name, name)
		}
		if err := cv.out.Setxattr(p, name, []byte(hdr.PAXRecords[schilyXattr+name])); err != nil {
			return fmt.Errorf("layer %s: %s: set the extended attribute %s: %w", cv.Layer, hdr.Name, name, err)
		}
	}
	return nil
}

// readInGuest says whether EROFS gives an attribute of that name back to the guest.
func readInGuest(name string) bool {
	return slices.ContainsFunc(erofsPrefixes, func(prefix string) bool {
		return strings.HasPrefix(name, prefix)
	})
}
