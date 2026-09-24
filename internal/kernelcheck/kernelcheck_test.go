package kernelcheck_test

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/kernelcheck"
)

var goarches = []string{"amd64", "arm64"}

// config writes a configuration that builds in the first option of every requirement of goarch
// but those in except, which are replaced by their line or dropped when it is empty, with the
// lines of extra appended, as make writes a .config.
func config(goarch string, except map[string]string, extra ...string) string {
	lines := []string{"#", "# Automatically generated file; DO NOT EDIT.", "#"}
	for _, opt := range kernelcheck.Requirements(goarch) {
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

func missingOf(t *testing.T, err error) []string {
	t.Helper()
	var missing *kernelcheck.MissingError
	require.ErrorAs(t, err, &missing)
	return missing.Options
}

func TestCheckAcceptsAKernelThatMeetsEveryRequirement(t *testing.T) {
	t.Parallel()

	for _, goarch := range goarches {
		cfg := config(goarch, nil, "CONFIG_BRIDGE=y", `CONFIG_LOCALVERSION="-cove"`)
		require.NoError(t, kernelcheck.CheckFor(strings.NewReader(cfg), goarch), goarch)
		// /proc/config.gz serves the same text compressed.
		require.NoError(t, kernelcheck.CheckFor(bytes.NewReader(gzipped(t, cfg)), goarch), goarch)
	}
}

func TestCheckNamesEveryMissingRequirementInTheOrderOfTheList(t *testing.T) {
	t.Parallel()

	cfg := config("arm64", map[string]string{
		"CONFIG_OVERLAY_FS":         "# CONFIG_OVERLAY_FS is not set",
		"CONFIG_EROFS_FS_POSIX_ACL": "",
		"CONFIG_VIRTIO_BLK":         "",
		"CONFIG_VIRTIO_MMIO":        "",
	})
	err := kernelcheck.CheckFor(bytes.NewReader(gzipped(t, cfg)), "arm64")

	require.Equal(t, []string{
		"CONFIG_VIRTIO_BLK", "CONFIG_EROFS_FS_POSIX_ACL", "CONFIG_OVERLAY_FS",
		"CONFIG_VIRTIO_MMIO or CONFIG_VIRTIO_PCI",
	}, missingOf(t, err))
	require.EqualError(t, err, "the kernel does not build in CONFIG_VIRTIO_BLK, CONFIG_EROFS_FS_POSIX_ACL, "+
		"CONFIG_OVERLAY_FS, CONFIG_VIRTIO_MMIO or CONFIG_VIRTIO_PCI, which cove requires")
}

func TestCheckTakesAnyAlternativeOfARequirement(t *testing.T) {
	t.Parallel()

	// A kernel with virtio over PCI only, a serial console only, and sysfs without devtmpfs.
	cfg := config("arm64", map[string]string{
		"CONFIG_VIRTIO_MMIO":    "CONFIG_VIRTIO_PCI=y",
		"CONFIG_VIRTIO_CONSOLE": "CONFIG_SERIAL_AMBA_PL011_CONSOLE=y",
		"CONFIG_DEVTMPFS":       "CONFIG_SYSFS=y",
	})
	require.NoError(t, kernelcheck.CheckFor(strings.NewReader(cfg), "arm64"))
}

func TestCheckAsksForTheCommandLineDevicesOnX86Only(t *testing.T) {
	t.Parallel()

	// libkrun and Firecracker declare their mmio devices on the command line on x86_64, and in a
	// device tree on arm64.
	cfg := config("arm64", nil)
	require.NoError(t, kernelcheck.CheckFor(strings.NewReader(cfg), "arm64"))
	require.Equal(t, []string{"CONFIG_VIRTIO_MMIO_CMDLINE_DEVICES"},
		missingOf(t, kernelcheck.CheckFor(strings.NewReader(cfg), "amd64")))
}

func TestCheckRefusesAModule(t *testing.T) {
	t.Parallel()

	// The init stacks the image before any /lib/modules exists: a module there is out of reach.
	cfg := config("arm64", map[string]string{"CONFIG_EROFS_FS": "CONFIG_EROFS_FS=m"})
	require.Equal(t, []string{"CONFIG_EROFS_FS"}, missingOf(t, kernelcheck.CheckFor(strings.NewReader(cfg), "arm64")))
}

func TestCheckDoesNotTakeAPrefixForTheOption(t *testing.T) {
	t.Parallel()

	// CONFIG_VSOCKETS_DIAG=y says nothing of CONFIG_VSOCKETS.
	cfg := config("arm64", map[string]string{"CONFIG_VSOCKETS": ""}, "CONFIG_VSOCKETS_DIAG=y")
	require.Equal(t, []string{"CONFIG_VSOCKETS"}, missingOf(t, kernelcheck.CheckFor(strings.NewReader(cfg), "arm64")))
}

func TestCheckFindsNothingInAnEmptyConfiguration(t *testing.T) {
	t.Parallel()

	missing := missingOf(t, kernelcheck.CheckFor(strings.NewReader(""), "amd64"))
	require.Len(t, missing, len(kernelcheck.Requirements("amd64")))
}

func TestCheckRefusesABrokenCompressedConfiguration(t *testing.T) {
	t.Parallel()

	broken := gzipped(t, config("arm64", nil))
	err := kernelcheck.CheckFor(bytes.NewReader(broken[:len(broken)/2]), "arm64")
	require.Error(t, err)
	var missing *kernelcheck.MissingError
	require.NotErrorAs(t, err, &missing)
}

func TestCheckFileRefusesAKernelThatDoesNotServeItsConfiguration(t *testing.T) {
	t.Parallel()

	// Nothing else says what the kernel carries: its absence is IKCONFIG_PROC missing.
	err := kernelcheck.CheckFileFor(filepath.Join(t.TempDir(), "config.gz"), "arm64")
	require.Equal(t, []string{"CONFIG_IKCONFIG_PROC"}, missingOf(t, err))
	require.EqualError(t, err, "the kernel does not build in CONFIG_IKCONFIG_PROC, which cove requires")
}

func TestCheckFileReadsTheConfigurationItServes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.gz")
	require.NoError(t, os.WriteFile(path, gzipped(t, config("amd64", nil)), 0o600))
	require.NoError(t, kernelcheck.CheckFileFor(path, "amd64"))

	require.NoError(t, os.WriteFile(path, gzipped(t, config("amd64", map[string]string{"CONFIG_EXT4_FS": ""})), 0o600))
	require.Equal(t, []string{"CONFIG_EXT4_FS"}, missingOf(t, kernelcheck.CheckFileFor(path, "amd64")))
}

// The fragment kernel/config/cove.config is where cove's own kernel meets the list, on its own:
// the other fragments are checked by no one, so the list and the fragment cannot drift apart.
func TestCoveKernelFragmentMeetsTheList(t *testing.T) {
	t.Parallel()

	fragment, err := os.ReadFile("../../kernel/config/cove.config")
	require.NoError(t, err)
	for _, goarch := range goarches {
		require.NoError(t, kernelcheck.CheckFor(bytes.NewReader(fragment), goarch), goarch)
	}
}
