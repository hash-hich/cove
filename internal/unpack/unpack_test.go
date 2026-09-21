//go:debug tarinsecurepath=0
package unpack_test

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/unpack"
)

const (
	// layer is the name the tests give the layer they unpack: what the messages must carry.
	layer = "sha256:0123"
	// rootRefused is the cause given when an entry would make the root something else than a directory.
	rootRefused = "the root of the layer must be a directory"
	// aSymlink and aFile are how the messages describe the type of an entry in the way.
	aSymlink = "a symbolic link"
	aFile    = "a regular file"
)

func TestReaderFlagsNonLocalNames(t *testing.T) {
	t.Parallel()

	// The belt the main package sets, tarinsecurepath=0, is on in this test binary too, so that
	// the tests exercise the reader as the binary has it.
	_, err := tar.NewReader(archive(t, file("../escape", ""))).Next()

	require.ErrorIs(t, err, tar.ErrInsecurePath)
}

func TestBoundsANameThatClimbsAboveTheRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		want    string
		bounded bool
	}{
		{name: "../escape", want: "/escape", bounded: true},
		{name: "ok/../../ailleurs", want: "/ailleurs", bounded: true},
		{name: "/etc/absolu", want: "/etc/absolu"},
		{name: "./bin/sh", want: "/bin/sh"},
		{name: "//double", want: "/double"},
		{name: "a/../b", want: "/b"},
		{name: "...", want: "/..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec, counts, log := unpackAll(t, file(tt.name, ""))

			require.Equal(t, []string{"file " + tt.want + " -rw-r--r-- 0:0 0"}, rec.calls)
			if !tt.bounded {
				require.Equal(t, unpack.Counts{}, counts)
				require.Empty(t, log)
				return
			}
			require.Equal(t, unpack.Counts{NormalizedEntries: 1}, counts)
			require.Equal(t, "layer "+layer+": entry "+fmt.Sprintf("%q", tt.name)+": name "+
				fmt.Sprintf("%q", tt.name)+" climbs above the root, bounded to "+tt.want+"\n", log)
		})
	}
}

func TestWritesASymbolicLinkAsIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target string
	}{
		{name: "relative above the root", target: "../../etc/passwd"},
		{name: "absolute", target: "/etc/passwd"},
		{name: "dangling", target: "nowhere"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec, counts, log := unpackAll(t, symlink("l", tt.target))

			require.Equal(t, []string{"symlink /l Lrwxrwxrwx 0:0 " + tt.target}, rec.calls)
			require.Equal(t, unpack.Counts{}, counts)
			require.Empty(t, log)
		})
	}
}

func TestWritesAHardLinkToWhatWasWrittenEarlier(t *testing.T) {
	t.Parallel()

	rec, counts, log := unpackAll(t, file("a", "x"), link("b", "./a"), link("c", "/a"), link("d", "../a"))

	require.Equal(t, []string{"file /a -rw-r--r-- 0:0 1", "link /b /a", "link /c /a", "link /d /a"}, rec.calls)
	require.Equal(t, unpack.Counts{NormalizedEntries: 1}, counts)
	require.Contains(t, log, `entry "d": hard link target "../a" climbs above the root, bounded to /a`)
}

func TestRefusesAHardLinkToWhatWasNotWritten(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []entry
		want    string
	}{
		{name: "absent", entries: []entry{file("a", ""), link("b", "missing")}, want: "/missing was not written earlier"},
		{name: "written later", entries: []entry{link("b", "a"), file("a", "")}, want: "/a was not written earlier"},
		{name: "itself", entries: []entry{file("b", ""), link("b", "b")}, want: "/b is the entry itself"},
		{name: "a directory", entries: []entry{dir("a"), link("b", "a")}, want: "/a is a directory"},
		{name: "an implicit directory", entries: []entry{file("a/x", ""), link("b", "a")}, want: "/a is a directory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := refused(t, "b", tt.entries...)

			require.Contains(t, err.Error(), "hard link target "+tt.want)
		})
	}
}

func TestRefusesAnEntryUnderWhatIsNotADirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []entry
		want    string
	}{
		{name: "symlink to the root", entries: []entry{symlink("l", "/"), file("l/traverse", "")}, want: aSymlink},
		{name: "regular file", entries: []entry{file("l", ""), file("l/traverse", "")}, want: aFile},
		{name: "deeper", entries: []entry{dir("a"), symlink("a/l", ".."), dir("a/l/traverse")}, want: aSymlink},
		{name: "opaque marker", entries: []entry{file("l", ""), whiteout("l/.wh..wh..opq")}, want: aFile},
		{name: "whiteout", entries: []entry{file("l", ""), whiteout("l/.wh.traverse")}, want: aFile},
		{
			name:    "hard link to a symlink",
			entries: []entry{symlink("s", "/etc"), link("l", "s"), file("l/traverse", "")},
			want:    aSymlink,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := refused(t, tt.entries[len(tt.entries)-1].hdr.Name, tt.entries...)

			require.Contains(t, err.Error(), "is "+tt.want+", not a directory")
			require.Contains(t, err.Error(), "parent /"+strings.SplitN(tt.entries[len(tt.entries)-1].hdr.Name, "/", 2)[0])
		})
	}
}

func TestTheLastEntryOfANameWins(t *testing.T) {
	t.Parallel()

	rec, _, _ := unpackAll(t,
		file("a", "one"), file("a", "two"),
		file("b", ""), dir("b"), file("b/x", ""),
		symlink("c", "/x"), whiteout(".wh.c"),
	)

	require.Equal(t, []string{
		"file /a -rw-r--r-- 0:0 3", "removeall /a", "file /a -rw-r--r-- 0:0 3",
		"file /b -rw-r--r-- 0:0 0", "removeall /b", "mkdir /b drwxr-xr-x 0:0", "file /b/x -rw-r--r-- 0:0 0",
		"symlink /c Lrwxrwxrwx 0:0 /x", "removeall /c", "mknod /c Dc--------- 0:0 0,0",
	}, rec.calls)
	require.Equal(t, "two", rec.content["/a"])
}

func TestTheIndexKeepsTheTypeOfTheLastEntry(t *testing.T) {
	t.Parallel()

	// The directory a was, and the file it held, are gone once a symbolic link takes its name.
	err := refused(t, "a/x", dir("a"), file("a/x", ""), symlink("a", "/tmp"), file("a/x", ""))
	require.Contains(t, err.Error(), "parent /a is a symbolic link")

	err = refused(t, "y", dir("a"), file("a/x", ""), symlink("a", "/tmp"), link("y", "a/x"))
	require.Contains(t, err.Error(), "hard link target /a/x was not written earlier")
}

func TestADirectoryDeclaredAgainKeepsWhatItHolds(t *testing.T) {
	t.Parallel()

	rec, _, _ := unpackAll(t, dir("a"), file("a/x", ""), dir("a"), file("b/y", ""), dir("b"), dir("./"), link("z", "a/x"))

	require.Equal(t, []string{
		"mkdir /a drwxr-xr-x 0:0", "file /a/x -rw-r--r-- 0:0 0", "setattr /a drwxr-xr-x 0:0",
		"file /b/y -rw-r--r-- 0:0 0", "setattr /b drwxr-xr-x 0:0",
		"setattr / drwxr-xr-x 0:0",
		"link /z /a/x",
	}, rec.calls)
}

func TestPassesTheAttributesAsDeclared(t *testing.T) {
	t.Parallel()

	when := time.Date(2026, time.September, 21, 12, 0, 0, 123456789, time.UTC)
	setuid := file("bin/su", "elf")
	setuid.hdr.Mode, setuid.hdr.Uid, setuid.hdr.Gid, setuid.hdr.ModTime = 0o4755, 1000, 2000, when
	// PAX keeps the nanoseconds of a time; ustar would keep the seconds only.
	setuid.hdr.Format = tar.FormatPAX
	typed := file("typed", "")
	// An old archive carries the type in the mode too; the type flag is what counts.
	typed.hdr.Mode = 0o100644
	char := entry{hdr: tar.Header{Typeflag: tar.TypeChar, Name: "dev/null", Mode: 0o666, Devmajor: 1, Devminor: 3}}
	block := entry{hdr: tar.Header{Typeflag: tar.TypeBlock, Name: "dev/sda", Mode: 0o660, Devmajor: 8, Devminor: 0}}
	fifo := entry{hdr: tar.Header{Typeflag: tar.TypeFifo, Name: "run/pipe", Mode: 0o600, Devmajor: 7, Devminor: 7}}
	sticky := dir("tmp")
	sticky.hdr.Mode = 0o1777

	rec, _, _ := unpackAll(t, setuid, typed, char, block, fifo, sticky)

	require.Equal(t, []string{
		"file /bin/su urwxr-xr-x 1000:2000 3",
		"file /typed -rw-r--r-- 0:0 0",
		"mknod /dev/null Dcrw-rw-rw- 0:0 1,3",
		"mknod /dev/sda Drw-rw---- 0:0 8,0",
		"mknod /run/pipe prw------- 0:0 0,0",
		"mkdir /tmp dtrwxrwxrwx 0:0",
	}, rec.calls)
	got := rec.attrs["/bin/su"]
	require.True(t, when.Equal(got.ModTime), "%s", got.ModTime)
	got.ModTime = when
	require.Equal(t, unpack.Attr{Mode: 0o755 | fs.ModeSetuid, UID: 1000, GID: 2000, ModTime: when}, got)
	require.Equal(t, "elf", rec.content["/bin/su"])
}

func TestRefusesWhatCannotBeWrittenAsDeclared(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []entry
		want    string
	}{
		{name: "a file at the root", entries: []entry{file(".", "")}, want: rootRefused},
		{name: "a whiteout of the root", entries: []entry{whiteout(".wh.")}, want: rootRefused},
		{name: "a whiteout above the root", entries: []entry{whiteout(".wh...")}, want: rootRefused},
		{
			name:    "a type no layer holds",
			entries: []entry{{hdr: tar.Header{Typeflag: 'Z', Name: "vendor"}}},
			want:    `entry type 'Z' is not one a layer holds`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := refused(t, tt.entries[0].hdr.Name, tt.entries...)

			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestRefusesAnArchiveItCannotRead(t *testing.T) {
	t.Parallel()

	_, err := unpack.Layer(layer, bytes.NewReader(bytes.Repeat([]byte("x"), 512)), &recorder{}, io.Discard)

	require.EqualError(t, err, "layer "+layer+": read the archive: archive/tar: invalid tar header")
	require.ErrorIs(t, err, tar.ErrHeader)
}

func TestNamesTheEntryAWriterRefuses(t *testing.T) {
	t.Parallel()

	rec := &recorder{refuse: "/b"}
	_, err := unpack.Layer(layer, archive(t, file("a", ""), file("b", "")), rec, io.Discard)

	require.EqualError(t, err, "layer "+layer+`: entry "b": the writer refuses it`)
	require.Equal(t, []string{"file /a -rw-r--r-- 0:0 0"}, rec.calls)
}

func TestRefusesAWriterThatLeavesContentUnread(t *testing.T) {
	t.Parallel()

	_, err := unpack.Layer(layer, archive(t, file("f", "hello")), &partial{}, io.Discard)

	require.EqualError(t, err, "layer "+layer+`: entry "f": the writer took 1 of the 5 bytes of the content`)
}

func TestSkipsTheGlobalHeaderOfAPaxArchive(t *testing.T) {
	t.Parallel()

	global := entry{hdr: tar.Header{
		Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header",
		PAXRecords: map[string]string{"comment": "built by a forge", "SCHILY.xattr.user.x": "y"},
	}}
	rec, counts, log := unpackAll(t, global, file("a", ""))

	require.Equal(t, []string{"file /a -rw-r--r-- 0:0 0"}, rec.calls)
	require.Equal(t, unpack.Counts{}, counts)
	require.Empty(t, log)
}

func TestUnpacksAnEmptyLayer(t *testing.T) {
	t.Parallel()

	rec, counts, log := unpackAll(t)

	require.Empty(t, rec.calls)
	require.Equal(t, unpack.Counts{}, counts)
	require.Empty(t, log)
}

func TestKeepsQuietWithoutALog(t *testing.T) {
	t.Parallel()

	counts, err := unpack.Layer(layer, archive(t, file("../escape", "")), &recorder{}, nil)

	require.NoError(t, err)
	require.Equal(t, unpack.Counts{NormalizedEntries: 1}, counts)
}
