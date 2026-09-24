// Package imageuser resolves the User of an image configuration into the identity a process of
// that image runs under, as runc resolves it for docker: images are tested against runc, so its
// rules are copied rather than improved. Names are looked up in the /etc/passwd and /etc/group of
// the image, never in those of the host or of the initramfs.
package imageuser

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// defaultHome is the home of a user the image does not list, as runc gives it.
const defaultHome = "/"

// Identity is who a process runs as.
type Identity struct {
	UID, GID uint32
	// Groups are the supplementary groups, empty rather than nil when there are none.
	Groups []uint32
	// Home is the home directory of the user, / for a uid /etc/passwd does not list.
	Home string
}

// ResolveIn resolves user in the image whose root is root. A file the image lacks counts as a file
// with no entry, as runc counts it: a numeric user still resolves, a name does not.
func ResolveIn(root, user string) (Identity, error) {
	passwd, err := openIfExists(filepath.Join(root, "etc/passwd"))
	if err != nil {
		return Identity{}, err
	}
	defer func() { _ = passwd.Close() }()
	group, err := openIfExists(filepath.Join(root, "etc/group"))
	if err != nil {
		return Identity{}, err
	}
	defer func() { _ = group.Close() }()
	return Resolve(user, passwd, group)
}

// Resolve resolves user, in one of the six forms of the User of an image, user, uid, user:group,
// uid:gid, uid:group and user:gid, against the contents of /etc/passwd and /etc/group. An empty
// user is root.
//
// A name must be listed; a number need not be, and a uid /etc/passwd does not list gets gid 0 and
// home /. When the group is given, it is the only group: the supplementary groups are those of
// /etc/group that list the user only when user names no group, as libcontainer has it.
func Resolve(user string, passwd, group io.Reader) (Identity, error) {
	userArg, groupArg, _ := strings.Cut(user, ":")
	id, matched, err := resolveUser(userArg, passwd)
	if err != nil {
		return Identity{}, err
	}
	if groupArg == "" && matched == "" {
		return id, nil
	}
	return resolveGroups(id, user, matched, groupArg, group)
}

// resolveUser returns the identity userArg names before its groups, and the name /etc/passwd
// lists it under, empty when it lists none.
func resolveUser(userArg string, passwd io.Reader) (Identity, string, error) {
	uidArg, uidErr := parseID(userArg)
	users, err := readPasswd(passwd, func(u passwdEntry) bool {
		switch {
		case userArg == "":
			return u.uid == 0
		case uidErr == nil:
			return u.uid == uidArg
		default:
			return u.name == userArg
		}
	})
	if err != nil {
		return Identity{}, "", err
	}
	id := Identity{Home: defaultHome, Groups: []uint32{}}
	switch {
	case len(users) > 0:
		id.UID, id.GID, id.Home = users[0].uid, users[0].gid, users[0].home
		return id, users[0].name, nil
	case uidErr == nil:
		id.UID = uidArg
	case userArg != "":
		return Identity{}, "", fmt.Errorf("image sets User %s, not found in the image's /etc/passwd", userArg)
	}
	return id, "", nil
}

// resolveGroups gives id the group groupArg names, or the supplementary groups of the user
// /etc/passwd lists as matched when groupArg is empty.
func resolveGroups(id Identity, user, matched, groupArg string, group io.Reader) (Identity, error) {
	gidArg, gidErr := parseID(groupArg)
	groups, err := readGroup(group, func(g groupEntry) bool {
		switch {
		case groupArg == "":
			return slices.Contains(g.members, matched)
		case gidErr == nil:
			return g.gid == gidArg
		default:
			return g.name == groupArg
		}
	})
	if err != nil {
		return Identity{}, err
	}
	switch {
	case groupArg == "":
		for _, g := range groups {
			id.Groups = append(id.Groups, g.gid)
		}
	case len(groups) > 0:
		id.GID = groups[0].gid
	case gidErr == nil:
		id.GID = gidArg
	default:
		return Identity{}, fmt.Errorf("image sets User %s, group %s not found in the image's /etc/group",
			user, groupArg)
	}
	return id, nil
}

// parseID reads a uid or a gid, which the kernel takes up to 2^31-1.
func parseID(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err //nolint:wrapcheck // Only whether it failed is read.
	}
	if n > math.MaxInt32 {
		return 0, fmt.Errorf("id %d out of range", n)
	}
	return uint32(n), nil
}

type passwdEntry struct {
	name     string
	uid, gid uint32
	home     string
}

type groupEntry struct {
	name    string
	gid     uint32
	members []string
}

// readPasswd returns the entries of r that keep holds. A field that is missing or not a number is
// read as empty or zero, as libcontainer reads it.
func readPasswd(r io.Reader, keep func(passwdEntry) bool) ([]passwdEntry, error) {
	var out []passwdEntry
	err := eachLine(r, func(f []string) {
		e := passwdEntry{name: field(f, 0), uid: number(field(f, 2)), gid: number(field(f, 3)), home: field(f, 5)}
		if keep(e) {
			out = append(out, e)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("read the image's /etc/passwd: %w", err)
	}
	return out, nil
}

// readGroup returns the entries of r that keep holds.
func readGroup(r io.Reader, keep func(groupEntry) bool) ([]groupEntry, error) {
	var out []groupEntry
	err := eachLine(r, func(f []string) {
		e := groupEntry{name: field(f, 0), gid: number(field(f, 2))}
		if m := field(f, 3); m != "" {
			e.members = strings.Split(m, ",")
		}
		if keep(e) {
			out = append(out, e)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("read the image's /etc/group: %w", err)
	}
	return out, nil
}

// eachLine calls fn with the fields of each line of r that is neither blank nor a comment.
func eachLine(r io.Reader, fn func([]string)) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fn(strings.Split(line, ":"))
	}
	return sc.Err() //nolint:wrapcheck // The callers name the file.
}

func field(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

func number(s string) uint32 {
	n, err := parseID(s)
	if err != nil {
		return 0
	}
	return n
}

// openIfExists opens path, or returns an empty reader when there is no such file.
func openIfExists(path string) (io.ReadCloser, error) {
	//nolint:gosec // G304: path is /etc/passwd or /etc/group under the root of the image.
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return io.NopCloser(strings.NewReader("")), nil
	}
	if err != nil {
		return nil, fmt.Errorf("open the image's /etc/%s: %w", filepath.Base(path), err)
	}
	return f, nil
}
