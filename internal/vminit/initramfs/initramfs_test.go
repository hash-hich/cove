package initramfs_test

import (
	"bytes"
	"io"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vminit/initramfs"
	"gitlab.com/hich-hich/cove/internal/vminit/spec"
)

// entry is one header of a newc archive as the kernel reads it, in init/initramfs.c.
type entry struct {
	name                    string
	ino, mode, nlink, mtime uint64
	rmajor, rminor          uint64
	data                    []byte
}

// readArchives reads every archive laid end to end in b, as the kernel unpacks an initramfs, and
// returns their entries, trailers included.
func readArchives(t *testing.T, b []byte) []entry {
	t.Helper()
	var out []entry
	for len(b) > 0 {
		require.GreaterOrEqual(t, len(b), 110)
		require.Equal(t, "070701", string(b[:6]))
		field := func(i int) uint64 {
			n, err := strconv.ParseUint(string(b[6+8*i:14+8*i]), 16, 32)
			require.NoError(t, err)
			return n
		}
		//nolint:gosec // G115: a field holds eight hexadecimal digits, which fit an int.
		size, nameSize := int(field(6)), int(field(11))
		e := entry{ino: field(0), mode: field(1), nlink: field(4), mtime: field(5), rmajor: field(9), rminor: field(10)}
		require.Zero(t, field(2), "owner")
		require.Zero(t, field(3), "group")
		e.name = string(b[110 : 110+nameSize-1])
		require.Zero(t, b[110+nameSize-1], "the name ends with a NUL")
		at := align(110 + nameSize)
		e.data = b[at : at+size]
		b = b[align(at+size):]
		out = append(out, e)
	}
	return out
}

func align(n int) int { return (n + 3) &^ 3 }

func names(es []entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.name)
	}
	return out
}

func TestTheBaseAndTheRunMakeOneInitramfs(t *testing.T) {
	t.Parallel()
	init := []byte("\x7fELF the init, of a length that needs padding")
	run := &spec.Run{Hostname: "sandbox", Agent: "claude", Project: "front"}
	var buf bytes.Buffer
	require.NoError(t, initramfs.WriteBase(&buf, init))
	require.NoError(t, initramfs.WriteRun(&buf, run))

	es := readArchives(t, buf.Bytes())
	const trailer = "TRAILER!!!"
	require.Equal(t, []string{
		"dev", "proc", "sys", "cove", "dev/console", "init", trailer,
		"cove/run.json", trailer,
	}, names(es))

	console := es[4]
	require.Equal(t, uint64(0o020600), console.mode)
	require.Equal(t, [2]uint64{5, 1}, [2]uint64{console.rmajor, console.rminor})
	require.Equal(t, uint64(0o100755), es[5].mode)
	require.Equal(t, init, es[5].data)
	require.Equal(t, uint64(0o040755), es[0].mode)

	var want bytes.Buffer
	require.NoError(t, run.Encode(&want))
	require.Equal(t, want.Bytes(), es[7].data)
}

func TestTheInitramfsIsReproducible(t *testing.T) {
	t.Parallel()
	write := func() []byte {
		var buf bytes.Buffer
		require.NoError(t, initramfs.WriteBase(&buf, []byte("init")))
		return buf.Bytes()
	}
	first := write()
	require.Equal(t, first, write())

	for i, e := range readArchives(t, first) {
		require.Zero(t, e.mtime, e.name)
		require.Equal(t, uint64(i+1), e.ino, e.name)
	}
}

func TestTheWriterRefusesANameOutsideTheRoot(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "/init", "../init", "..", "a/../b", "a/", "TRAILER!!!"} {
		err := initramfs.NewWriter(io.Discard).File(name, 0o644, nil)
		require.Error(t, err, name)
	}
}
