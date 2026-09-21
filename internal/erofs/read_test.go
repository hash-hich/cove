package erofs_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	goerofs "github.com/erofs/go-erofs"
	"github.com/stretchr/testify/require"
)

// mounted opens the blob at path as the guest would read it, the tests standing in for the
// kernel: an assertion on the bytes of a blob is worth nothing unless it reads them back.
func mounted(t testing.TB, path string) fs.FS {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // G304: the path comes from the cache the test just wrote.
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	img, err := goerofs.Open(f)
	require.NoError(t, err)
	return img
}

// stat returns what the blob holds at name, which is absolute as the loop writes it.
func stat(t testing.TB, img fs.FS, name string) *goerofs.Stat {
	t.Helper()
	info, err := fs.Stat(img, strings.TrimPrefix(name, "/"))
	require.NoError(t, err)
	sys, ok := info.Sys().(*goerofs.Stat)
	require.True(t, ok, "the reader gave no inode for %s", name)
	return sys
}

// names returns the entries of the directory name, sorted as the reader gives them.
func names(t testing.TB, img fs.FS, name string) []string {
	t.Helper()
	entries, err := fs.ReadDir(img, strings.TrimPrefix(name, "/"))
	require.NoError(t, err)
	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name())
	}
	return got
}
