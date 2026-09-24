package writedisk_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/writedisk"
)

// fingerprints are the sha256 of the templates once decompressed, as the Dockerfile builds them:
// the same builder and options give the same bytes, and a template that changes changes here.
var fingerprints = map[string]string{
	"8g":   "c377d85d84c0436c2655c9cc87a25be4444bad8262173c2d9503d73ba42e19ff",
	"16g":  "7d7e6fd199ca2b6974a13fcd2d3fa629396df36b319c000dd45a1081ec2d7dc9",
	"32g":  "fa52fdc2f9c0d5fa35d93044054ecdc32b03e9f67304e43d91e04095ecf623e9",
	"64g":  "47dbe40d538b2b338058d0cae408c4c88f377112fd1afed134043286c18d8354",
	"128g": "d2909de55b9081da815736ec0aebbcf35de3ca183fe7c43dccd87697d121dd01",
	"256g": "c19fff26c836903635a72fa8ae89400bbd6e3a2970e30cd98c240ebe1f9d468e",
	"512g": "2c42c77e04f22f2e06d156f8aa678c6a0d909da43703b317c831a23efea279e5",
	"1t":   "3d503eee2c6fb99167511f812d9862ba6df116811994c0fa7e6482e22c27177c",
}

// template returns the embedded template of size, decompressed.
func template(t *testing.T, size writedisk.Size) []byte {
	t.Helper()
	f, err := writedisk.Template(size)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	zr, err := gzip.NewReader(f)
	require.NoError(t, err)
	b, err := io.ReadAll(zr)
	require.NoError(t, err)
	return b
}

func TestEveryTemplateIsTheOneTheBuildMade(t *testing.T) {
	t.Parallel()
	require.Len(t, fingerprints, len(writedisk.Sizes))
	for _, size := range writedisk.Sizes {
		sum := sha256.Sum256(template(t, size))
		require.Equal(t, fingerprints[size.String()], hex.EncodeToString(sum[:]), size.String())
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
	name := string(sb[0x78 : 0x78+16])
	if i := bytes.IndexByte(sb[0x78:0x78+16], 0); i >= 0 {
		name = name[:i]
	}
	return binary.LittleEndian.Uint16(sb[0x38:]), name, blocks << (10 + logBlock)
}

// allocated returns the bytes the host gave the file at path.
func allocated(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	st, ok := fi.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	return st.Blocks * 512
}

func TestCreateWritesASparseExt4OfTheNominalSize(t *testing.T) {
	t.Parallel()
	for _, size := range writedisk.Sizes {
		t.Run(size.String(), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "rw.ext4")
			require.NoError(t, writedisk.Create(path, size))

			fi, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, int64(size), fi.Size())
			require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
			magic, label, covered := superblock(t, path)
			require.Equal(t, uint16(0xef53), magic)
			require.Equal(t, "cove-rw", label)
			require.Equal(t, int64(size), covered)
			// The host pays for the template, never for the nominal size.
			require.Less(t, allocated(t, path), int64(32<<20))
		})
	}
}

func TestExtractGivesBackTheTemplateOfADiskCreateWrote(t *testing.T) {
	t.Parallel()
	for _, size := range []writedisk.Size{writedisk.Sizes[0], writedisk.DefaultCap} {
		path := filepath.Join(t.TempDir(), "rw.ext4")
		require.NoError(t, writedisk.Create(path, size))
		f, err := os.Open(path) //nolint:gosec // G304: a disk the test wrote.
		require.NoError(t, err)
		var out bytes.Buffer
		require.NoError(t, writedisk.Extract(f, &out))
		require.NoError(t, f.Close())
		zr, err := gzip.NewReader(&out)
		require.NoError(t, err)
		got, err := io.ReadAll(zr)
		require.NoError(t, err)
		require.Equal(t, template(t, size), got, size.String())
	}
}

func TestExtractRefusesAFileOfAnotherSize(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "disk")
	require.NoError(t, os.WriteFile(path, make([]byte, 4096), 0o600))
	f, err := os.Open(path) //nolint:gosec // G304: a file the test wrote.
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.ErrorContains(t, writedisk.Extract(f, io.Discard), "not one of the sizes of a write disk")
}

func TestCreateLeavesAnExistingFileAlone(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rw.ext4")
	require.NoError(t, os.WriteFile(path, []byte("kept"), 0o600))
	require.ErrorIs(t, writedisk.Create(path, writedisk.Sizes[0]), os.ErrExist)
	got, err := os.ReadFile(path) //nolint:gosec // G304: a file the test wrote.
	require.NoError(t, err)
	require.Equal(t, "kept", string(got))
}

func TestCreateRefusesASizeWithoutATemplate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rw.ext4")
	require.ErrorContains(t, writedisk.Create(path, writedisk.Size(10<<30)), "no template of 10g")
	require.NoFileExists(t, path)
}

func TestCheckSparseRefusesADiskTheHostWroteWhole(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rw.ext4")
	dense := bytes.Repeat([]byte{1}, 8<<20)
	require.NoError(t, os.WriteFile(path, dense, 0o600))
	f, err := os.Open(path) //nolint:gosec // G304: a file the test wrote.
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.ErrorIs(t, writedisk.CheckSparse(f, path, writedisk.Sizes[0], 4096), writedisk.ErrNotSparse)
	require.NoError(t, writedisk.CheckSparse(f, path, writedisk.Sizes[0], int64(len(dense))))
}

// record is one record of a template, as decode reads it.
type record struct {
	off  uint64
	data []byte
}

// build writes a template of size holding records, compressed as the build writes them.
func build(t *testing.T, size writedisk.Size, magic string, records ...record) []byte {
	t.Helper()
	var raw bytes.Buffer
	_, _ = raw.WriteString(magic)
	_, _ = raw.Write(binary.BigEndian.AppendUint64(nil, uint64(size))) //nolint:gosec // G115: a size of the test.
	for _, r := range records {
		_, _ = raw.Write(binary.BigEndian.AppendUint64(nil, r.off))
		_, _ = raw.Write(binary.BigEndian.AppendUint32(nil, uint32(len(r.data)))) //nolint:gosec // G115: a block or two.
		_, _ = raw.Write(r.data)
	}
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	_, err := zw.Write(raw.Bytes())
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return out.Bytes()
}

// outOfPlace is what decode says of a record that does not follow the one before.
const outOfPlace = "out of place"

func TestDecodeRefusesATemplateOutOfPlace(t *testing.T) {
	t.Parallel()
	size := writedisk.Sizes[0]
	block := bytes.Repeat([]byte{7}, 4096)
	tests := []struct {
		name     string
		template []byte
		want     string
	}{
		{name: "another format", template: build(t, size, "cove-rw0"), want: "not a template"},
		{name: "another size", template: build(t, 16<<30, writedisk.TemplateMagic), want: "it is of 16g, not 8g"},
		{
			name:     "records overlap",
			template: build(t, size, writedisk.TemplateMagic, record{8192, block}, record{8192, block}),
			want:     outOfPlace,
		},
		{
			name:     "records go backwards",
			template: build(t, size, writedisk.TemplateMagic, record{8192, block}, record{0, block}),
			want:     outOfPlace,
		},
		{
			name:     "a record past the size",
			template: build(t, size, writedisk.TemplateMagic, record{8<<30 - 2048, block}),
			want:     outOfPlace,
		},
		{name: "an empty record", template: build(t, size, writedisk.TemplateMagic, record{0, nil}), want: outOfPlace},
		{name: "a truncated record", template: build(t, size, writedisk.TemplateMagic)[:10], want: "read the template"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := writedisk.Decode(bytes.NewReader(tt.template), size, func(int64, []byte) error { return nil })
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestDecodeGivesTheRecordsInOrder(t *testing.T) {
	t.Parallel()
	size := writedisk.Sizes[0]
	a, b := bytes.Repeat([]byte{1}, 4096), bytes.Repeat([]byte{2}, 8192)
	var got []record
	n, err := writedisk.Decode(bytes.NewReader(build(t, size, writedisk.TemplateMagic, record{0, a}, record{1 << 20, b})),
		size, func(off int64, data []byte) error {
			got = append(got, record{uint64(off), bytes.Clone(data)}) //nolint:gosec // G115: an offset of the test.
			return nil
		})
	require.NoError(t, err)
	require.Equal(t, int64(len(a)+len(b)), n)
	require.Equal(t, []record{{0, a}, {1 << 20, b}}, got)
}
