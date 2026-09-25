// Package boot is the init of cove, the first process of the VM and the only part of cove
// inside it. It makes the VM habitable before the first turn: it checks the kernel, stacks the
// layers of the image under the write disk of the run, mounts what every program expects, gives
// the VM its name and its resolvers, prepares the directory of the project, and hands the image
// root to the launcher, in the mount namespace of the agent.
//
// It stays in the initramfs, outside the root of the image, and remains PID 1: the agent is a
// child it collects, never the init itself. Nothing in the VM is protected from the agent, the
// init included, so the init is judged on what it makes possible and never on what it prevents.
package boot

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gitlab.com/hich-hich/cove/internal/vminit/spec"
)

// Where the init mounts what the image root is made of, in the initramfs.
const (
	// writeMount holds the write disk: the upper and the work directory of the overlay, and one
	// directory per volume.
	writeMount = "/w"
	upperDir   = writeMount + "/upper"
	workDir    = writeMount + "/work"
	volumesDir = writeMount + "/volumes"
)

// defaultWorkingDir is the working directory of an image that declares none, or declares /.
const defaultWorkingDir = "/work"

// workingDir returns the working directory of the image, the parent of the directory of the
// project. The image keeps what it holds there: a WORKDIR /home/agent leaves the dotfiles of the
// home in place, and the project lands next to them.
func workingDir(s *spec.Run) string {
	if s.WorkingDir == "" || path.Clean(s.WorkingDir) == "/" {
		return defaultWorkingDir
	}
	return path.Clean(s.WorkingDir)
}

// overlayOptions returns the options of the mount of the image root: the layers, the highest
// first, over the upper of the run.
func overlayOptions(s *spec.Run) string {
	lower := make([]string, 0, len(s.Layers))
	for _, l := range s.Layers {
		lower = append(lower, l.Mountpoint)
	}
	opts := make([]string, 0, 3+len(s.MountOptions))
	opts = append(opts, "lowerdir="+strings.Join(lower, ":"), "upperdir="+upperDir, "workdir="+workDir)
	return strings.Join(append(opts, s.MountOptions...), ",")
}

// volumePaths returns the volumes of the image, cleaned, without duplicates, a parent before what
// it holds so that a volume inside another is mounted on top of it, as docker orders them.
func volumePaths(vols []string) ([]string, error) {
	out := make([]string, 0, len(vols))
	for _, v := range vols {
		if !path.IsAbs(v) {
			return nil, fmt.Errorf("image sets Volume %s, not an absolute path", v)
		}
		c := path.Clean(v)
		if c == "/" {
			return nil, errors.New("image sets Volume /, which would hide the image")
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		if d := strings.Count(a, "/") - strings.Count(b, "/"); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return out, nil
}

// hostsFile returns the /etc/hosts of the VM: the loopback under localhost and under the name of
// the VM, since nothing else in the VM has an address a name could point to.
func hostsFile(hostname string) []byte {
	return []byte("127.0.0.1\tlocalhost\n" +
		"::1\tlocalhost ip6-localhost ip6-loopback\n" +
		"127.0.1.1\t" + hostname + "\n")
}

// resolvConf returns the /etc/resolv.conf of the VM, naming the resolvers of the run.
func resolvConf(nameservers []string) []byte {
	var b strings.Builder
	for _, ns := range nameservers {
		_, _ = b.WriteString("nameserver " + ns + "\n")
	}
	return []byte(b.String())
}

// maxLinks is how many symbolic links resolveIn follows before giving up, the limit of Linux.
const maxLinks = 40

// resolveIn returns the path under root that p, a path of the image rooted at root, names once
// every symbolic link on the way is followed inside root: an absolute target starts again from
// root, and .. never climbs above it. The last component is followed too. A component that does
// not exist ends the resolution, the rest joined as it is.
//
// The init sees the image from outside, so a link of the image to /etc would otherwise lead it to
// the /etc of the initramfs.
func resolveIn(root, p string) (string, error) {
	rest := strings.Split(strings.TrimPrefix(path.Clean("/"+p), "/"), "/")
	cur := "/"
	for links := 0; len(rest) > 0; {
		c := rest[0]
		rest = rest[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			cur = path.Dir(cur)
			continue
		}
		next := path.Join(cur, c)
		fi, err := os.Lstat(filepath.Join(root, next))
		if errors.Is(err, os.ErrNotExist) {
			return filepath.Join(root, path.Join(append([]string{next}, rest...)...)), nil
		}
		if err != nil {
			return "", fmt.Errorf("resolve %s in the image: %w", p, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			cur = next
			continue
		}
		if links++; links > maxLinks {
			return "", fmt.Errorf("resolve %s in the image: too many levels of symbolic links", p)
		}
		target, err := os.Readlink(filepath.Join(root, next))
		if err != nil {
			return "", fmt.Errorf("resolve %s in the image: %w", p, err)
		}
		if path.IsAbs(target) {
			cur = "/"
		}
		rest = append(strings.Split(target, "/"), rest...)
	}
	return filepath.Join(root, cur), nil
}

// diskOrder sorts the names the kernel gives the virtio disks into their order of attachment:
// vda to vdz, then vdaa, as the kernel names them one after the other.
func diskOrder(names []string) {
	slices.SortFunc(names, func(a, b string) int {
		if d := len(a) - len(b); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
}

// matchDisks checks the disks the VM was given, named in their order of attachment with their
// sizes in bytes, against the description: the layers, then the write disk. Nothing names a disk
// on every backend, so a disk out of place is caught by its size, and the refusal names the layer
// it was taken for.
func matchDisks(s *spec.Run, names []string, sizes []int64) error {
	if len(names) != len(s.Layers)+1 {
		return fmt.Errorf("the VM has %d disks, the run describes %d, %d layers and the write disk",
			len(names), len(s.Layers)+1, len(s.Layers))
	}
	for i, l := range s.Layers {
		if sizes[i] != l.Size {
			return fmt.Errorf("disk %s holds %d bytes, layer %s is %d", names[i], sizes[i], l.DiffID, l.Size)
		}
	}
	if last := len(names) - 1; sizes[last] != s.Write.Size {
		return fmt.Errorf("disk %s holds %d bytes, the write disk is %d", names[last], sizes[last], s.Write.Size)
	}
	return nil
}

// Where the ext4 superblock and its fields lie, the superblock at superblockOffset on the disk and
// the fields from its start.
const (
	superblockOffset = 1024
	superblockSize   = 1024
	ext4MagicOffset  = 0x38
	ext4LabelOffset  = 0x78
	ext4LabelSize    = 16
	ext4Magic        = 0xef53
)

// checkWriteDisk checks sb, the superblock read on the disk named disk, against the empty ext4
// every write disk starts from: an ext4 labelled spec.WriteLabel. The disks are found by their order and
// sizes only, so the label is what tells the write disk from a layer of the same size, before the
// init writes on it.
func checkWriteDisk(disk string, sb []byte) error {
	if len(sb) < superblockSize || binary.LittleEndian.Uint16(sb[ext4MagicOffset:]) != ext4Magic {
		return fmt.Errorf("disk %s holds no ext4, it is not the write disk", disk)
	}
	label := sb[ext4LabelOffset : ext4LabelOffset+ext4LabelSize]
	if i := bytes.IndexByte(label, 0); i >= 0 {
		label = label[:i]
	}
	if string(label) != spec.WriteLabel {
		return fmt.Errorf("disk %s is labelled %q, not %q: it is not the write disk", disk, label, spec.WriteLabel)
	}
	return nil
}
