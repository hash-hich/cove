package erofs

import (
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
	// redirectDirOn lets a directory that comes from a blob be renamed in the merged view: without
	// it the rename fails with EXDEV, which pip install --upgrade meets on a package of the image.
	// It is a mount option rather than a guest kernel default, so that a kernel of the user gets
	// it too. What it costs elsewhere, an upper layer no older kernel can mount, is nothing here:
	// the upper of a run is thrown away with it and never mounted anywhere else.
	redirectDirOn = "redirect_dir=on"
	// mountRoot is where the blobs are mounted in the guest, one directory per disk. The names
	// are short on purpose: the kernel takes a page of options for a mount and truncates what
	// does not fit, and fifty of these hold in a few hundred bytes.
	mountRoot = "/l"
)

// ceiling is how many disks a VM is given at most, which is a constant of the virtual machine
// monitor and of the architecture and not of the host: each virtio device takes a line of
// interrupt, and the attachment fails outright once they run out.
func ceiling() int {
	if runtime.GOARCH == "arm64" {
		return 115
	}
	return 10
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
	// DiffID names the layer the disk holds.
	DiffID string `json:"diffid"`
	// Path is the file to attach, Mountpoint where the guest mounts it.
	Path       string `json:"path"`
	Mountpoint string `json:"mountpoint"`

	BlobSHA256           string `json:"blob_sha256"`
	DiffIDVerified       bool   `json:"diffid_verified"`
	NormalizedEntries    int    `json:"normalized_entries"`
	UnknownXattrPrefixes int    `json:"unknown_xattr_prefixes"`
}

// Plan returns the mount plan of img, whose layers are the blobs layers in the order of the
// manifest, the bottom layer first. Each layer takes a disk of its own.
//
// It fails, and nothing is attached, when the image has more layers than the architecture can
// attach disks, the message naming both.
func (c *Cache) Plan(img Image, layers []Blob) (Plan, error) {
	return c.planUnder(img, layers, ceiling())
}

// planUnder is Plan with the ceiling given, so that an image over the ceiling is exercised on a
// host whose architecture would never reach it.
func (c *Cache) planUnder(img Image, layers []Blob, ceiling int) (Plan, error) {
	if len(layers) > ceiling {
		return Plan{}, fmt.Errorf("an image of %d layers does not fit on the %d disks of this architecture",
			len(layers), ceiling)
	}
	p := Plan{
		Image:           img.Ref,
		ImageDigest:     img.Digest,
		SourceDateEpoch: c.epoch,
		MountOptions:    []string{xinoOn, redirectDirOn},
		Layers:          make([]Mount, 0, len(layers)),
	}
	// The plan lists the highest layer first, the order overlayfs reads its lower directories
	// in, where the manifest lists the lowest layer first.
	for i := range layers {
		p.Layers = append(p.Layers, mountOf(layers[len(layers)-1-i], i))
	}
	return p, nil
}

// mountOf returns the entry of the plan for the blob b, which is the nth disk of the plan.
func mountOf(b Blob, n int) Mount {
	return Mount{
		DiffID:               b.DiffID,
		Path:                 b.Path,
		Mountpoint:           fmt.Sprintf("%s/%02d", mountRoot, n),
		BlobSHA256:           b.Meta.BlobSHA256,
		DiffIDVerified:       b.Meta.DiffIDVerified,
		NormalizedEntries:    b.Meta.NormalizedEntries,
		UnknownXattrPrefixes: b.Meta.UnknownXattrPrefixes,
	}
}
