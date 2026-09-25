// Package writedisk decides the size of the disk a run writes on: the upper of the image root and
// the volumes, one ext4 file system of a size fixed when the sandbox is created. The disk itself
// is written by internal/emptyext4, which holds an empty ext4 of each of Sizes.
//
// The file of the disk is sparse, so the host pays for what the run writes, never for the size it
// was allowed.
package writedisk

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Size is the nominal size of a write disk in bytes, one of Sizes.
type Size int64

// The units of a size, in powers of two as docker run -m counts them.
const (
	gib Size = 1 << 30
	tib Size = 1 << 40
)

// Sizes are the nominal sizes a write disk can take, one per empty ext4, the smallest first.
var Sizes = []Size{8 * gib, 16 * gib, 32 * gib, 64 * gib, 128 * gib, 256 * gib, 512 * gib, tib}

// DefaultCap is the size a run is given when it does not ask for one.
const DefaultCap = 64 * gib

// DefaultMargin is the free space cove leaves to the host whatever the runs ask for.
const DefaultMargin = int64(4 * gib)

// String returns the size as ParseSize reads it, 64g or 1t.
func (s Size) String() string {
	if s >= tib && s%tib == 0 {
		return strconv.FormatInt(int64(s/tib), 10) + "t"
	}
	return strconv.FormatInt(int64(s/gib), 10) + "g"
}

// ParseSize reads one of the sizes of Sizes, written as String writes it, in either case. Any
// other value is refused with the list of the sizes, since only those have an empty ext4.
func ParseSize(v string) (Size, error) {
	for _, s := range Sizes {
		if strings.EqualFold(v, s.String()) {
			return s, nil
		}
	}
	names := make([]string, len(Sizes))
	for i, s := range Sizes {
		names[i] = s.String()
	}
	return 0, fmt.Errorf("the write disk takes one of %s, not %q", strings.Join(names, ", "), v)
}

// Nominal returns the size of the disk of a new sandbox: the largest of Sizes that is at most
// capacity and fits in free, the free space of the host, once margin is left to the host. The
// capacity decides and the free space only lowers it, so a host with a terabyte free still gives
// capacity. It fails when not even the smallest size fits, naming the free space and the margin.
//
// Several sandboxes share one free space, each with its own nominal size: the disks are sparse,
// so the host is overcommitted on purpose, and the margin is what is left when that comes due.
func Nominal(capacity Size, free, margin int64) (Size, error) {
	var got Size
	for _, s := range Sizes {
		if s <= capacity && int64(s) <= free-margin {
			got = s
		}
	}
	if got == 0 {
		return 0, fmt.Errorf("the host has %s free and keeps %s of it, short of the %s of the smallest write disk",
			gibs(free), gibs(margin), Sizes[0])
	}
	return got, nil
}

// Free returns the space of the file system that holds dir that an unprivileged process can
// still write.
func Free(dir string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("read the free space of %s: %w", dir, err)
	}
	return times(st.Bavail, st.Bsize), nil
}

// times returns blocks blocks of size bytes. The size of a block is an int64 on Linux and a uint32
// on macOS, which one conversion could not take on both without a linter refusing it on one.
func times[B int64 | uint32](blocks uint64, size B) int64 {
	return int64(blocks) * int64(size) //nolint:gosec // G115: a free space far from the top of an int64.
}

// gibs writes n bytes in gibibytes, one decimal, as a person reads a free space.
func gibs(n int64) string {
	return strconv.FormatFloat(float64(n)/float64(gib), 'f', 1, 64) + "g"
}
