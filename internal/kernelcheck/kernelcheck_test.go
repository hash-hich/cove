package kernelcheck_test

import (
	"bytes"
	"compress/gzip"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/kernelcheck"
)

// config writes a configuration that builds in every required option but those in except, with
// the lines of extra appended, as make writes a .config.
func config(except map[string]string, extra ...string) string {
	lines := []string{"#", "# Automatically generated file; DO NOT EDIT.", "#"}
	for _, opt := range kernelcheck.Required {
		line, ok := except[opt]
		switch {
		case !ok:
			lines = append(lines, opt+"=y")
		case line != "":
			lines = append(lines, line)
		}
	}
	lines = append(lines, extra...)
	return strings.Join(lines, "\n") + "\n"
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write([]byte(s))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func TestCheckAcceptsAKernelThatBuildsEverythingIn(t *testing.T) {
	t.Parallel()

	cfg := config(nil, "CONFIG_BRIDGE=y", `CONFIG_LOCALVERSION="-cove"`)
	require.NoError(t, kernelcheck.Check(strings.NewReader(cfg)))
	// /proc/config.gz serves the same text compressed.
	require.NoError(t, kernelcheck.Check(bytes.NewReader(gzipped(t, cfg))))
}

func TestCheckNamesEveryMissingOptionInTheOrderOfTheList(t *testing.T) {
	t.Parallel()

	cfg := config(map[string]string{
		"CONFIG_OVERLAY_FS":         "# CONFIG_OVERLAY_FS is not set",
		"CONFIG_EROFS_FS_POSIX_ACL": "",
		"CONFIG_VIRTIO_BLK":         "",
	})
	err := kernelcheck.Check(bytes.NewReader(gzipped(t, cfg)))

	var missing *kernelcheck.MissingError
	require.ErrorAs(t, err, &missing)
	require.Equal(t, []string{"CONFIG_VIRTIO_BLK", "CONFIG_EROFS_FS_POSIX_ACL", "CONFIG_OVERLAY_FS"}, missing.Options)
	require.EqualError(t, err, "the kernel does not build in CONFIG_VIRTIO_BLK, CONFIG_EROFS_FS_POSIX_ACL, "+
		"CONFIG_OVERLAY_FS, which cove requires")
}

func TestCheckRefusesAModule(t *testing.T) {
	t.Parallel()

	// The init stacks the image before any /lib/modules exists: a module there is out of reach.
	err := kernelcheck.Check(strings.NewReader(config(map[string]string{"CONFIG_EROFS_FS": "CONFIG_EROFS_FS=m"})))

	var missing *kernelcheck.MissingError
	require.ErrorAs(t, err, &missing)
	require.Equal(t, []string{"CONFIG_EROFS_FS"}, missing.Options)
}

func TestCheckDoesNotTakeAPrefixForTheOption(t *testing.T) {
	t.Parallel()

	// CONFIG_VSOCKETS_DIAG=y says nothing of CONFIG_VSOCKETS.
	cfg := config(map[string]string{"CONFIG_VSOCKETS": ""}, "CONFIG_VSOCKETS_DIAG=y")
	var missing *kernelcheck.MissingError
	require.ErrorAs(t, kernelcheck.Check(strings.NewReader(cfg)), &missing)
	require.Equal(t, []string{"CONFIG_VSOCKETS"}, missing.Options)
}

func TestCheckFindsNothingInAnEmptyConfiguration(t *testing.T) {
	t.Parallel()

	var missing *kernelcheck.MissingError
	require.ErrorAs(t, kernelcheck.Check(strings.NewReader("")), &missing)
	require.Equal(t, kernelcheck.Required, missing.Options)
}

func TestCheckRefusesABrokenCompressedConfiguration(t *testing.T) {
	t.Parallel()

	broken := gzipped(t, config(nil))
	err := kernelcheck.Check(bytes.NewReader(broken[:len(broken)/2]))
	require.Error(t, err)
	var missing *kernelcheck.MissingError
	require.NotErrorAs(t, err, &missing)
}

// The fragment kernel/config/cove.config is where cove's own kernel meets the list: every
// requirement there, and the list and the fragment cannot drift apart.
func TestCoveKernelFragmentMeetsTheList(t *testing.T) {
	t.Parallel()

	fragment, err := os.ReadFile("../../kernel/config/cove.config")
	require.NoError(t, err)
	require.NoError(t, kernelcheck.Check(bytes.NewReader(fragment)))
}
