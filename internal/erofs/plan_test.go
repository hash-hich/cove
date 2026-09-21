package erofs_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/erofs"
)

// anImage is what the plans of the tests are drawn for.
var anImage = erofs.Image{Ref: "ghcr.io/org/agent:2.1", Digest: "sha256:9f3c"}

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

	p, err := c.PlanWithCeiling(t.Context(), anImage, layers, 10)

	require.NoError(t, err)
	require.Equal(t, anImage.Ref, p.Image)
	require.Equal(t, anImage.Digest, p.ImageDigest)
	require.Equal(t, []string{"xino=on"}, p.MountOptions)
	require.Len(t, p.Layers, 3)
	// The highest layer comes first, the order overlayfs reads its lower directories in.
	for i, want := range []erofs.Blob{layers[2], layers[1], layers[0]} {
		require.Equal(t, want.DiffID, p.Layers[i].DiffID)
		require.Equal(t, want.Path, p.Layers[i].Path)
		require.Equal(t, want.Meta.BlobSHA256, p.Layers[i].BlobSHA256)
		require.True(t, p.Layers[i].DiffIDVerified)
		require.Empty(t, p.Layers[i].Members)
	}
	require.Equal(t, []string{"/l/00", "/l/01", "/l/02"}, mountpoints(p))
}

func TestComposesTheLayersAtTheBottomOverTheCeiling(t *testing.T) {
	t.Parallel()

	c := opened(t)
	layers := tower(t, c, 6)

	// Six layers on four disks: one group of the four at the bottom, then two layers on their own.
	p, err := c.PlanWithCeiling(t.Context(), anImage, layers, 4)

	require.NoError(t, err)
	require.Len(t, p.Layers, 3)
	require.Equal(t, layers[5].DiffID, p.Layers[0].DiffID)
	require.Equal(t, layers[4].DiffID, p.Layers[1].DiffID)
	group := p.Layers[2]
	require.Empty(t, group.DiffID, "a group is named by its members, not by a diff id")
	require.Equal(t, []string{
		layers[0].DiffID, layers[1].DiffID, layers[2].DiffID, layers[3].DiffID,
	}, group.Members, "the members are in the order of the manifest")
	require.Equal(t, []string{"/l/00", "/l/01", "/l/02"}, mountpoints(p))
	require.FileExists(t, group.Path)

	img := mounted(t, group.Path)
	require.Equal(t, []string{"layer00", "layer01", "layer02", "layer03"}, names(t, img, "/"))
}

func TestComposesAsFewGroupsAsMakeTheLayersFit(t *testing.T) {
	t.Parallel()

	c := opened(t)
	layers := tower(t, c, 9)

	// Nine layers on five disks: two groups of four, then one layer on its own.
	p, err := c.PlanWithCeiling(t.Context(), anImage, layers, 5)

	require.NoError(t, err)
	require.Len(t, p.Layers, 3)
	require.Equal(t, layers[8].DiffID, p.Layers[0].DiffID)
	require.Equal(t, layers[4].DiffID, p.Layers[1].Members[0], "the second group starts where the first stops")
	require.Equal(t, layers[0].DiffID, p.Layers[2].Members[0])
}

func TestAskedTwiceTheSameImageGivesTheSamePlan(t *testing.T) {
	t.Parallel()

	c := opened(t)
	layers := tower(t, c, 6)
	first, err := c.PlanWithCeiling(t.Context(), anImage, layers, 4)
	require.NoError(t, err)

	again, err := c.PlanWithCeiling(t.Context(), anImage, layers, 4)

	require.NoError(t, err)
	require.Equal(t, first, again, "the same image asks for the same groups, composed once")
}

func TestRefusesAnImageThatDoesNotFitOnTheDisksOfTheArchitecture(t *testing.T) {
	t.Parallel()

	c := opened(t)
	layers := tower(t, c, 7)

	// Two disks hold at most two groups, which is eight layers, and a group is never left short
	// of its four: seven layers have no layout.
	_, err := c.PlanWithCeiling(t.Context(), anImage, layers, 2)

	require.ErrorContains(t, err, "an image of 7 layers does not fit on the 2 disks")
	require.ErrorContains(t, err, "groups of 4")
}

// mountpoints returns where the plan mounts each of its disks.
func mountpoints(p erofs.Plan) []string {
	got := make([]string, 0, len(p.Layers))
	for _, m := range p.Layers {
		got = append(got, m.Mountpoint)
	}
	return got
}
