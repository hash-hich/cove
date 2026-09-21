package erofs

import (
	"context"
	"fmt"
	"runtime"
)

// The options the mount of the image cannot do without, and the shape of the mount points.
const (
	// xinoOn keeps the inode numbers of two blobs apart in the merged view. The guest kernel is
	// built without CONFIG_OVERLAY_FS_XINO_AUTO, so without this option two files coming from
	// two blobs can be given one st_ino, and anything that tells files apart by inode number
	// would take them for one.
	xinoOn = "xino=on"
	// mountRoot is where the blobs are mounted in the guest, one directory per disk. The names
	// are short on purpose: the kernel takes a page of options for a mount and truncates what
	// does not fit, and fifty of these hold in a few hundred bytes.
	mountRoot = "/l"
)

// ceiling is how many disks a VM is given at most, which is a constant of the virtual machine
// monitor and of the architecture and not of the host: each virtio device takes a line of
// interrupt, and the attachment fails outright once they run out. It does not enter the key of a
// group, so a group composed on one architecture serves the other.
func ceiling() int {
	if runtime.GOARCH == "arm64" {
		return 115
	}
	return 10
}

// sliceSize is how many consecutive layers a group holds. A blob is never a member of two
// different groups, so the number is a parameter of the writer: changing it makes the groups
// already composed unusable, which is why it is part of what cacheVersion stands for.
const sliceSize = 4

// span is one disk of a VM and the layers it holds, counted from the bottom of the manifest: one
// layer for a blob, sliceSize of them for a group.
type span struct {
	start int
	count int
}

// layout returns how many disks the layers of an image take and which layers each one holds,
// bottom first. Under the ceiling every layer keeps a disk of its own. Over it, the layers at the
// bottom are composed into groups of sliceSize consecutive layers, as few as make the rest fit,
// and the layers at the top stay on their own.
//
// The slices are taken from the bottom and are of a fixed size because that is what keeps them
// shared: the group of the first sliceSize layers is the same for every image that starts with
// those layers, whatever its height. Composing the deepest layers of each image instead, in a
// number its own height decides, would give two images that share a base two groups nothing can
// match.
func layout(layers, ceiling int) ([]span, error) {
	groups := 0
	if layers > ceiling {
		// Each group takes sliceSize layers and gives back one disk, so it saves sliceSize-1.
		groups = (layers - ceiling + sliceSize - 2) / (sliceSize - 1)
	}
	if groups*sliceSize > layers {
		return nil, fmt.Errorf("an image of %d layers does not fit on the %d disks of this architecture, "+
			"even composed in groups of %d", layers, ceiling, sliceSize)
	}
	spans := make([]span, 0, groups+layers-groups*sliceSize)
	for i := range groups {
		spans = append(spans, span{start: i * sliceSize, count: sliceSize})
	}
	for i := groups * sliceSize; i < layers; i++ {
		spans = append(spans, span{start: i, count: 1})
	}
	return spans, nil
}

// Image is what a plan was made for: the reference the user asked for and the manifest that
// answered. The two are kept apart from the fingerprints of the blobs, and one is never reported
// for the other.
type Image struct {
	// Ref is the reference asked for.
	Ref string
	// Digest names the manifest of the platform, never an index.
	Digest string
}

// Plan is what the cache hands the verb that attaches an image: which blobs to give the VM, in
// which order, where each one is mounted, and the options the mount must carry. The verb adds
// what it has verified itself, entry by entry, without changing the shape.
//
// The keys are snake case, the shape the verbs of cove report in.
//
//nolint:tagliatelle // The keys are the contract of the plan, written down before the linter's rule.
type Plan struct {
	Image           string `json:"image"`
	ImageDigest     string `json:"image_digest"`
	SourceDateEpoch int64  `json:"source_date_epoch"`
	// MountOptions are the options the overlay must be mounted with.
	MountOptions []string `json:"mount_options"`
	// Layers are the disks to attach, the highest first, which is the order overlayfs takes its
	// lower directories in.
	Layers []Mount `json:"layers"`
}

// Mount is one disk of the plan and what the cache knows of it. It says what was done and what
// was verified, never more: BlobSHA256 is the fingerprint of the conversion, not of the file as
// it stands now, so a blob changed since then still carries the one of its conversion. The name
// of its directory ties it to a diff id and nothing else does; checking the file again is for
// whoever attaches it.
//
//nolint:tagliatelle // The keys are the contract of the plan, written down before the linter's rule.
type Mount struct {
	// DiffID names the layer of a blob, Members the layers of a group in the order of the
	// manifest. An entry carries one or the other.
	DiffID  string   `json:"diffid,omitempty"`
	Members []string `json:"members,omitempty"`
	// Path is the file to attach, Mountpoint where the guest mounts it.
	Path       string `json:"path"`
	Mountpoint string `json:"mountpoint"`

	BlobSHA256           string `json:"blob_sha256"`
	DiffIDVerified       bool   `json:"diffid_verified"`
	NormalizedEntries    int    `json:"normalized_entries"`
	UnknownXattrPrefixes int    `json:"unknown_xattr_prefixes"`
}

// Plan returns the mount plan of img, whose layers are the blobs layers in the order of the
// manifest, the bottom layer first. Every layer keeps a disk of its own while the architecture
// can attach that many; over that, the layers at the bottom are composed into groups, which are
// written to the cache here if it does not hold them yet. onWait, when not nil, is called once if
// another process is composing a group this one needs.
//
// It fails, and no disk is attached, when the layers do not fit on the disks of the architecture
// even composed in groups.
func (c *Cache) Plan(ctx context.Context, img Image, layers []Blob, onWait func()) (Plan, error) {
	return c.planUnder(ctx, img, layers, ceiling(), onWait)
}

// planUnder is Plan with the ceiling given, so that the composition of groups is exercised on a
// host whose architecture would never reach it.
func (c *Cache) planUnder(ctx context.Context, img Image, layers []Blob, ceiling int, onWait func(),
) (Plan, error) {
	spans, err := layout(len(layers), ceiling)
	if err != nil {
		return Plan{}, err
	}
	disks := make([]Blob, 0, len(spans))
	for _, s := range spans {
		if s.count == 1 {
			disks = append(disks, layers[s.start])
			continue
		}
		group, err := c.Group(ctx, layers[s.start:s.start+s.count], onWait)
		if err != nil {
			return Plan{}, err
		}
		disks = append(disks, group)
	}
	p := Plan{
		Image:           img.Ref,
		ImageDigest:     img.Digest,
		SourceDateEpoch: c.epoch,
		MountOptions:    []string{xinoOn},
		Layers:          make([]Mount, 0, len(disks)),
	}
	// The plan lists the highest disk first, the order overlayfs reads its lower directories in,
	// where the manifest lists the lowest layer first.
	for i := range disks {
		p.Layers = append(p.Layers, mountOf(disks[len(disks)-1-i], i))
	}
	return p, nil
}

// mountOf returns the entry of the plan for the blob b, which is the nth disk of the plan.
func mountOf(b Blob, n int) Mount {
	return Mount{
		DiffID:               b.DiffID,
		Members:              b.Meta.Members,
		Path:                 b.Path,
		Mountpoint:           fmt.Sprintf("%s/%02d", mountRoot, n),
		BlobSHA256:           b.Meta.BlobSHA256,
		DiffIDVerified:       b.Meta.DiffIDVerified,
		NormalizedEntries:    b.Meta.NormalizedEntries,
		UnknownXattrPrefixes: b.Meta.UnknownXattrPrefixes,
	}
}
