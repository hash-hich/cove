package unpack

import (
	"archive/tar"
	"maps"
	"slices"
	"strings"
)

// xattrRecord heads the PAX records that carry the extended attributes of an entry. The mapping
// to one Setxattr of the same name is the one mkfs.erofs --tar makes, measured against it.
const xattrRecord = "SCHILY.xattr."

// xattrs sets on the entry at p each extended attribute of hdr, by name, with the value as the
// archive holds it, NUL and non UTF-8 bytes included, in the order of the names so that a blob
// does not depend on the order
// of a map. An attribute under a name EROFS does not read is written all the same, counted and
// said.
func (c *unpacking) xattrs(hdr *tar.Header, p string) error {
	for _, key := range slices.Sorted(maps.Keys(hdr.PAXRecords)) {
		name, ok := strings.CutPrefix(key, xattrRecord)
		if !ok {
			continue
		}
		if !readable(name) {
			c.counts.UnknownXattrPrefixes++
			c.logf("entry %q: attribute %q is written but will not be readable in the guest: EROFS reads "+
				"user., trusted., security. and the two POSIX ACL attributes only", hdr.Name, name)
		}
		if err := c.w.Setxattr(p, name, hdr.PAXRecords[key]); err != nil {
			return c.refuse(hdr, err)
		}
	}
	return nil
}

// readable reports whether EROFS reads the extended attribute name in the guest: the prefixes
// user., trusted. and security., and the two POSIX ACL attributes. An attribute under any other
// name is written to the blob all the same: the blob says what the archive said.
func readable(name string) bool {
	for _, prefix := range []string{"user.", "trusted.", "security."} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return name == "system.posix_acl_access" || name == "system.posix_acl_default"
}
