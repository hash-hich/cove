package erofs_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/erofs"
)

// stacked converts each of layers, bottom first as a manifest lists them, and returns the blobs.
func stacked(t *testing.T, c *erofs.Cache, layers ...[]entry) []erofs.Blob {
	t.Helper()
	blobs := make([]erofs.Blob, 0, len(layers))
	for _, entries := range layers {
		blobs = append(blobs, converted(t, c, layerOf(t, entries...), nil))
	}
	return blobs
}

// grouped composes the blobs and fails the test if it does not go through.
func grouped(t *testing.T, c *erofs.Cache, members []erofs.Blob) erofs.Blob {
	t.Helper()
	g, err := c.Group(t.Context(), members, nil)
	require.NoError(t, err)
	return g
}

func TestAGroupHidesWhatItsUpperMemberTookAway(t *testing.T) {
	t.Parallel()

	c := opened(t)
	members := stacked(t,
		c,
		[]entry{dir("etc"), file("etc/passwd", "root"), file("etc/hosts", "localhost")},
		[]entry{dir("etc"), whiteout("etc/.wh.hosts"), file("etc/resolv.conf", "nameserver")},
	)

	g := grouped(t, c, members)

	img := mounted(t, g.Path)
	require.Equal(t, []string{"hosts", "passwd", "resolv.conf"}, names(t, img, "/etc"))
	require.Equal(t, fs.ModeDevice|fs.ModeCharDevice, stat(t, img, "/etc/hosts").Mode&fs.ModeType,
		"the marker stays, so the groups below stay hidden too")
	require.Equal(t, uint32(0), stat(t, img, "/etc/hosts").Rdev)
}

func TestAGroupEmptiesADirectoryItsUpperMemberMadeOpaque(t *testing.T) {
	t.Parallel()

	c := opened(t)
	members := stacked(t,
		c,
		[]entry{dir("opt"), file("opt/old", "one"), dir("opt/sub"), file("opt/sub/deep", "two")},
		[]entry{dir("opt"), whiteout("opt/.wh..wh..opq"), file("opt/new", "three")},
	)

	g := grouped(t, c, members)

	img := mounted(t, g.Path)
	require.Equal(t, []string{"new"}, names(t, img, "/opt"), "nothing of the member below is left")
	require.Equal(t, "y", stat(t, img, "/opt").Xattrs["trusted.overlay.opaque"],
		"the marker stays, so the groups below stay hidden too")
}

func TestAGroupTakesTheUpperMemberOfTwoNamesAndItsContent(t *testing.T) {
	t.Parallel()

	c := opened(t)
	members := stacked(t, c,
		[]entry{file("a", "below"), file("kept", "one")},
		[]entry{file("a", "above")},
	)

	g := grouped(t, c, members)

	content, err := fs.ReadFile(mounted(t, g.Path), "a")
	require.NoError(t, err)
	require.Equal(t, "above", string(content))
	require.Equal(t, []string{"a", "kept"}, names(t, mounted(t, g.Path), "/"))
}

func TestGivesTheSameGroupTwice(t *testing.T) {
	t.Parallel()

	layers := [][]entry{{file("a", "one")}, {file("b", "two")}, {whiteout(".wh.a")}}

	first := grouped(t, opened(t), stacked(t, opened(t), layers...))
	other := opened(t)
	again := grouped(t, other, stacked(t, other, layers...))

	require.Equal(t, first.Meta.BlobSHA256, again.Meta.BlobSHA256)
	require.Equal(t, read(t, first.Path), read(t, again.Path))
}

func TestTwoImagesShareTheGroupOfTheLayersTheyShare(t *testing.T) {
	t.Parallel()

	c := opened(t)
	base := stacked(t, c, []entry{file("a", "one")}, []entry{file("b", "two")})
	first := grouped(t, c, base)

	// The second image is built on the same two layers: the group is the one already composed.
	again := grouped(t, c, base)

	require.True(t, first.Converted)
	require.False(t, again.Converted, "the group of two shared layers is composed once")
	require.Equal(t, first.Path, again.Path)
	require.Equal(t, base[0].DiffID, again.Meta.Members[0])
	require.Equal(t, base[1].DiffID, again.Meta.Members[1])
}

func TestRefusesAGroupOfFewerThanTwoBlobs(t *testing.T) {
	t.Parallel()

	c := opened(t)

	_, err := c.Group(t.Context(), stacked(t, c, []entry{file("a", "one")}), nil)

	require.ErrorContains(t, err, "a group composes at least two blobs")
}
