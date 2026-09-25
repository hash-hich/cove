// Command mkemptyext4 makes the empty ext4 file systems cove copies its write disks from, one per
// size a run can ask for, and writes them in ../../internal/emptyext4/generated with their
// SHA256SUMS. make emptyext4 runs it in the image of the Dockerfile next to it.
//
// It runs mkfs.ext4 on a sparse file of each size, then keeps only the blocks that are not zero:
// a few megabytes, whatever the size, where the file system itself is up to a terabyte. It runs
// in the image of the Dockerfile next to it, where e2fsprogs is pinned, and refuses another
// version of mke2fs, which would make other bytes. It shares no code with cove: the format below
// is the contract, and cove reads it in internal/writedisk.
//
// Format: a gzip stream of the magic, the size as a big endian uint64, then one record per run of
// blocks that are not zero, in increasing order of offset, each its offset as a big endian uint64,
// its length as a big endian uint32, and its bytes.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// magic opens every file this command writes, and names its format.
const magic = "cove-rw1"

// label is the label of every empty ext4, which the init of cove checks before it mounts a disk.
const label = "cove-rw"

// blockSize is the block of the file systems made, the unit of what is kept of them.
const blockSize = 4096

// maxKept bounds the bytes kept of one file system. The largest, 1t, keeps about 14 MiB; past the
// bound, the file was read without its holes, and a terabyte of zeros is refused rather than kept.
const maxKept = 64 << 20

// maxRecord bounds one record, so that neither side holds more than that in memory.
const maxRecord = 1 << 20

// headSize is the size of the magic and the size that open a file.
const headSize = len(magic) + 8

// recordHead is the size of the offset and the length that open a record.
const recordHead = 12

// out is where cove embeds the empty ext4, from the directory of this command.
const out = "../../internal/emptyext4/generated"

// sums names the file of the fingerprints of what is written in out, in the format of sha256sum.
const sums = "SHA256SUMS"

// e2fsprogsVersion is the version of mke2fs the Dockerfile pins, and the only one that makes the
// bytes of SHA256SUMS.
const e2fsprogsVersion = "1.47.2"

// sizes are the sizes a write disk can take, as cove names them.
var sizes = []string{"8g", "16g", "32g", "64g", "128g", "256g", "512g", "1t"}

func main() {
	err := checkMke2fs()
	if err == nil {
		err = makeAll(out, os.TempDir())
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "mkemptyext4:", err)
		os.Exit(1)
	}
}

// checkMke2fs refuses a mke2fs of another version than the pinned one.
func checkMke2fs() error {
	b, err := exec.Command("mke2fs", "-V").CombinedOutput() //nolint:noctx // A build step, run to its end.
	if err != nil {
		return fmt.Errorf("run mke2fs, which the image of the Dockerfile holds: %w", err)
	}
	if !strings.HasPrefix(string(b), "mke2fs "+e2fsprogsVersion+" ") {
		return fmt.Errorf("mke2fs is %q, not %s: run it in the image of the Dockerfile",
			strings.SplitN(string(b), "\n", 2)[0], e2fsprogsVersion)
	}
	return nil
}

// makeAll writes in dir the empty ext4 of every size, made in the directory work, and their
// SHA256SUMS.
func makeAll(dir, work string) error {
	var list strings.Builder
	for _, name := range sizes {
		size, err := parseSize(name)
		if err != nil {
			return err
		}
		kept, err := makeOne(work, name, size)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".gz"), kept, 0o644); err != nil { //nolint:gosec // G306: committed.
			return fmt.Errorf("%s: %w", name, err)
		}
		_, _ = fmt.Fprintf(&list, "%x  %s.gz\n", sha256.Sum256(kept), name)
	}
	//nolint:gosec // G306: committed.
	if err := os.WriteFile(filepath.Join(dir, sums), []byte(list.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", sums, err)
	}
	return nil
}

// makeOne makes the empty ext4 of size, named name, in the directory work, and returns what is kept
// of it.
func makeOne(work, name string, size int64) ([]byte, error) {
	disk := filepath.Join(work, name+".ext4")
	if err := sparse(disk, size); err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(disk) }()
	//nolint:noctx,gosec // G204: a build step on a file of its own, run to its end.
	mkfs := exec.Command("mkfs.ext4", mkfsArgs(disk)...)
	mkfs.Env, mkfs.Stdout, mkfs.Stderr = mkfsEnv(), os.Stderr, os.Stderr
	if err := mkfs.Run(); err != nil {
		return nil, fmt.Errorf("mkfs.ext4: %w", err)
	}
	return keepFile(disk)
}

// parseSize reads a size in gibibytes or tebibytes, 64g or 1t, as docker run -m counts them.
func parseSize(v string) (int64, error) {
	units := map[string]int64{"g": 1 << 30, "t": 1 << 40}
	for suffix, unit := range units {
		if n, ok := strings.CutSuffix(v, suffix); ok {
			i, err := strconv.ParseInt(n, 10, 64)
			if err != nil || i <= 0 || i > 1<<20 {
				break
			}
			return i * unit, nil
		}
	}
	return 0, fmt.Errorf("a size is a count of g or t, not %q", v)
}

// mkfsArgs returns the arguments of mkfs.ext4 for the file system at path. What each one fixes:
//
//   - -b 4096, -i 16384: a block of the page of the guest, and an inode per 16 KiB, the default
//     ratio. The count of inodes never changes afterwards, and node_modules or the layers of an
//     engine take millions: an ENOSPC with space left is the most misleading failure. Tables not
//     yet written cost nothing here.
//   - -m 0: no block kept for root, since the agent is root.
//   - -L: the label the init checks before it mounts the disk.
//   - -U, hash_seed, and E2FSPROGS_FAKE_TIME in mkfsEnv: the file system is reproducible byte for
//     byte. Every disk carries the same UUID, which holds because a VM mounts one write disk only,
//     by its device.
//   - lazy_itable_init, lazy_journal_init, nodiscard: neither the tables of inodes nor the journal
//     are zeroed, which is what keeps what is kept small. The init mounts with noinit_itable, or
//     the kernel zeroes them in the background and grows the file of the host by gigabytes.
//   - ^resize_inode: a disk is never grown, and mke2fs writes the blocks it would reserve for the
//     table of group descriptors: measured, 4 MiB more to write for each disk at 8g, and 36 MiB
//     less to use. The day growing a disk is decided, the empty ext4 are made again with it.
//   - -J size=256: a journal of 256 MiB whatever the size. It stays, as nothing else would bring
//     back a disk after a VM is cut off: cove ships no fsck.
func mkfsArgs(path string) []string {
	return []string{
		"-F", "-q", "-b", "4096", "-i", "16384", "-m", "0", "-L", label,
		"-U", "5f3c9a1e-7c2d-4b0e-9a61-3d8e2f4c6b10",
		"-E", "hash_seed=a1d0c6e2-3f4b-4c8a-9e7d-2b5f1a6c8e34,lazy_itable_init=1,lazy_journal_init=1,nodiscard",
		"-O", "^resize_inode", "-J", "size=256",
		path,
	}
}

// mkfsEnv is the environment mkfs.ext4 runs in: the time is one second after the epoch, since
// mke2fs reads zero as no time and takes the clock.
func mkfsEnv() []string {
	return append(os.Environ(), "E2FSPROGS_FAKE_TIME=1")
}

// sparse creates at path a file of size bytes that is one hole.
func sparse(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a file of the build.
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	err = f.Truncate(size)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("size %s: %w", path, err)
	}
	return nil
}

// keepFile returns what is kept of the file system at path.
func keepFile(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // G304: a file of the build.
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	var b bytes.Buffer
	if err := keep(f, &b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// keep writes to w what is kept of the file system in src: the blocks of its data that are not
// zero, found by seekData and seekHole and then read.
func keep(src *os.File, w io.Writer) error {
	fi, err := src.Stat()
	if err != nil {
		return fmt.Errorf("read %s: %w", src.Name(), err)
	}
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	k := &keeper{w: zw}
	k.head(fi.Size())
	k.walk(src, fi.Size())
	k.flush()
	if k.err != nil {
		return k.err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("compress: %w", err)
	}
	return nil
}

// keeper gathers the blocks that are not zero into records, the first error ending the work.
type keeper struct {
	w     io.Writer
	run   []byte
	start int64
	// done is where the blocks read so far end, so that a region starting inside a block already
	// read does not read it twice.
	done  int64
	total int64
	err   error
}

func (k *keeper) head(size int64) {
	b := append([]byte(magic), make([]byte, 8)...)
	binary.BigEndian.PutUint64(b[len(magic):], uint64(size)) //nolint:gosec // G115: a size is positive.
	_, k.err = k.w.Write(b)
}

// walk goes through the regions of data of src, of size bytes, skipping its holes.
func (k *keeper) walk(src *os.File, size int64) {
	for off := int64(0); off < size && k.err == nil; {
		data, err := src.Seek(off, seekData)
		if errors.Is(err, syscall.ENXIO) {
			return
		} else if err != nil {
			k.err = fmt.Errorf("find the data of %s: %w", src.Name(), err)
			return
		}
		hole, err := src.Seek(data, seekHole)
		if err != nil {
			k.err = fmt.Errorf("find the holes of %s: %w", src.Name(), err)
			return
		}
		k.region(src, data, hole)
		off = hole
	}
}

// region reads the data of src from data to hole, a block at a time, and keeps the blocks that
// are not zero, a run of them in one record.
func (k *keeper) region(src *os.File, data, hole int64) {
	zero := make([]byte, blockSize)
	blk := make([]byte, blockSize)
	for off := max(data&^(blockSize-1), k.done); off < hole && k.err == nil; off += blockSize {
		k.done = off + blockSize
		n, err := src.ReadAt(blk, off)
		if err != nil && !errors.Is(err, io.EOF) {
			k.err = fmt.Errorf("read %s: %w", src.Name(), err)
			return
		}
		if bytes.Equal(blk[:n], zero[:n]) {
			k.flush()
			continue
		}
		if len(k.run) == 0 || k.start+int64(len(k.run)) != off || len(k.run) >= maxRecord {
			k.flush()
			k.start = off
		}
		k.run = append(k.run, blk[:n]...)
	}
}

// flush writes the run of blocks gathered so far as one record.
func (k *keeper) flush() {
	if len(k.run) == 0 || k.err != nil {
		k.run = k.run[:0]
		return
	}
	if k.total += int64(len(k.run)); k.total > maxKept {
		k.err = fmt.Errorf("more than %d bytes kept, the file was read without its holes", maxKept)
		return
	}
	rec := make([]byte, recordHead, recordHead+len(k.run))
	binary.BigEndian.PutUint64(rec, uint64(k.start)) //nolint:gosec // G115: an offset in a file is positive.
	binary.BigEndian.PutUint32(rec[8:], uint32(len(k.run)))
	_, k.err = k.w.Write(append(rec, k.run...))
	k.run = k.run[:0]
}

// writeBack writes at path, which must not exist, the file system of size kept in kept.
func writeBack(path string, kept []byte, size int64) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: a file of the build.
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("write back %s: %w", path, cerr)
		}
	}()
	if err := records(bytes.NewReader(kept), size, func(off int64, b []byte) error {
		_, err := f.WriteAt(b, off)
		return err //nolint:wrapcheck // The caller names the file.
	}); err != nil {
		return fmt.Errorf("write back %s: %w", path, err)
	}
	if err := f.Truncate(size); err != nil {
		return fmt.Errorf("write back %s: %w", path, err)
	}
	return nil
}

// records reads what was kept of a file system of size in r and calls put with each record,
// refusing a head of another format or size, and records that overlap or go past size.
func records(r io.Reader, size int64, put func(off int64, b []byte) error) error {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("decompress: %w", err)
	}
	br := bufio.NewReader(zr)
	head := make([]byte, headSize)
	if _, err := io.ReadFull(br, head); err != nil {
		return fmt.Errorf("read the head: %w", err)
	}
	//nolint:gosec // G115: compared with a size, a value past the top of an int64 differs from it.
	if string(head[:len(magic)]) != magic || int64(binary.BigEndian.Uint64(head[len(magic):])) != size {
		return fmt.Errorf("not a file system of %d bytes kept by mkemptyext4", size)
	}
	return eachRecord(br, size, put)
}

// eachRecord reads the records that follow the head in r and calls put with each one.
func eachRecord(r io.Reader, size int64, put func(off int64, b []byte) error) error {
	var end int64
	rec := make([]byte, recordHead)
	buf := make([]byte, maxRecord)
	for {
		if _, err := io.ReadFull(r, rec); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("read a record: %w", err)
		}
		//nolint:gosec // G115: a value past the top of an int64 turns negative and is refused below.
		off, n := int64(binary.BigEndian.Uint64(rec)), int64(binary.BigEndian.Uint32(rec[8:]))
		if off < end || n == 0 || n > maxRecord || off+n > size {
			return fmt.Errorf("a record of %d bytes at %d is out of place", n, off)
		}
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return fmt.Errorf("read a record: %w", err)
		}
		if err := put(off, buf[:n]); err != nil {
			return err
		}
		end = off + n
	}
}
