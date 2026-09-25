package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSize(t *testing.T) {
	t.Parallel()
	for v, want := range map[string]int64{"8g": 8 << 30, "64g": 64 << 30, "1t": 1 << 40} {
		got, err := parseSize(v)
		require.NoError(t, err)
		require.Equal(t, want, got, v)
	}
	for _, v := range []string{"", "g", "0g", "-1g", "8G", "8", "8m"} {
		_, err := parseSize(v)
		require.Error(t, err, v)
	}
	for _, v := range sizes {
		_, err := parseSize(v)
		require.NoError(t, err, v)
	}
}

// record is one record of what is kept, as records gives it.
type record struct {
	off  int64
	data []byte
}

// kept returns the records of what is kept of a file of size.
func kept(t *testing.T, b []byte, size int64) []record {
	t.Helper()
	var got []record
	require.NoError(t, records(bytes.NewReader(b), size, func(off int64, data []byte) error {
		got = append(got, record{off, bytes.Clone(data)})
		return nil
	}))
	return got
}

func TestKeepSkipsTheHolesAndTheBlocksOfZeros(t *testing.T) {
	t.Parallel()
	const size = 64 << 20
	path := filepath.Join(t.TempDir(), "disk")
	require.NoError(t, sparse(path, size))
	a := bytes.Repeat([]byte{1}, 2*blockSize)
	b := bytes.Repeat([]byte{2}, blockSize)
	f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // G304: a file the test wrote.
	require.NoError(t, err)
	for _, w := range []record{{0, a}, {2 * blockSize, make([]byte, blockSize)}, {3 * blockSize, b}, {32 << 20, b}} {
		_, err := f.WriteAt(w.data, w.off)
		require.NoError(t, err)
	}
	require.NoError(t, f.Close())

	first, err := keepFile(path)
	require.NoError(t, err)
	// The block of zeros written between a and b is data to the file system, and not kept.
	require.Equal(t, []record{{0, a}, {3 * blockSize, b}, {32 << 20, b}}, kept(t, first, size))

	back := filepath.Join(t.TempDir(), "back")
	require.NoError(t, writeBack(back, first, size))
	fi, err := os.Stat(back)
	require.NoError(t, err)
	require.Equal(t, int64(size), fi.Size())
	again, err := keepFile(back)
	require.NoError(t, err)
	require.Equal(t, first, again)
}

// build writes a file kept of size, holding recs, with the magic m.
func build(t *testing.T, m string, size int64, recs ...record) []byte {
	t.Helper()
	//nolint:gosec // G115: a size of the test.
	raw := append([]byte(m), binary.BigEndian.AppendUint64(nil, uint64(size))...)
	for _, r := range recs {
		raw = binary.BigEndian.AppendUint64(raw, uint64(r.off))       //nolint:gosec // G115: an offset of the test.
		raw = binary.BigEndian.AppendUint32(raw, uint32(len(r.data))) //nolint:gosec // G115: a block of the test.
		raw = append(raw, r.data...)
	}
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	_, err := zw.Write(raw)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return out.Bytes()
}

func TestRecordsRefusesWhatIsOutOfPlace(t *testing.T) {
	t.Parallel()
	const size = 1 << 20
	blk := bytes.Repeat([]byte{7}, blockSize)
	for name, b := range map[string][]byte{
		"another format":  build(t, "cove-rw0", size),
		"another size":    build(t, magic, 2*size),
		"overlap":         build(t, magic, size, record{blockSize, blk}, record{blockSize, blk}),
		"backwards":       build(t, magic, size, record{blockSize, blk}, record{0, blk}),
		"past the size":   build(t, magic, size, record{size - blockSize/2, blk}),
		"an empty record": build(t, magic, size, record{0, nil}),
	} {
		err := records(bytes.NewReader(b), size, func(int64, []byte) error { return nil })
		require.Error(t, err, name)
	}
}

// TestTheEmptyExt4AreTheOnesCoveEmbeds makes every size again, checks each with e2fsck once written
// back from what is kept, and compares the fingerprints with those cove embeds. It needs the
// e2fsprogs the Dockerfile pins, and says so elsewhere.
func TestTheEmptyExt4AreTheOnesCoveEmbeds(t *testing.T) {
	t.Parallel()
	if err := checkMke2fs(); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	require.NoError(t, makeAll(dir, t.TempDir()))
	want, err := os.ReadFile(filepath.Join(out, sums))
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(dir, sums)) //nolint:gosec // G304: a file the test wrote.
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))

	for _, name := range sizes {
		size, err := parseSize(name)
		require.NoError(t, err)
		b, err := os.ReadFile(filepath.Join(out, name+".gz")) //nolint:gosec // G304: a file cove embeds.
		require.NoError(t, err)
		var n int
		for _, r := range kept(t, b, size) {
			n += len(r.data)
		}
		require.LessOrEqual(t, n, maxKept, name)
		disk := filepath.Join(t.TempDir(), name+".ext4")
		require.NoError(t, writeBack(disk, b, size))
		//nolint:gosec // G204: a file of the test.
		fsck, err := exec.CommandContext(t.Context(), "e2fsck", "-fn", disk).CombinedOutput()
		require.NoError(t, err, "%s", fsck)
		require.NoError(t, os.Remove(disk))
	}
}
