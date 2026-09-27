package rwdisk_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/rwdisk"
)

const gib = int64(1) << 30

func TestParseSizeReadsWhatStringWrites(t *testing.T) {
	t.Parallel()
	for _, s := range rwdisk.Sizes {
		got, err := rwdisk.ParseSize(s.String())
		require.NoError(t, err)
		require.Equal(t, s, got)
	}
	got, err := rwdisk.ParseSize("64G")
	require.NoError(t, err)
	require.Equal(t, rwdisk.Size(64*gib), got)
	require.Equal(t, "1t", rwdisk.Size(1024*gib).String())
}

func TestParseSizeNamesTheSizesItTakes(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"10g", "64", "64gb", "0g", "2t", ""} {
		_, err := rwdisk.ParseSize(v)
		require.ErrorContains(t, err, "8g, 16g, 32g, 64g, 128g, 256g, 512g, 1t", v)
	}
}

func TestNominal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		capacity rwdisk.Size
		free     int64
		want     rwdisk.Size
	}{
		{name: "the capacity decides on a large host", capacity: rwdisk.DefaultCap, free: 1024 * gib, want: 64 << 30},
		{name: "the free space lowers it", capacity: rwdisk.DefaultCap, free: 25 * gib, want: 16 << 30},
		{name: "the margin is left to the host", capacity: rwdisk.DefaultCap, free: 36 * gib, want: 32 << 30},
		{name: "a fit just past the margin", capacity: rwdisk.DefaultCap, free: 12 * gib, want: 8 << 30},
		{name: "a small capacity on a large host", capacity: 8 << 30, free: 1024 * gib, want: 8 << 30},
		{name: "the largest", capacity: 1 << 40, free: 2048 * gib, want: 1 << 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := rwdisk.Nominal(tt.capacity, tt.free, rwdisk.DefaultMargin)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNominalRefusesAHostWithoutRoom(t *testing.T) {
	t.Parallel()
	_, err := rwdisk.Nominal(rwdisk.DefaultCap, 11*gib, rwdisk.DefaultMargin)
	require.EqualError(t, err, "the host has 11.0g free and keeps 4.0g of it, short of the 8g of the smallest write disk")
}

func TestFreeReadsTheFileSystemOfTheDirectory(t *testing.T) {
	t.Parallel()
	free, err := rwdisk.Free(t.TempDir())
	require.NoError(t, err)
	require.Positive(t, free)
	_, err = rwdisk.Free(t.TempDir() + "/missing")
	require.Error(t, err)
}

func TestEverySizeHasAnEmptyExt4(t *testing.T) {
	t.Parallel()
	for _, s := range rwdisk.Sizes {
		require.NoError(t, rwdisk.Create(filepath.Join(t.TempDir(), "rw.ext4"), s), s.String())
	}
}
