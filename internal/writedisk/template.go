package writedisk

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"syscall"

	"golang.org/x/sys/unix"
)

// A template is a gzip stream: the magic, the nominal size, then one record per run of blocks that
// are not zero, in increasing order of offset, each its offset, its length and its bytes, the
// integers big endian. The fingerprint of a template is the sha256 of the stream once
// decompressed, so that a new version of Go compressing it otherwise leaves it unchanged.
//
// The templates are built by the Dockerfile next to this file, which says how.
//
//go:embed templates/*.gz
var templates embed.FS

// templateMagic opens every template, and names its format.
const templateMagic = "cove-rw1"

// blockSize is the block of the file systems of the templates, the unit of what is kept of them.
const blockSize = 4096

// maxTemplate bounds the bytes a template keeps. The largest, 1t, keeps about 14 MiB; a template
// past the bound is a file read without its holes, which the extraction refuses rather than
// embedding a terabyte of zeros.
const maxTemplate = 64 << 20

// maxRecord bounds one record, so that neither side holds more than that in memory.
const maxRecord = 1 << 20

// Create writes at path a new write disk of the nominal size size: the blocks of its template at
// their offsets, then the rest as a hole. It fails when path exists, and when the file system of
// path keeps no sparse files, where the disk would take its whole size on the host at once; what
// it wrote is removed when it fails.
func Create(path string, size Size) error {
	src, err := templates.Open("templates/" + size.String() + ".gz")
	if err != nil {
		return fmt.Errorf("the write disk has no template of %s: %w", size, err)
	}
	defer func() { _ = src.Close() }()
	return writeDisk(path, size, src)
}

// CreateFrom writes at path a write disk as Create does, from the template in the file template
// rather than from those embedded: the build checks with it the templates it is making.
func CreateFrom(path string, size Size, template string) error {
	src, err := os.Open(template) //nolint:gosec // G304: the template the build just wrote.
	if err != nil {
		return fmt.Errorf("open the template: %w", err)
	}
	defer func() { _ = src.Close() }()
	return writeDisk(path, size, src)
}

func writeDisk(path string, size Size, src io.Reader) (err error) {
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
	if err := f.Truncate(int64(size)); err != nil {
		return fmt.Errorf("size the write disk %s: %w", path, err)
	}
	return checkSparse(f, path, size, written)
}

// checkSparse refuses the disk f when the host gave it much more than the bytes written, which is
// what a file system without sparse files does with the ftruncate.
func checkSparse(f *os.File, path string, size Size, written int64) error {
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("read the write disk %s: %w", path, err)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("read the write disk %s: no blocks in its status", path)
	}
	// The host allocates in blocks of its own, so the slack is generous: a file system without
	// holes gives the whole nominal size, never twice what was written.
	if allocated := st.Blocks * 512; allocated > 2*written+maxRecord {
		return fmt.Errorf("the write disk %s of %s takes %d bytes on the host: %w", path, size, allocated, errNotSparse)
	}
	return nil
}

// decode reads the template r of the nominal size size and calls put with each run of its blocks
// and its offset. It returns the bytes it gave put, and refuses a template of another size, or
// whose records overlap, go past size or past maxTemplate.
func decode(r io.Reader, size Size, put func(off int64, b []byte) error) (int64, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("read the template: %w", err)
	}
	br := bufio.NewReader(zr)
	if err := readHead(br, size); err != nil {
		return 0, err
	}
	var total, end int64
	buf := make([]byte, maxRecord)
	rec := make([]byte, 12)
	for {
		if _, err := io.ReadFull(br, rec); errors.Is(err, io.EOF) {
			return total, nil
		} else if err != nil {
			return 0, fmt.Errorf("read the template: %w", err)
		}
		//nolint:gosec // G115: a value past the top of an int64 turns negative and is refused below.
		off, n := int64(binary.BigEndian.Uint64(rec)), int64(binary.BigEndian.Uint32(rec[8:]))
		if err := checkRecord(off, n, end, total, size); err != nil {
			return 0, err
		}
		if _, err := io.ReadFull(br, buf[:n]); err != nil {
			return 0, fmt.Errorf("read the template: %w", err)
		}
		if err := put(off, buf[:n]); err != nil {
			return 0, err
		}
		total, end = total+n, off+n
	}
}

// readHead reads the magic and the size that open a template, and refuses a template of another
// format or of another size than size.
func readHead(r io.Reader, size Size) error {
	head := make([]byte, len(templateMagic)+8)
	if _, err := io.ReadFull(r, head); err != nil {
		return fmt.Errorf("read the template: %w", err)
	}
	if string(head[:len(templateMagic)]) != templateMagic {
		return errors.New("read the template: not a template of a write disk")
	}
	//nolint:gosec // G115: compared with a size, a value past the top of an int64 differs from it.
	if got := Size(binary.BigEndian.Uint64(head[len(templateMagic):])); got != size {
		return fmt.Errorf("read the template: it is of %s, not %s", got, size)
	}
	return nil
}

// checkRecord refuses a record of n bytes at off that starts before end, where the one before it
// ended, or that is empty, larger than maxRecord, past the size of the disk, or past maxTemplate
// with the total of the records before it.
func checkRecord(off, n, end, total int64, size Size) error {
	if off < end || n == 0 || n > maxRecord || off+n > int64(size) || total+n > maxTemplate {
		return fmt.Errorf("read the template: a record of %d bytes at %d is out of place", n, off)
	}
	return nil
}

// Extract writes to w the template of the file system in src, a sparse file of one of Sizes: the
// blocks of its data that are not zero, found by SEEK_DATA and SEEK_HOLE and then read. It runs
// at the build of the templates, never on a disk a run wrote on.
func Extract(src *os.File, w io.Writer) error {
	fi, err := src.Stat()
	if err != nil {
		return fmt.Errorf("read %s: %w", src.Name(), err)
	}
	size := fi.Size()
	if !slices.Contains(Sizes, Size(size)) {
		return fmt.Errorf("%s holds %d bytes, not one of the sizes of a write disk", src.Name(), size)
	}
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("write the template: %w", err)
	}
	e := &extractor{w: zw}
	e.head(size)
	e.walk(src, size)
	e.flush()
	if e.err != nil {
		return e.err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("write the template: %w", err)
	}
	return nil
}

// extractor gathers the blocks that are not zero into the records of a template, the first error
// ending the work.
type extractor struct {
	w     io.Writer
	run   []byte
	start int64
	// done is where the blocks read so far end, so that a region starting inside a block already
	// read does not read it twice.
	done  int64
	total int64
	err   error
}

func (e *extractor) head(size int64) {
	b := append([]byte(templateMagic), make([]byte, 8)...)
	binary.BigEndian.PutUint64(b[len(templateMagic):], uint64(size)) //nolint:gosec // G115: one of Sizes.
	_, e.err = e.w.Write(b)
}

// walk goes through the regions of data of src, of size bytes, skipping its holes.
func (e *extractor) walk(src *os.File, size int64) {
	fd := int(src.Fd())
	for off := int64(0); off < size && e.err == nil; {
		data, err := unix.Seek(fd, off, unix.SEEK_DATA)
		if errors.Is(err, unix.ENXIO) {
			return
		} else if err != nil {
			e.err = fmt.Errorf("find the data of %s: %w", src.Name(), err)
			return
		}
		hole, err := unix.Seek(fd, data, unix.SEEK_HOLE)
		if err != nil {
			e.err = fmt.Errorf("find the holes of %s: %w", src.Name(), err)
			return
		}
		e.region(src, data, hole)
		off = hole
	}
}

// region reads the data of src from data to hole, a block at a time, and keeps the blocks that
// are not zero, a run of them in one record.
func (e *extractor) region(src *os.File, data, hole int64) {
	zero := make([]byte, blockSize)
	blk := make([]byte, blockSize)
	for off := max(data&^(blockSize-1), e.done); off < hole && e.err == nil; off += blockSize {
		e.done = off + blockSize
		n, err := src.ReadAt(blk, off)
		if err != nil && !errors.Is(err, io.EOF) {
			e.err = fmt.Errorf("read %s: %w", src.Name(), err)
			return
		}
		if bytes.Equal(blk[:n], zero[:n]) {
			e.flush()
			continue
		}
		if len(e.run) == 0 || e.start+int64(len(e.run)) != off || len(e.run) >= maxRecord {
			e.flush()
			e.start = off
		}
		e.run = append(e.run, blk[:n]...)
	}
}

// flush writes the run of blocks gathered so far as one record.
func (e *extractor) flush() {
	if len(e.run) == 0 || e.err != nil {
		e.run = e.run[:0]
		return
	}
	if e.total += int64(len(e.run)); e.total > maxTemplate {
		e.err = fmt.Errorf("the template keeps more than %d bytes, the file was read without its holes", maxTemplate)
		return
	}
	rec := make([]byte, 12, 12+len(e.run))
	binary.BigEndian.PutUint64(rec, uint64(e.start)) //nolint:gosec // G115: an offset in a file is positive.
	binary.BigEndian.PutUint32(rec[8:], uint32(len(e.run)))
	_, e.err = e.w.Write(append(rec, e.run...))
	e.run = e.run[:0]
}
