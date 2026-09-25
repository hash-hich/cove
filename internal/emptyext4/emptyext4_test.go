package emptyext4_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/emptyext4"
	"gitlab.com/hich-hich/cove/internal/vminit/spec"
)

// sizes returns the sizes of the empty ext4 SHA256SUMS lists, by name, and checks that every file
// embedded has the fingerprint it lists.
func sizes(t *testing.T) map[string]int64 {
	t.Helper()
	sums, err := fs.ReadFile(emptyext4.Files(), "SHA256SUMS")
	require.NoError(t, err)
	got := map[string]int64{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		sum, file, ok := strings.Cut(sc.Text(), "  ")
		require.True(t, ok, sc.Text())
		b, err := fs.ReadFile(emptyext4.Files(), file)
		require.NoError(t, err)
		h := sha256.Sum256(b)
		require.Equal(t, sum, hex.EncodeToString(h[:]), file)
		name := strings.TrimSuffix(file, ".gz")
		n, err := strconv.ParseInt(name[:len(name)-1], 10, 64)
		require.NoError(t, err)
		got[name] = n << map[byte]int{'g': 30, 't': 40}[name[len(name)-1]]
	}
	embedded, err := fs.Glob(emptyext4.Files(), "*.gz")
	require.NoError(t, err)
	require.Len(t, got, len(embedded), "every file embedded is in SHA256SUMS")
	return got
}

// record is one record of an empty ext4, as decode gives it.
type record struct {
	off  int64
	data []byte
}

func records(t *testing.T, name string, size int64) []record {
	t.Helper()
	f, err := emptyext4.Files().Open(name + ".gz")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var got []record
	_, err = emptyext4.Decode(f, size, func(off int64, b []byte) error {
		got = append(got, record{off, bytes.Clone(b)})
		return nil
	})
	require.NoError(t, err)
	return got
}

func TestEveryEmptyExt4DecodesToSortedDisjointRecords(t *testing.T) {
	t.Parallel()
	all := sizes(t)
	require.Len(t, all, 8)
	for name, size := range all {
		var end int64
		for _, r := range records(t, name, size) {
			require.GreaterOrEqual(t, r.off, end, name)
			end = r.off + int64(len(r.data))
			require.LessOrEqual(t, end, size, name)
		}
	}
}

// superblock reads what the init and the kernel read of the ext4 at path: its magic, its label and
// the bytes its blocks cover.
func superblock(t *testing.T, path string) (uint16, string, int64) {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // G304: a disk the test wrote.
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	sb := make([]byte, 1024)
	_, err = f.ReadAt(sb, 1024)
	require.NoError(t, err)
	blocks := int64(binary.LittleEndian.Uint32(sb[0x04:])) | int64(binary.LittleEndian.Uint32(sb[0x150:]))<<32
	logBlock := binary.LittleEndian.Uint32(sb[0x18:])
	label, _, _ := bytes.Cut(sb[0x78:0x78+16], []byte{0})
	return binary.LittleEndian.Uint16(sb[0x38:]), string(label), blocks << (10 + logBlock)
}

func TestWriteGivesASparseFileThatReadsBackAsTheRecords(t *testing.T) {
	t.Parallel()
	for name, size := range sizes(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "rw.ext4")
			require.NoError(t, emptyext4.Write(path, size))

			fi, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, size, fi.Size())
			require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
			st, ok := fi.Sys().(*syscall.Stat_t)
			require.True(t, ok)
			// The host pays for what is kept, never for the size.
			require.Less(t, st.Blocks*512, int64(32<<20))

			f, err := os.Open(path) //nolint:gosec // G304: a disk the test wrote.
			require.NoError(t, err)
			defer func() { _ = f.Close() }()
			for _, r := range records(t, name, size) {
				got := make([]byte, len(r.data))
				_, err := f.ReadAt(got, r.off)
				require.NoError(t, err)
				require.Equal(t, r.data, got, "the record at %d", r.off)
			}
			magic, label, covered := superblock(t, path)
			require.Equal(t, uint16(0xef53), magic)
			require.Equal(t, spec.WriteLabel, label)
			require.Equal(t, size, covered)
		})
	}
}

func TestWriteLeavesAnExistingFileAlone(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rw.ext4")
	require.NoError(t, os.WriteFile(path, []byte("kept"), 0o600))
	require.ErrorIs(t, emptyext4.Write(path, 8<<30), os.ErrExist)
	got, err := os.ReadFile(path) //nolint:gosec // G304: a file the test wrote.
	require.NoError(t, err)
	require.Equal(t, "kept", string(got))
}

func TestWriteRefusesASizeWithoutAnEmptyExt4(t *testing.T) {
	t.Parallel()
	for size, want := range map[int64]string{10 << 30: "no empty ext4 of 10g", 3 << 20: "no empty ext4 of 3145728 bytes"} {
		path := filepath.Join(t.TempDir(), "rw.ext4")
		require.ErrorContains(t, emptyext4.Write(path, size), want)
		require.NoFileExists(t, path)
	}
}

func TestCheckSparseRefusesAFileTheHostWroteWhole(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rw.ext4")
	dense := bytes.Repeat([]byte{1}, 8<<20)
	require.NoError(t, os.WriteFile(path, dense, 0o600))
	f, err := os.Open(path) //nolint:gosec // G304: a file the test wrote.
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.ErrorIs(t, emptyext4.CheckSparse(f, path, 8<<30, 4096), emptyext4.ErrNotSparse)
	require.NoError(t, emptyext4.CheckSparse(f, path, 8<<30, int64(len(dense))))
}

// build writes a file of size holding recs, with the magic m, compressed as tools/mkemptyext4
// writes it.
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

// outOfPlace is what decode says of a record that does not follow the one before.
const outOfPlace = "out of place"

func TestDecodeRefusesAFileOutOfPlace(t *testing.T) {
	t.Parallel()
	const size = 8 << 30
	block := bytes.Repeat([]byte{7}, 4096)
	tests := []struct {
		name string
		file []byte
		want string
	}{
		{name: "another format", file: build(t, "cove-rw0", size), want: "not a file of tools/mkemptyext4"},
		{name: "another size", file: build(t, emptyext4.Magic, 16<<30), want: "it is of 17179869184 bytes, not 8589934592"},
		{
			name: "records overlap",
			file: build(t, emptyext4.Magic, size, record{8192, block}, record{8192, block}),
			want: outOfPlace,
		},
		{
			name: "records go backwards",
			file: build(t, emptyext4.Magic, size, record{8192, block}, record{0, block}),
			want: outOfPlace,
		},
		{name: "a record past the size", file: build(t, emptyext4.Magic, size, record{size - 2048, block}), want: outOfPlace},
		{name: "an empty record", file: build(t, emptyext4.Magic, size, record{0, nil}), want: outOfPlace},
		{name: "a truncated head", file: build(t, emptyext4.Magic, size)[:10], want: "read the empty ext4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := emptyext4.Decode(bytes.NewReader(tt.file), size, func(int64, []byte) error { return nil })
			require.ErrorContains(t, err, tt.want)
		})
	}
}
