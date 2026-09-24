package kernelcheck

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
)

// ErrNoEmbeddedConfig says a file of a kernel shows no configuration in the clear: the kernel
// was built without IKCONFIG, or its image is compressed. The check then falls to the guest.
var ErrNoEmbeddedConfig = errors.New("the kernel file shows no embedded configuration")

// ikconfigStart opens the configuration IKCONFIG embeds in a kernel, the gzip stream
// /proc/config.gz serves, between the markers IKCFG_ST and IKCFG_ED (kernel/configs.c). The file
// cove boots is uncompressed on both architectures, the raw Image on arm64 and the ELF vmlinux on
// x86_64, so the stream is there as it is.
var ikconfigStart = []byte("IKCFG_ST")

// CheckKernelFile checks the kernel in the file at path, as it will be booted with cmdline, before
// the VM exists: every option it lacks is refused by name, those the guest could never report
// included, since without them the init does not run. It returns ErrNoEmbeddedConfig when the file
// shows no configuration, and a *MissingError when the kernel lacks what cove requires.
func CheckKernelFile(path, cmdline string) error {
	return checkImage(path, cmdline, runtime.GOARCH)
}

func checkImage(path, cmdline, goarch string) error {
	//nolint:gosec // G304: path is the kernel cove boots, its own or the one the user names.
	img, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the kernel: %w", err)
	}
	start := bytes.Index(img, ikconfigStart)
	if start < 0 {
		return ErrNoEmbeddedConfig
	}
	stream := img[start+len(ikconfigStart):]
	if !bytes.HasPrefix(stream, gzipMagic) {
		return ErrNoEmbeddedConfig
	}
	return checkArch(bytes.NewReader(stream), cmdline, goarch)
}
