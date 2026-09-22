// Package erofs turns the layers of an OCI image into the read only disks a sandbox mounts.
//
// One layer gives one blob, an EROFS image holding what that archive declared and nothing of the
// layers around it, filed under the diff id of the layer. The guest stacks the blobs in overlayfs
// in the order of the manifest, so whiteouts stay in the form overlayfs reads and nothing is
// flattened here. Three properties come out of that shape, in the order of their weight: a
// conversion depends on one archive only, so it runs while the other layers are still coming
// down; a base layer is converted once and shared by every image built on it, whatever the
// architecture of the host; and a blob is a function of its archive alone, so two hosts that
// convert it obtain the same bytes.
//
// What it costs is one disk per layer instead of one, so an image with more layers than the
// architecture can attach disks has no plan at all. A hard link whose target is in another layer
// cannot be expressed either, and is a named failure rather than a common case.
//
// What a layer holds is decided in internal/layer, which this package gives a writer to. This
// package writes the file, files it in the cache and says what it did in meta.json.
package erofs

// blockSize is the size of a block of the file systems written, fixed here and never taken from
// the page of the host, which is 16384 on Apple Silicon and would make a blob that depends on the
// machine that wrote it. The guest kernel runs in pages of 4 K on both architectures, so a block
// equals a page.
const blockSize = 4096

// cacheVersion is the directory the blobs of this writer live under, and what meta.json carries
// as writer_version. It stands for the three things that decide the bytes of a blob: the pinned
// commit of go-erofs, which go.mod holds, the parameters written into the images, and the code of
// this package. Any of the three moving makes a new directory: conversions start again under it,
// while the one before stays intact and is removed whole.
const cacheVersion = "v1"
