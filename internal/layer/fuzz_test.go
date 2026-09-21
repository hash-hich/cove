package layer_test

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"

	"gitlab.com/hich-hich/cove/internal/layer"
)

// sink is the Writer of the fuzzing: it keeps nothing, drains each content, and checks on every
// call what the loop promises of a path: absolute, clean, and never the root removed.
type sink struct {
	f *testing.F
}

// check fails the fuzzing when p is not what the loop promises.
func (s *sink) check(p string) error {
	if !path.IsAbs(p) || path.Clean(p) != p {
		s.f.Errorf("path %q is not absolute and clean", p)
	}
	return nil
}

func (s *sink) Mkdir(p string, _ layer.Attr) error             { return s.check(p) }
func (s *sink) Setattr(p string, _ layer.Attr) error           { return s.check(p) }
func (s *sink) Symlink(p string, _ layer.Attr, _ string) error { return s.check(p) }
func (s *sink) Link(p, target string) error                    { return s.check(p + target) }
func (s *sink) Mknod(p string, _ layer.Attr, _, _ int64) error { return s.check(p) }
func (s *sink) Setxattr(p, _, _ string) error                  { return s.check(p) }

func (s *sink) WriteFile(p string, _ layer.Attr, _ int64, content io.Reader) error {
	if _, err := io.Copy(io.Discard, content); err != nil {
		return fmt.Errorf("drain the content: %w", err)
	}
	return s.check(p)
}

func (s *sink) RemoveAll(p string) error {
	if p == "/" {
		s.f.Error("the root was removed")
	}
	return s.check(p)
}

// hostile returns the archives the fuzzing starts from: every case the loop decides, the ones that
// go wrong on purpose, and a few that are not archives at all.
func hostile(f *testing.F) [][]byte {
	deep := strings.Repeat("d/", 200) + "leaf"
	long := strings.Repeat("n", 300)
	huge := tar.Header{Typeflag: tar.TypeReg, Name: "huge", Mode: 0o644, Size: 1 << 40}
	device := entry{hdr: tar.Header{Typeflag: tar.TypeChar, Name: "dev/x", Mode: 0o666, Devmajor: 1 << 40, Devminor: -1}}
	odd := entry{hdr: tar.Header{Typeflag: tar.TypeReg, Name: "odd", Mode: -1, Uid: -1, Gid: 1 << 40}}
	global := entry{hdr: tar.Header{
		Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header",
		PAXRecords: map[string]string{"comment": "x"},
	}}
	// A valid archive, to cut in the middle of an entry and at the end of a data section.
	whole := read(f, archive(f, file("a", strings.Repeat("a", 600)), file("b", "b")))
	return [][]byte{
		read(f, archive(f, file("../escape", "x"), file("ok/../../ailleurs", ""), file("/etc/absolu", ""))),
		read(f, archive(f, symlink("l", "/"), file("l/traverse", ""))),
		read(f, archive(f, symlink("up", "../../.."), symlink("abs", "/etc/passwd"), symlink("empty", ""))),
		read(f, archive(f, file("a", "x"), link("b", "a"), link("c", "missing"), link("d", "d"), link("e", "../a"))),
		read(f, archive(f, dir("a"), file("a/x", ""), symlink("a", "/tmp"), file("a/x", ""), link("y", "a/x"))),
		read(f, archive(f, file("a", "one"), file("a", "two"), dir("a"), file("a/b", ""), dir("./"), dir("/"))),
		read(f, archive(f, dir("d"), whiteout("d/.wh.x"), whiteout("d/.wh..wh..opq"), whiteout("e/.wh..wh..opq"))),
		read(f, archive(f, whiteout(".wh."), whiteout(".wh.."), whiteout(".wh..."), whiteout("d/.wh..."))),
		read(f, archive(f, whiteout("../.wh.x"), whiteout(".wh..wh.plnk"), whiteout(".wh..wh..opq.x"))),
		read(f, archive(f, whiteout("/.wh..wh..opq"), whiteout("a/b/.wh..wh..opq"), whiteout("a/.wh.b"))),
		read(f, archive(f, xattrs(file("f", ""), map[string]string{"user.raw": "a\x00b\xff", "": "", "foo.bar": "v"}))),
		read(f, archive(f, file(deep, ""), symlink(deep+"/l", ".."), file(deep+"/l/x", ""), file(long, ""))),
		read(f, archive(f, device, odd, entry{hdr: tar.Header{Typeflag: 'Z', Name: "z"}})),
		read(f, archive(f, global, entry{hdr: tar.Header{Typeflag: tar.TypeFifo, Name: "p"}})),
		read(f, archive(f, file("", ""), file(".", ""), file("..", ""), dir("/"))),
		headerOnly(f, &huge),
		bytes.Repeat([]byte("x"), 512),
		make([]byte, 512),
		make([]byte, 1024),
		[]byte("not a tar"),
		{},
		whole[:700],
		whole[:512+600],
	}
}

// headerOnly returns the header hdr followed by one empty block: an archive whose entry announces
// a body that is not there.
func headerOnly(f *testing.F, hdr *tar.Header) []byte {
	var buf bytes.Buffer
	if err := tar.NewWriter(&buf).WriteHeader(hdr); err != nil {
		f.Fatal(err)
	}
	return append(buf.Bytes(), make([]byte, 512)...)
}

// read returns the bytes of r.
func read(f *testing.F, r io.Reader) []byte {
	data, err := io.ReadAll(r)
	if err != nil {
		f.Fatal(err)
	}
	return data
}

func FuzzApply(f *testing.F) {
	for _, seed := range hostile(f) {
		f.Add(seed)
	}

	f.Fuzz(func(_ *testing.T, data []byte) {
		// A refusal is an outcome; the only failure is a panic, or a path the loop did not
		// promise.
		_, _ = layer.Apply("fuzz", bytes.NewReader(data), &sink{f: f}, io.Discard)
	})
}
