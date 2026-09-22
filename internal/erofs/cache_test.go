package erofs_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/erofs"
	"gitlab.com/hich-hich/cove/internal/filelock"
)

func TestSweepsWhatAConversionThatWasKilledLeftBehind(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	c := opened(t, root)
	tmp := filepath.Join(c.Root(), "tmp")
	killed := filepath.Join(tmp, "abcdef.4242")
	require.NoError(t, os.MkdirAll(killed, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(killed, "layer.erofs"), []byte("half"), 0o600))
	spool := filepath.Join(tmp, "erofs-mkfs-123456")
	require.NoError(t, os.WriteFile(spool, []byte("half"), 0o600))
	lock := filepath.Join(tmp, "abcdef.lock")
	require.NoError(t, os.WriteFile(lock, nil, 0o600))

	_, err := erofs.Open(root)

	require.NoError(t, err)
	require.NoDirExists(t, killed, "a conversion nobody holds has no value: it starts again from the beginning")
	require.NoFileExists(t, spool, "a spool its process did not get to unlink is swept too")
	require.FileExists(t, lock, "a lock file removed would let two processes lock two inodes of one name")
}

func TestLeavesAloneTheTemporaryOfAConversionThatIsRunning(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	c := opened(t, root)
	tmp := filepath.Join(c.Root(), "tmp")
	running := filepath.Join(tmp, "abcdef.4242")
	require.NoError(t, os.MkdirAll(running, 0o750))
	held, ok, err := filelock.Try(filepath.Join(tmp, "abcdef.lock"))
	require.NoError(t, err)
	require.True(t, ok)
	t.Cleanup(held.Release)

	_, err = erofs.Open(root)

	require.NoError(t, err)
	require.DirExists(t, running, "a sweep cannot take away the work of a conversion in progress")
}

func TestTwoConversionsOfOneLayerFileItOnce(t *testing.T) {
	t.Parallel()

	c := opened(t)
	l := layerOf(t, file("a", "x"))
	got := make([]erofs.Blob, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup

	for i := range got {
		wg.Go(func() { got[i], errs[i] = c.Convert(t.Context(), l, nil, nil) })
	}
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, got[0].Path, got[1].Path)
	require.Equal(t, got[0].Meta.BlobSHA256, got[1].Meta.BlobSHA256)
	require.NotEqual(t, got[0].Converted, got[1].Converted, "one converted the layer, the other was served it")
	filed := left(t, c)
	require.Len(t, filed, 3, "one directory, the blob and its meta: %v", filed)
}

func TestKeepsTheBlobsOfAWriterOfAnotherVersionApart(t *testing.T) {
	t.Parallel()

	c := opened(t)

	require.Equal(t, "v1", filepath.Base(c.Root()),
		"the version is a directory of its own, so a writer that would produce other bytes starts beside it")
	require.True(t, strings.HasSuffix(filepath.Dir(c.Root()), "rootfs"),
		"the blobs live beside the store of the images, not inside its layout")
}
