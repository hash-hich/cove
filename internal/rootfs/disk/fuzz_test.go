package disk_test

import (
	"bytes"
	"io"
	"testing"

	"gitlab.com/hich-hich/cove/internal/rootfs/disk"
)

// FuzzConvert holds C25 of the rootfs spec: whatever the bytes, the loop returns, it never
// panics. The seeds are the hostile corpus, the cases the acceptance criteria name plus the
// truncations of them, so that the fuzzer starts on archives that already reach every branch.
func FuzzConvert(f *testing.F) {
	deep := "a/" + "../../" + "b/../../../c"
	corpus := [][]item{
		{file("../escape", "hello")},
		{file("ok/../../ailleurs", "")},
		{file("/etc/absolu", "")},
		{file(deep, "")},
		{file(".", ""), file("", "")},
		{symlink("l", "/"), file("l/traverse", "")},
		{symlink("l", "../../.."), hardlink("h", "l")},
		{file("there", ""), hardlink("link", "elsewhere")},
		{dir("d"), file("d/.wh.x", ""), file("d/.wh..wh..opq", ""), file("d/.wh..wh.plnk", "")},
		{file(".wh.", ""), file("../.wh.x", "")},
		{withXattr(file("bin", "x"), "user.weird", string([]byte{0x00, 0xff})), withXattr(dir("d"), "bogus.x", "v")},
	}
	for _, items := range corpus {
		raw := archive(f, items...)
		f.Add(raw)
		// A layer cut short is what an interrupted download leaves; the loop meets it too.
		f.Add(raw[:len(raw)/2])
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		c := &disk.Converter{Layer: layerName, Log: io.Discard}
		_, _ = c.Convert(bytes.NewReader(raw), &recorder{})
	})
}
