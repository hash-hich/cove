package layer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/layer"
)

func TestTurnsWhiteoutsIntoWhatOverlayfsReads(t *testing.T) {
	t.Parallel()

	rec, counts, log := applyAll(t,
		dir("d"), whiteout("d/.wh.x"), whiteout("d/.wh..wh..opq"),
		whiteout("e/.wh..wh..opq"), dir("e"),
		whiteout(".wh..wh..opq"),
	)

	require.Equal(t, []string{
		"mkdir /d drwxr-xr-x 0:0", "mknod /d/x Dc--------- 0:0 0,0", `setxattr /d trusted.overlay.opaque="y"`,
		`setxattr /e trusted.overlay.opaque="y"`, "setattr /e drwxr-xr-x 0:0",
		`setxattr / trusted.overlay.opaque="y"`,
	}, rec.calls)
	require.Equal(t, layer.Counts{}, counts)
	require.Empty(t, log)
}

func TestIgnoresTheOtherReservedWhiteouts(t *testing.T) {
	t.Parallel()

	rec, counts, log := applyAll(t, whiteout("d/.wh..wh.plnk"), whiteout(".wh..wh.aufs"), whiteout(".wh..wh..opq.x"))

	require.Empty(t, rec.calls)
	require.Equal(t, layer.Counts{}, counts)
	require.Empty(t, log)
}

func TestNormalizesTheNameUnderAWhiteoutPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		want  string
		count int
	}{
		{name: "../.wh.x", want: "/x", count: 1},
		{name: "a/../.wh.x", want: "/x"},
		{name: "/d/.wh.x", want: "/d/x"},
		{name: "d/.wh..", want: "/d"},
		{name: "d/.wh.", want: "/d"},
		{name: "d/e/.wh...", want: "/d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec, counts, _ := applyAll(t, whiteout(tt.name))

			require.Equal(t, []string{"mknod " + tt.want + " Dc--------- 0:0 0,0"}, rec.calls)
			require.Equal(t, layer.Counts{NormalizedEntries: tt.count}, counts)
		})
	}
}
