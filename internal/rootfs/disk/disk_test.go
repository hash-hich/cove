// The loop asks archive/tar for the names it would otherwise wave through: with
// tarinsecurepath=0, Next reports a name that is not local and hands the header over all the
// same. The binary sets the same value in cmd/cove, so the tests run the regime the users get.
//go:debug tarinsecurepath=0

package disk_test

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/rootfs/disk"
)

// layerName names the layer under conversion in the tests, as a DiffID does.
const layerName = "sha256:0f1e2d"

// item is one entry of an archive built by a test: its header, and the bytes of its body.
type item struct {
	hdr  tar.Header
	body string
}

// file, dir, symlink and hardlink build the items the tests feed the loop.
func file(name, body string) item {
	return item{hdr: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}, body: body}
}

func dir(name string) item {
	return item{hdr: tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755}}
}

func symlink(name, target string) item {
	return item{hdr: tar.Header{Name: name, Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: target}}
}

func hardlink(name, target string) item {
	return item{hdr: tar.Header{Name: name, Typeflag: tar.TypeLink, Mode: 0o644, Linkname: target}}
}

// withXattr hangs one extended attribute on an item, as a PAX record does.
func withXattr(it item, name, value string) item {
	if it.hdr.PAXRecords == nil {
		it.hdr.PAXRecords = map[string]string{}
	}
	it.hdr.PAXRecords["SCHILY.xattr."+name] = value
	return it
}

// archive writes the items as a tar stream, the shape the loop reads.
func archive(t testing.TB, items ...item) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, it := range items {
		hdr := it.hdr
		require.NoError(t, w.WriteHeader(&hdr))
		_, err := io.WriteString(w, it.body)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// attr is one call to Setxattr, kept in the order it came.
type attr struct {
	path  string
	name  string
	value []byte
}

// recorder is the sink of the tests: it keeps what the loop decided, bodies included, and fails
// on demand to show what the loop does with a writer that refuses.
type recorder struct {
	entries []disk.Entry
	bodies  []string
	attrs   []attr
}

func (r *recorder) Add(e disk.Entry) error {
	body := ""
	if e.Body != nil {
		read, err := io.ReadAll(e.Body)
		if err != nil {
			return fmt.Errorf("read the body of %s: %w", e.Path, err)
		}
		body = string(read)
	}
	e.Body = nil
	r.entries = append(r.entries, e)
	r.bodies = append(r.bodies, body)
	return nil
}

func (r *recorder) Setxattr(path, name string, value []byte) error {
	r.attrs = append(r.attrs, attr{path: path, name: name, value: value})
	return nil
}

// paths lists the paths of what was written, in order.
func (r *recorder) paths() []string {
	out := make([]string, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e.Path)
	}
	return out
}

// result is what one conversion of the tests did.
type result struct {
	rec    *recorder
	log    string
	counts disk.Counts
	err    error
}

// convert runs the loop on an archive of the items and gathers everything a test looks at.
func convert(t *testing.T, items ...item) result {
	t.Helper()
	rec := &recorder{}
	var log strings.Builder
	c := &disk.Converter{Layer: layerName, Log: &log}
	counts, err := c.Convert(bytes.NewReader(archive(t, items...)), rec)
	return result{rec: rec, log: log.String(), counts: counts, err: err}
}

func TestBoundsANameThatClimbsAboveTheRoot(t *testing.T) {
	t.Parallel()
	got := convert(t, file("../escape", "hello"))

	require.NoError(t, got.err)
	require.Equal(t, []string{"/escape"}, got.rec.paths())
	require.Equal(t, []string{"hello"}, got.rec.bodies)
	require.Equal(t, 1, got.counts.NormalizedEntries)
	require.Contains(t, got.log, layerName+": ../escape: bounded to /escape")
}

func TestBoundsANameThatClimbsThroughItsParents(t *testing.T) {
	t.Parallel()
	got := convert(t, file("ok/../../ailleurs", ""))

	require.NoError(t, got.err)
	require.Equal(t, []string{"/ailleurs"}, got.rec.paths())
	require.Equal(t, 1, got.counts.NormalizedEntries)
}

func TestKeepsAnAbsoluteNameWhereItPointsAndCountsNoBounding(t *testing.T) {
	t.Parallel()
	got := convert(t, file("/etc/absolu", ""))

	require.NoError(t, got.err)
	require.Equal(t, []string{"/etc/absolu"}, got.rec.paths())
	require.Zero(t, got.counts.NormalizedEntries)
	require.Empty(t, got.log)
}

func TestWritesASymbolicLinkAsDeclared(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target string
	}{
		{name: "climbing above the root", target: "../../../etc/shadow"},
		{name: "absolute", target: "/etc/passwd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := convert(t, symlink("link", tt.target))

			require.NoError(t, got.err)
			require.Len(t, got.rec.entries, 1)
			require.Equal(t, disk.Symlink, got.rec.entries[0].Kind)
			require.Equal(t, tt.target, got.rec.entries[0].Target)
			require.Zero(t, got.counts.NormalizedEntries)
		})
	}
}

func TestRefusesAHardLinkToSomethingTheLayerNeverWrote(t *testing.T) {
	t.Parallel()
	got := convert(t, file("there", ""), hardlink("link", "elsewhere"))

	require.ErrorIs(t, got.err, disk.ErrImpossibleEntry)
	require.Contains(t, got.err.Error(), layerName)
	require.Contains(t, got.err.Error(), "link")
	require.Contains(t, got.err.Error(), "/elsewhere")
}

func TestWritesAHardLinkToWhatTheLayerWroteBefore(t *testing.T) {
	t.Parallel()
	got := convert(t, file("there", "x"), hardlink("link", "there"))

	require.NoError(t, got.err)
	require.Len(t, got.rec.entries, 2)
	require.Equal(t, disk.HardLink, got.rec.entries[1].Kind)
	require.Equal(t, "/there", got.rec.entries[1].Target)
}

func TestRefusesAPathHangingFromSomethingThatIsNotADirectory(t *testing.T) {
	t.Parallel()
	got := convert(t, symlink("l", "/"), file("l/traverse", ""))

	require.ErrorIs(t, got.err, disk.ErrImpossibleEntry)
	require.Contains(t, got.err.Error(), layerName)
	require.Contains(t, got.err.Error(), "l/traverse")
	require.Contains(t, got.err.Error(), "/l")
	require.Equal(t, []string{"/l"}, got.rec.paths())
}

func TestTheLastEntryOfANameWinsAndTheIndexHoldsItsKind(t *testing.T) {
	t.Parallel()
	got := convert(t, dir("a"), file("a", "second"), file("a/b", ""))

	require.ErrorIs(t, got.err, disk.ErrImpossibleEntry)
	require.Contains(t, got.err.Error(), "its parent /a is a regular file")
	require.Equal(t, []string{"/a", "/a"}, got.rec.paths())
	require.Equal(t, disk.Dir, got.rec.entries[0].Kind)
	require.Equal(t, disk.Regular, got.rec.entries[1].Kind)
	require.Equal(t, []string{"", "second"}, got.rec.bodies)
}

func TestRefusesAnEntryThatWouldReplaceTheRoot(t *testing.T) {
	t.Parallel()
	got := convert(t, file(".", ""))

	require.ErrorIs(t, got.err, disk.ErrImpossibleEntry)
	require.Contains(t, got.err.Error(), "the root of the image is a directory")
}

func TestTurnsTheWhiteoutsIntoWhatOverlayfsReads(t *testing.T) {
	t.Parallel()
	got := convert(t, dir("d"), file("d/.wh.x", ""), file("d/.wh..wh..opq", ""))

	require.NoError(t, got.err)
	require.Equal(t, []string{"/d", "/d/x"}, got.rec.paths())
	require.Equal(t, disk.CharDevice, got.rec.entries[1].Kind)
	require.Zero(t, got.rec.entries[1].Major)
	require.Zero(t, got.rec.entries[1].Minor)
	require.Equal(t, []attr{{path: "/d", name: "trusted.overlay.opaque", value: []byte("y")}}, got.rec.attrs)
}

func TestIgnoresTheMarkersOverlayfsDoesNotRead(t *testing.T) {
	t.Parallel()
	got := convert(t, dir("d"), file("d/.wh..wh.plnk", ""), file("d/.wh..wh.aufs", ""))

	require.NoError(t, got.err)
	require.Equal(t, []string{"/d"}, got.rec.paths())
	require.Empty(t, got.rec.attrs)
	require.Contains(t, got.log, "d/.wh..wh.plnk: ignored")
}

func TestNormalizesTheNameCarriedUnderAWhiteoutPrefix(t *testing.T) {
	t.Parallel()
	got := convert(t, file("../.wh.x", ""))

	require.NoError(t, got.err)
	require.Equal(t, []string{"/x"}, got.rec.paths())
	require.Equal(t, disk.CharDevice, got.rec.entries[0].Kind)
	require.Equal(t, 1, got.counts.NormalizedEntries)
}

func TestWritesTheRawBytesOfAnExtendedAttribute(t *testing.T) {
	t.Parallel()
	value := string([]byte{'a', 0x00, 0xff, 'b'})
	got := convert(t, withXattr(file("bin", ""), "user.weird", value))

	require.NoError(t, got.err)
	require.Equal(t, []attr{{path: "/bin", name: "user.weird", value: []byte(value)}}, got.rec.attrs)
	require.Zero(t, got.counts.UnknownXattrPrefixes)
}

func TestWritesAnAttributeOfAnUnknownPrefixAndSaysTheGuestWillNotSeeIt(t *testing.T) {
	t.Parallel()
	got := convert(t, withXattr(file("bin", ""), "bogus.thing", "v"))

	require.NoError(t, got.err)
	require.Equal(t, []attr{{path: "/bin", name: "bogus.thing", value: []byte("v")}}, got.rec.attrs)
	require.Equal(t, 1, got.counts.UnknownXattrPrefixes)
	require.Contains(t, got.log, "bogus.thing")
	require.Contains(t, got.log, "the guest will not see it")
}
