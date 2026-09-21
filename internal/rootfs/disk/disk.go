// Package disk turns the tar archive of one OCI layer into the entries to write into the disk
// image of that layer.
//
// It is the only code of cove that reads bytes nobody vouches for, and the only place where the
// fidelity the rootfs promises is decided: what is bounded, what fails, what a whiteout becomes,
// what an extended attribute is worth. Nothing downstream catches a wrong decision taken here,
// and the failures are silent ones: an image that lost an opaque directory mounts and reads
// without a word, it just hands back files that were meant to be gone.
//
// The loop decides, the writer writes: entries leave through Sink, which the writer implements
// and the tests record. Paths are handled with path and strings alone, never with path/filepath,
// which carries the rules of the host, Windows included; a lint rule holds that line.
package disk

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrImpossibleEntry reports an entry no disk image can hold: a hard link to something the layer
// never wrote, a path hanging from something that is not a directory, or a root turned into
// something else. The layer is refused whole. A path that merely climbs above the root is
// bounded instead, and counted.
var ErrImpossibleEntry = errors.New("the entry cannot be written")

// modeBits is what an entry keeps of the mode the archive declares: the permissions and the
// setuid, setgid and sticky bits. The type of the entry is in Kind, never in the mode.
const modeBits = 0o7777

// Kind is what an entry of a layer is.
type Kind uint8

// The kinds of entry a disk image holds. The zero value is no kind: an entry always has one, and
// a type of the archive that maps to none of these is not an entry.
const (
	Dir Kind = iota + 1
	Regular
	Symlink
	HardLink
	CharDevice
	BlockDevice
	Fifo
)

// String names the kind as a message says it.
func (k Kind) String() string {
	switch k {
	case Dir:
		return "directory"
	case Regular:
		return "regular file"
	case Symlink:
		return "symbolic link"
	case HardLink:
		return "hard link"
	case CharDevice:
		return "character device"
	case BlockDevice:
		return "block device"
	case Fifo:
		return "fifo"
	default:
		return "nothing"
	}
}

// Entry is one thing to write, as the loop decided it. Path is rooted at the root of the image
// and never climbs above it; the rest is what the archive declared, unless a whiteout rule made
// the entry something else.
type Entry struct {
	// Path is where the entry goes, from the root of the image: "/" for the root itself,
	// "/etc/passwd" for a file. Never empty, never ending in a slash.
	Path string
	// Kind is what to write there.
	Kind Kind
	// Mode holds the permission bits and the setuid, setgid and sticky bits as the archive
	// declares them, and nothing of the type of the entry.
	Mode int64
	// UID and GID own the entry, as numbers. The names the archive may carry beside them are
	// dropped: nothing in the guest resolves them.
	UID, GID int
	// ModTime is the modification time the archive declares.
	ModTime time.Time
	// Size is the length of Body, on a Regular entry; zero on every other kind.
	Size int64
	// Body reads the content of a Regular entry, Size bytes at most. It is valid during the call
	// to Add alone, and what the sink leaves unread is skipped with the rest of the entry.
	Body io.Reader
	// Target is the target of a Symlink, exactly as the archive declares it, absolute or climbing
	// above the root included; or, on a HardLink, the path of the entry it points at, rooted like
	// Path and written already.
	Target string
	// Major and Minor number a CharDevice or a BlockDevice.
	Major, Minor int64
}

// Sink receives what a conversion decided, in the order of the archive. The writer implements it;
// composing a group of layers writes through the same two calls without going through the loop.
type Sink interface {
	// Add writes one entry. Its parents have been written already, as directories.
	Add(Entry) error
	// Setxattr sets one extended attribute on the entry at path, with the raw bytes of the value:
	// a NUL and a byte that is not UTF-8 are a value like any other.
	Setxattr(path, name string, value []byte) error
}

// Counts is what a conversion counted. It is handed back to the caller, which is what writes it
// to meta.json and to the report.
type Counts struct {
	// NormalizedEntries counts the paths bounded to the root, the target of a hard link included.
	NormalizedEntries int
	// UnknownXattrPrefixes counts the extended attributes written under a prefix EROFS does not
	// read. They are in the image; they will not be readable in the guest.
	UnknownXattrPrefixes int
}

// Converter turns the archives of layers into the entries to write and says what it did on Log.
type Converter struct {
	// Layer names the layer in every message and every error: its DiffID, or the digest of the
	// compressed blob when no DiffID is known. Conversions run in parallel, so no line of Log is
	// worth reading without it.
	Layer string
	// Log receives the facts of the conversion, one line each; nil keeps quiet.
	Log io.Writer
}

// conversion is one layer being converted: what it has written so far, and what it counted.
type conversion struct {
	*Converter
	out Sink
	// index holds the kind written at each path. It is what refuses a path hanging from a file,
	// what finds the target of a hard link, and what makes the last entry of a name the one that
	// counts.
	index  map[string]Kind
	counts Counts
}

// logf writes one line of facts to Log.
func (cv *conversion) logf(format string, args ...any) {
	if cv.Log != nil {
		_, _ = fmt.Fprintf(cv.Log, format+"\n", args...)
	}
}
