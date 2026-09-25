// Package emptyext4 holds the empty ext4 file systems a write disk starts from, one per size a run
// can ask for, and writes one into a sparse file.
//
// An empty ext4 is formatted once, outside cove, by tools/mkemptyext4, and kept here as its blocks
// that are not zero: a few megabytes, whatever the size, where the file system itself is up to a
// terabyte. What a sandbox mounts at its first boot is, to the byte, what was checked before it
// shipped, on every host and under every kernel. The files of generated/ and their SHA256SUMS are
// the only link between that tool and cove, which shares no code with it: a reviewer reads a
// change of an empty ext4 on the line of SHA256SUMS, which the .gz next to it does not show.
//
// Format of generated/<size>.gz: a gzip stream of the magic cove-rw1, the size as a big endian uint64, then
// one record per run of blocks that are not zero, in increasing order of offset, each its offset
// as a big endian uint64, its length as a big endian uint32, and its bytes.
package emptyext4

import (
	"bufio"
	"compress/gzip"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"syscall"
)

//go:embed generated/*.gz generated/SHA256SUMS
var files embed.FS

// magic opens every file, and names its format.
const magic = "cove-rw1"

// maxKept bounds the bytes an empty ext4 keeps. The largest, 1t, keeps about 14 MiB; a file past
// the bound is not one tools/mkemptyext4 wrote.
const maxKept = 64 << 20

// maxRecord bounds one record, so that no more than that is held in memory.
const maxRecord = 1 << 20

// recordHead is the size of the offset and the length that open a record.
const recordHead = 12

// The units of a size, in powers of two as docker run -m counts them.
const (
	gib = 1 << 30
	tib = 1 << 40
)

// errNotSparse is the refusal of a file system that keeps no sparse files, where a write disk
// would take its whole size on the host at once.
var errNotSparse = errors.New("the file system keeps no sparse files")

// name returns the name of the empty ext4 of size, 64g or 1t.
func name(size int64) string {
	if size >= tib && size%tib == 0 {
		return strconv.FormatInt(size/tib, 10) + "t"
	}
	return strconv.FormatInt(size/gib, 10) + "g"
}

// Write writes at path the empty ext4 of size bytes: its blocks at their offsets, then the rest
// as a hole. It fails when there is no empty ext4 of that size, when path exists, and when the
// file system of path keeps no sparse files, where the file would take its whole size on the
// host at once; what it wrote is removed when it fails.
func Write(path string, size int64) (err error) {
	if size%gib != 0 {
		return fmt.Errorf("there is no empty ext4 of %d bytes", size)
	}
	src, err := files.Open("generated/" + name(size) + ".gz")
	if err != nil {
		return fmt.Errorf("there is no empty ext4 of %s: %w", name(size), err)
	}
	defer func() { _ = src.Close() }()
	//nolint:gosec // G304: path is the disk of the sandbox cove creates.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create the write disk: %w", err)
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("create the write disk: %w", cerr)
		}
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	written, err := decode(src, size, func(off int64, b []byte) error {
		_, err := f.WriteAt(b, off)
		return err //nolint:wrapcheck // The caller names the disk.
	})
	if err != nil {
		return fmt.Errorf("write the write disk %s: %w", path, err)
	}
	if err := f.Truncate(size); err != nil {
		return fmt.Errorf("size the write disk %s: %w", path, err)
	}
	return checkSparse(f, path, size, written)
}

// checkSparse refuses the file f when the host gave it much more than the bytes written, which is
// what a file system without sparse files does with the ftruncate.
func checkSparse(f *os.File, path string, size, written int64) error {
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("read the write disk %s: %w", path, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("read the write disk %s: no blocks in its status", path)
	}
	// The host allocates in blocks of its own, so the slack is generous: a file system without
	// holes gives the whole size, never twice what was written.
	if allocated := st.Blocks * 512; allocated > 2*written+maxRecord {
		return fmt.Errorf("the write disk %s of %s takes %d bytes on the host: %w", path, name(size), allocated, errNotSparse)
	}
	return nil
}

// decode reads the empty ext4 of size in r and calls put with each record and its offset. It
// returns the bytes it gave put, and refuses a file of another format or size, or whose records
// are empty, overlap, go past size or past maxKept.
func decode(r io.Reader, size int64, put func(off int64, b []byte) error) (int64, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("read the empty ext4: %w", err)
	}
	br := bufio.NewReader(zr)
	if err := readHead(br, size); err != nil {
		return 0, err
	}
	var total, end int64
	buf := make([]byte, maxRecord)
	rec := make([]byte, recordHead)
	for {
		if _, err := io.ReadFull(br, rec); errors.Is(err, io.EOF) {
			return total, nil
		} else if err != nil {
			return 0, fmt.Errorf("read the empty ext4: %w", err)
		}
		//nolint:gosec // G115: a value past the top of an int64 turns negative and is refused below.
		off, n := int64(binary.BigEndian.Uint64(rec)), int64(binary.BigEndian.Uint32(rec[8:]))
		if err := checkRecord(off, n, end, total, size); err != nil {
			return 0, err
		}
		if _, err := io.ReadFull(br, buf[:n]); err != nil {
			return 0, fmt.Errorf("read the empty ext4: %w", err)
		}
		if err := put(off, buf[:n]); err != nil {
			return 0, err
		}
		total, end = total+n, off+n
	}
}

// readHead reads the magic and the size that open a file, and refuses a file of another format or
// of another size than size.
func readHead(r io.Reader, size int64) error {
	head := make([]byte, len(magic)+8)
	if _, err := io.ReadFull(r, head); err != nil {
		return fmt.Errorf("read the empty ext4: %w", err)
	}
	if string(head[:len(magic)]) != magic {
		return errors.New("read the empty ext4: not a file of tools/mkemptyext4")
	}
	//nolint:gosec // G115: compared with a size, a value past the top of an int64 differs from it.
	if got := int64(binary.BigEndian.Uint64(head[len(magic):])); got != size {
		return fmt.Errorf("read the empty ext4: it is of %d bytes, not %d", got, size)
	}
	return nil
}

// checkRecord refuses a record of n bytes at off that starts before end, where the one before it
// ended, or that is empty, larger than maxRecord, past size, or past maxKept with the total of the
// records before it.
func checkRecord(off, n, end, total, size int64) error {
	if off < end || n == 0 || n > maxRecord || off+n > size || total+n > maxKept {
		return fmt.Errorf("read the empty ext4: a record of %d bytes at %d is out of place", n, off)
	}
	return nil
}
