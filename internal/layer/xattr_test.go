package layer_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/layer"
)

func TestPassesExtendedAttributesRaw(t *testing.T) {
	t.Parallel()

	raw := "a\x00b\xff\xfe"
	capability := "\x01\x00\x00\x02\x00\x00\x00\x00"
	rec, counts, log := applyAll(t,
		xattrs(file("f", ""), map[string]string{"user.raw": raw, "security.capability": capability}),
		xattrs(symlink("l", "f"), map[string]string{"trusted.overlay.redirect": "/f"}),
		xattrs(link("h", "f"), map[string]string{"user.linked": "1"}),
		xattrs(dir("d"), map[string]string{"system.posix_acl_access": "\x02\x00\x00\x00"}),
	)

	require.Equal(t, []string{
		"file /f -rw-r--r-- 0:0 0",
		fmt.Sprintf("setxattr /f security.capability=%q", capability),
		fmt.Sprintf("setxattr /f user.raw=%q", raw),
		"symlink /l Lrwxrwxrwx 0:0 f", `setxattr /l trusted.overlay.redirect="/f"`,
		"link /h /f", `setxattr /h user.linked="1"`,
		"mkdir /d drwxr-xr-x 0:0", `setxattr /d system.posix_acl_access="\x02\x00\x00\x00"`,
	}, rec.calls)
	require.Equal(t, layer.Counts{}, counts)
	require.Empty(t, log)
}

func TestCountsAnAttributeErofsWillNotRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		count int
	}{
		{name: "user.x"},
		{name: "trusted.x"},
		{name: "security.x"},
		{name: "system.posix_acl_access"},
		{name: "system.posix_acl_default"},
		{name: "system.posix_acl_accessx", count: 1},
		{name: "system.nfs4_acl", count: 1},
		{name: "btrfs.compression", count: 1},
		{name: "user", count: 1},
		{name: "", count: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec, counts, log := applyAll(t, xattrs(file("f", ""), map[string]string{tt.name: "v"}))

			// The attribute is written whatever its name: the blob says what the archive said.
			require.Equal(t, []string{"file /f -rw-r--r-- 0:0 0", "setxattr /f " + tt.name + `="v"`}, rec.calls)
			require.Equal(t, layer.Counts{UnknownXattrPrefixes: tt.count}, counts)
			if tt.count == 0 {
				require.Empty(t, log)
				return
			}
			require.Equal(t, "layer "+id+`: entry "f": attribute `+fmt.Sprintf("%q", tt.name)+
				" is written but will not be readable in the guest: EROFS reads user., trusted., security. and the "+
				"two POSIX ACL attributes only\n", log)
		})
	}
}
