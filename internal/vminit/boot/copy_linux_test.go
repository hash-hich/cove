package boot_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/internal/vminit/boot"
)

func TestAVolumeIsFilledWithWhatTheImageHoldsAsItHoldsIt(t *testing.T) {
	t.Parallel()
	src, dst := filepath.Join(t.TempDir(), "src"), filepath.Join(t.TempDir(), "volume")
	date := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, os.MkdirAll(filepath.Join(src, "sub/deeper"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(src, "sub/file"), []byte("data"), 0o600))
	require.NoError(t, unix.Chmod(filepath.Join(src, "sub/file"), 0o4750))
	require.NoError(t, os.Link(filepath.Join(src, "sub/file"), filepath.Join(src, "hard")))
	require.NoError(t, os.Symlink("/etc/outside", filepath.Join(src, "link")))
	require.NoError(t, unix.Mkfifo(filepath.Join(src, "fifo"), 0o600))
	for _, p := range []string{"sub/file", "sub/deeper", "sub", "."} {
		require.NoError(t, os.Chtimes(filepath.Join(src, p), date, date))
	}

	require.NoError(t, boot.CopyTree(src, dst))

	var file, hard unix.Stat_t
	require.NoError(t, unix.Lstat(filepath.Join(dst, "sub/file"), &file))
	require.NoError(t, unix.Lstat(filepath.Join(dst, "hard"), &hard))
	require.Equal(t, file.Ino, hard.Ino, "a hard link stays one file")
	require.Equal(t, uint32(0o4750), file.Mode&0o7777, "the set-id bits survive the change of owner")
	data, err := os.ReadFile(filepath.Join(dst, "sub/file")) //nolint:gosec // G304: a file the test copied.
	require.NoError(t, err)
	require.Equal(t, "data", string(data))

	target, err := os.Readlink(filepath.Join(dst, "link"))
	require.NoError(t, err)
	require.Equal(t, "/etc/outside", target, "a link is copied, never followed")

	fi, err := os.Lstat(filepath.Join(dst, "fifo"))
	require.NoError(t, err)
	require.Equal(t, os.ModeNamedPipe, fi.Mode().Type())

	for _, p := range []string{"sub/file", "sub/deeper", "sub", "."} {
		fi, err := os.Stat(filepath.Join(dst, p))
		require.NoError(t, err)
		require.True(t, date.Equal(fi.ModTime()), "%s is dated %s", p, fi.ModTime())
	}
	fi, err = os.Stat(filepath.Join(dst, "sub"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o750), fi.Mode().Perm())
}
