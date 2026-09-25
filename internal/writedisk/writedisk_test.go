package writedisk_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/emptyext4"
	"gitlab.com/hich-hich/cove/internal/writedisk"
)

const gib = int64(1) << 30

func TestParseSizeReadsWhatStringWrites(t *testing.T) {
	t.Parallel()
	for _, s := range writedisk.Sizes {
		got, err := writedisk.ParseSize(s.String())
		require.NoError(t, err)
		require.Equal(t, s, got)
	}
	got, err := writedisk.ParseSize("64G")
	require.NoError(t, err)
	require.Equal(t, writedisk.Size(64*gib), got)
	require.Equal(t, "1t", writedisk.Size(1024*gib).String())
}

func TestParseSizeNamesTheSizesItTakes(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"10g", "64", "64gb", "0g", "2t", ""} {
		_, err := writedisk.ParseSize(v)
		require.ErrorContains(t, err, "8g, 16g, 32g, 64g, 128g, 256g, 512g, 1t", v)
	}
}

func TestNominal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		capacity writedisk.Size
		free     int64
		want     writedisk.Size
	}{
		{name: "the capacity decides on a large host", capacity: writedisk.DefaultCap, free: 1024 * gib, want: 64 << 30},
		{name: "the free space lowers it", capacity: writedisk.DefaultCap, free: 25 * gib, want: 16 << 30},
		{name: "the margin is left to the host", capacity: writedisk.DefaultCap, free: 36 * gib, want: 32 << 30},
		{name: "a fit just past the margin", capacity: writedisk.DefaultCap, free: 12 * gib, want: 8 << 30},
		{name: "a small capacity on a large host", capacity: 8 << 30, free: 1024 * gib, want: 8 << 30},
		{name: "the largest", capacity: 1 << 40, free: 2048 * gib, want: 1 << 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := writedisk.Nominal(tt.capacity, tt.free, writedisk.DefaultMargin)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNominalRefusesAHostWithoutRoom(t *testing.T) {
	t.Parallel()
	_, err := writedisk.Nominal(writedisk.DefaultCap, 11*gib, writedisk.DefaultMargin)
	require.EqualError(t, err, "the host has 11.0g free and keeps 4.0g of it, short of the 8g of the smallest write disk")
}

func TestFreeReadsTheFileSystemOfTheDirectory(t *testing.T) {
	t.Parallel()
	free, err := writedisk.Free(t.TempDir())
	require.NoError(t, err)
	require.Positive(t, free)
	_, err = writedisk.Free(t.TempDir() + "/missing")
	require.Error(t, err)
}

func TestEverySizeHasAnEmptyExt4(t *testing.T) {
	t.Parallel()
	for _, s := range writedisk.Sizes {
		require.NoError(t, emptyext4.Write(filepath.Join(t.TempDir(), "rw.ext4"), int64(s)), s.String())
	}
}
