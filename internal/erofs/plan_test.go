package erofs_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/erofs"
)

// anImage is what the plans of the tests are drawn for.
var anImage = erofs.Image{Ref: "ghcr.io/org/agent:2.1", Digest: "sha256:9f3c"}

// stacked converts each of layers, bottom first as a manifest lists them, and returns the blobs.
func stacked(t *testing.T, c *erofs.Cache, layers ...[]entry) []erofs.Blob {
	t.Helper()
	blobs := make([]erofs.Blob, 0, len(layers))
	for _, entries := range layers {
		blobs = append(blobs, converted(t, c, layerOf(t, entries...), nil))
	}
	return blobs
}

// tower converts n layers, each holding a file of its own, bottom first.
func tower(t *testing.T, c *erofs.Cache, n int) []erofs.Blob {
	t.Helper()
	layers := make([][]entry, 0, n)
	for i := range n {
		layers = append(layers, []entry{file(fmt.Sprintf("layer%02d", i), "x")})
	}
	return stacked(t, c, layers...)
}

func TestLaysOutOneDiskPerLayerUnderTheCeiling(t *testing.T) {
	t.Parallel()

	c := opened(t)
	layers := tower(t, c, 3)

	p, err := c.PlanWithCeiling(anImage, layers, 10)

	require.NoError(t, err)
	require.Equal(t, anImage.Ref, p.Image)
	require.Equal(t, anImage.Digest, p.ImageDigest)
	// redirect_dir=on lets a directory of a lower layer be renamed, which pip install --upgrade
	// does to a package of the image: without it the rename fails with EXDEV.
	require.Equal(t, []string{"xino=on", "redirect_dir=on"}, p.MountOptions)
	require.Len(t, p.Layers, 3)
	// The highest layer comes first, the order overlayfs reads its lower directories in.
	for i, want := range []erofs.Blob{layers[2], layers[1], layers[0]} {
		require.Equal(t, want.DiffID, p.Layers[i].DiffID)
		require.Equal(t, want.Path, p.Layers[i].Path)
		require.Equal(t, want.Meta.BlobSHA256, p.Layers[i].BlobSHA256)
		require.True(t, p.Layers[i].DiffIDVerified)
	}
	require.Equal(t, []string{"/l/00", "/l/01", "/l/02"}, mountpoints(p))
}

func TestRefusesAnImageThatDoesNotFitOnTheDisksOfTheArchitecture(t *testing.T) {
	t.Parallel()

	c := opened(t)
	layers := tower(t, c, 3)

	_, err := c.PlanWithCeiling(anImage, layers, 2)

	require.ErrorContains(t, err, "an image of 3 layers does not fit on the 2 disks")
}

// mountpoints returns where the plan mounts each of its disks.
func mountpoints(p erofs.Plan) []string {
	got := make([]string, 0, len(p.Layers))
	for _, m := range p.Layers {
		got = append(got, m.Mountpoint)
	}
	return got
}
