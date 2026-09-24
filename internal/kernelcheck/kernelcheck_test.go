package kernelcheck_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/kernelcheck"
)

// The command lines of the two backends: libkrun gives a virtio console, Firecracker an 8250, and
// both declare their mmio devices on the command line on x86_64.
const (
	libkrunCmdline     = "console=hvc0 rdinit=/init"
	firecrackerCmdline = "console=ttyS0 reboot=k panic=1 virtio_mmio.device=4K@0xc0001000:5"
)

var goarches = []string{"amd64", "arm64"}

const (
	binfmtELF     = "CONFIG_BINFMT_ELF"
	futex         = "CONFIG_FUTEX"
	virtioConsole = "CONFIG_VIRTIO_CONSOLE"
)

// config writes a configuration that builds in every requirement of a kernel booted with cmdline
// on goarch but those in except, which are replaced by their line or dropped when it is empty,
// with the lines of extra appended, as make writes a .config.
func config(cmdline, goarch string, except map[string]string, extra ...string) string {
	lines := []string{"#", "# Automatically generated file; DO NOT EDIT.", "#"}
	for _, opt := range kernelcheck.Requirements(cmdline, goarch) {
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
		for _, cmdline := range []string{libkrunCmdline, firecrackerCmdline} {
			cfg := config(cmdline, goarch, nil, "CONFIG_BRIDGE=y", `CONFIG_LOCALVERSION="-cove"`)
			require.NoError(t, kernelcheck.CheckFor(strings.NewReader(cfg), cmdline, goarch), goarch)
			// /proc/config.gz serves the same text compressed.
			require.NoError(t, kernelcheck.CheckFor(bytes.NewReader(gzipped(t, cfg)), cmdline, goarch), goarch)
		}
	}
}

func TestCheckNamesEveryMissingOptionInTheOrderOfTheList(t *testing.T) {
	t.Parallel()

	cfg := config(libkrunCmdline, "arm64", map[string]string{
		"CONFIG_OVERLAY_FS":         "# CONFIG_OVERLAY_FS is not set",
		"CONFIG_EROFS_FS_POSIX_ACL": "",
		"CONFIG_VIRTIO_BLK":         "",
		virtioConsole:               "",
	})
	err := kernelcheck.CheckFor(bytes.NewReader(gzipped(t, cfg)), libkrunCmdline, "arm64")

	require.Equal(t, []string{
		"CONFIG_VIRTIO_BLK", "CONFIG_EROFS_FS_POSIX_ACL", "CONFIG_OVERLAY_FS", virtioConsole,
	}, missingOf(t, err))
	require.EqualError(t, err, "the kernel does not build in CONFIG_VIRTIO_BLK, CONFIG_EROFS_FS_POSIX_ACL, "+
		"CONFIG_OVERLAY_FS, CONFIG_VIRTIO_CONSOLE, which cove requires")
}

func TestCheckAsksForTheConsoleTheCommandLineNames(t *testing.T) {
	t.Parallel()

	// A kernel with a PL011 console only would pass a check that took any console, and write its
	// refusal under libkrun to a port that is not there.
	cfg := config("console=ttyAMA0", "arm64", nil)
	require.NoError(t, kernelcheck.CheckFor(strings.NewReader(cfg), "console=ttyAMA0", "arm64"))
	require.Equal(t, []string{virtioConsole},
		missingOf(t, kernelcheck.CheckFor(strings.NewReader(cfg), libkrunCmdline, "arm64")))

	// The last console named is /dev/console.
	require.NoError(t, kernelcheck.CheckFor(strings.NewReader(cfg), "console=hvc0 console=ttyAMA0", "arm64"))

	// On arm64 the 8250 of Firecracker is found in the device tree.
	require.Contains(t, kernelcheck.Requirements("console=ttyS0", "arm64"), "CONFIG_SERIAL_OF_PLATFORM")
	require.NotContains(t, kernelcheck.Requirements("console=ttyS0", "amd64"), "CONFIG_SERIAL_OF_PLATFORM")
}

func TestCheckRefusesACommandLineWithoutAKnownConsole(t *testing.T) {
	t.Parallel()

	for _, cmdline := range []string{"rdinit=/init", "console=tty0"} {
		err := kernelcheck.CheckFor(strings.NewReader(config(libkrunCmdline, "arm64", nil)), cmdline, "arm64")
		require.ErrorContains(t, err, "names no console cove knows", cmdline)
	}
}

func TestCheckAsksForCommandLineDevicesWhenTheCommandLineDeclaresThem(t *testing.T) {
	t.Parallel()

	cfg := config(libkrunCmdline, "amd64", nil, "CONFIG_SERIAL_8250_CONSOLE=y")
	require.NoError(t, kernelcheck.CheckFor(strings.NewReader(cfg), "console=ttyS0", "amd64"))
	require.Equal(t, []string{"CONFIG_VIRTIO_MMIO_CMDLINE_DEVICES"},
		missingOf(t, kernelcheck.CheckFor(strings.NewReader(cfg), firecrackerCmdline, "amd64")))
}

func TestCheckRefusesAModule(t *testing.T) {
	t.Parallel()

	// The init stacks the image before any /lib/modules exists: a module there is out of reach.
	cfg := config(libkrunCmdline, "arm64", map[string]string{"CONFIG_EROFS_FS": "CONFIG_EROFS_FS=m"})
	require.Equal(t, []string{"CONFIG_EROFS_FS"},
		missingOf(t, kernelcheck.CheckFor(strings.NewReader(cfg), libkrunCmdline, "arm64")))
}

func TestCheckDoesNotTakeAPrefixForTheOption(t *testing.T) {
	t.Parallel()

	// CONFIG_VSOCKETS_DIAG=y says nothing of CONFIG_VSOCKETS.
	cfg := config(libkrunCmdline, "arm64", map[string]string{"CONFIG_VSOCKETS": ""}, "CONFIG_VSOCKETS_DIAG=y")
	require.Equal(t, []string{"CONFIG_VSOCKETS"},
		missingOf(t, kernelcheck.CheckFor(strings.NewReader(cfg), libkrunCmdline, "arm64")))
}

func TestCheckFindsNothingInAnEmptyConfiguration(t *testing.T) {
	t.Parallel()

	missing := missingOf(t, kernelcheck.CheckFor(strings.NewReader(""), firecrackerCmdline, "amd64"))
	require.Equal(t, kernelcheck.Requirements(firecrackerCmdline, "amd64"), missing)
}

func TestCheckRefusesABrokenCompressedConfiguration(t *testing.T) {
	t.Parallel()

	broken := gzipped(t, config(libkrunCmdline, "arm64", nil))
	err := kernelcheck.CheckFor(bytes.NewReader(broken[:len(broken)/2]), libkrunCmdline, "arm64")
	require.Error(t, err)
	var missing *kernelcheck.MissingError
	require.NotErrorAs(t, err, &missing)
}

// procAt lays out under a temporary root what the guest's /proc holds for the check.
func procAt(t *testing.T, cmdline string, configGz []byte) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "proc/self"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "proc/cmdline"), []byte(cmdline+"\n"), 0o600))
	if configGz != nil {
		require.NoError(t, os.WriteFile(filepath.Join(root, "proc/config.gz"), configGz, 0o600))
	}
	return root
}

func TestCheckRunningReadsTheConfigurationAndCommandLineOfTheGuest(t *testing.T) {
	t.Parallel()

	root := procAt(t, firecrackerCmdline, gzipped(t, config(firecrackerCmdline, "amd64", nil)))
	require.NoError(t, kernelcheck.CheckRunningAt(root, "amd64"))

	root = procAt(t, firecrackerCmdline,
		gzipped(t, config(firecrackerCmdline, "amd64", map[string]string{"CONFIG_EXT4_FS": ""})))
	require.Equal(t, []string{"CONFIG_EXT4_FS"}, missingOf(t, kernelcheck.CheckRunningAt(root, "amd64")))
}

func TestCheckRunningBlamesTheRightOptionForAMissingFile(t *testing.T) {
	t.Parallel()

	// /proc is there but serves no configuration: nothing else says what the kernel carries.
	err := kernelcheck.CheckRunningAt(procAt(t, libkrunCmdline, nil), "arm64")
	require.Equal(t, []string{"CONFIG_IKCONFIG_PROC"}, missingOf(t, err))

	// No /proc at all: every file below is missing, and IKCONFIG_PROC is not the one to blame.
	err = kernelcheck.CheckRunningAt(t.TempDir(), "arm64")
	require.Equal(t, []string{"CONFIG_PROC_FS"}, missingOf(t, err))
}

func TestCheckKernelFileReadsTheConfigurationTheImageEmbeds(t *testing.T) {
	t.Parallel()

	// The layout of kernel/configs.c: the gzip stream between two markers, amid the code.
	image := func(cfg string) string {
		img := bytes.Join([][]byte{
			[]byte("\x7fELF code and data before"),
			[]byte("IKCFG_ST"), gzipped(t, cfg), []byte("IKCFG_ED"),
			[]byte("and more code after"),
		}, nil)
		path := filepath.Join(t.TempDir(), "kernel")
		require.NoError(t, os.WriteFile(path, img, 0o600))
		return path
	}

	path := image(config(libkrunCmdline, "arm64", nil))
	require.NoError(t, kernelcheck.CheckKernelFileFor(path, libkrunCmdline, "arm64"))

	// The four options the guest could never report are refused before boot.
	path = image(config(libkrunCmdline, "arm64", map[string]string{binfmtELF: "", futex: ""}))
	require.Equal(t, []string{binfmtELF, futex},
		missingOf(t, kernelcheck.CheckKernelFileFor(path, libkrunCmdline, "arm64")))
}

func TestCheckKernelFileSaysWhenTheImageShowsNoConfiguration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, content := range map[string]string{
		// Built without IKCONFIG.
		"plain": "\x7fELF code and data, no marker",
		// A compressed image: the marker is inside the compressed stream, or a marker without its
		// stream.
		"marker only": "IKCFG_ST not a gzip stream",
	} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		require.ErrorIs(t, kernelcheck.CheckKernelFileFor(path, libkrunCmdline, "arm64"),
			kernelcheck.ErrNoEmbeddedConfig, name)
	}
}

// fragmentLines returns the options a fragment of kernel/config sets to y.
func fragmentLines(t *testing.T, name string) []string {
	t.Helper()
	//nolint:gosec // G304: name is a fragment of kernel/config the tests name.
	f, err := os.Open(filepath.Join("../../kernel/config", name))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var opts []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if opt, ok := strings.CutSuffix(sc.Text(), "=y"); ok && strings.HasPrefix(opt, "CONFIG_") {
			opts = append(opts, opt)
		}
	}
	require.NoError(t, sc.Err())
	return opts
}

// The fragment kernel/config/cove.config is where cove's own kernel meets the list, with the
// architecture fragment for what one architecture adds, under the command line of each backend.
func TestCoveKernelFragmentMeetsTheList(t *testing.T) {
	t.Parallel()

	cove, err := os.ReadFile("../../kernel/config/cove.config")
	require.NoError(t, err)
	for _, goarch := range goarches {
		arch, err := os.ReadFile(filepath.Join("../../kernel/config", map[string]string{
			"amd64": "x86_64.config", "arm64": "arm64.config",
		}[goarch]))
		require.NoError(t, err)
		for _, cmdline := range []string{libkrunCmdline, firecrackerCmdline} {
			cfg := append(append([]byte(nil), cove...), arch...)
			require.NoError(t, kernelcheck.CheckFor(bytes.NewReader(cfg), cmdline, goarch), goarch+" "+cmdline)
		}
	}
}

// The other way: every line of cove.config is required, or is the parent in Kconfig of a
// required line, the menu it sits under or what it depends on. A line that is neither is a
// requirement the list does not know, or one that should live in another fragment.
func TestCoveKernelFragmentHoldsNothingButTheList(t *testing.T) {
	t.Parallel()

	parents := map[string]bool{
		"CONFIG_BLOCK":            true, // VIRTIO_BLK, BLK_DEV_INITRD.
		"CONFIG_BLK_DEV":          true, // VIRTIO_BLK.
		"CONFIG_MISC_FILESYSTEMS": true, // EROFS_FS.
		"CONFIG_NET":              true, // VSOCKETS.
		"CONFIG_NETDEVICES":       true, // VIRTIO_NET.
		"CONFIG_NET_CORE":         true, // VIRTIO_NET.
		"CONFIG_VIRTIO_MENU":      true, // VIRTIO_MMIO.
		"CONFIG_TTY":              true, // the consoles.
		"CONFIG_SERIAL_8250":      true, // SERIAL_8250_CONSOLE.
		"CONFIG_SHMEM":            true, // TMPFS.
	}
	every := kernelcheck.EveryOption()
	for _, opt := range fragmentLines(t, "cove.config") {
		require.True(t, every[opt] || parents[opt], "%s is in cove.config but neither required nor a parent", opt)
	}
}
