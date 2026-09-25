//go:build cgo && (darwin || linux)

// Command cove-vmm runs the virtual machine monitor of one VM of cove, and nothing else. cove starts
// it with the files of the VM already open, as vmmproto describes, and it holds no right to open
// anything else: a guest that escapes the monitor lands here, in a process that has nothing of the
// user to give.
//
// It links libkrun, whose sha256 make records in it; a library that does not match is refused
// before a word is said to cove. It is built by make cove-vmm alone, which signs it with the right
// to create VMs that cove itself does not hold.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"gitlab.com/hich-hich/cove/internal/confine"
	"gitlab.com/hich-hich/cove/internal/krun"
	"gitlab.com/hich-hich/cove/internal/vmmproto"
)

// libkrunVersion and libkrunSHA256 are set by the linker from third_party/libkrun.lock and from
// the library the build produced.
var (
	libkrunVersion string
	libkrunSHA256  string
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cove-vmm: %v\n", err)
		os.Exit(1)
	}
}

// run returns only when the VM could not be started: once it starts, libkrun owns the process and
// exits it when the guest powers off.
func run() error {
	pipe := os.NewFile(vmmproto.Pipe, "pipe")
	if pipe == nil {
		return errors.New("started without the pipe of cove")
	}
	sum, err := librarySHA256()
	if err != nil {
		return err
	}
	if err := vmmproto.Send(pipe, vmmproto.Hello{
		Build: vmmproto.Build(), VMM: "libkrun " + libkrunVersion, LibrarySHA256: sum,
	}); err != nil {
		return err //nolint:wrapcheck // Send names the message.
	}
	var boot vmmproto.Boot
	if err := vmmproto.NewReceiver(pipe).Receive(&boot); err != nil {
		return err //nolint:wrapcheck // Receive names the message.
	}
	ctx, err := configure(boot)
	if err != nil {
		return answer(pipe, err)
	}
	if err := answer(pipe, nil); err != nil {
		return err
	}
	_ = pipe.Close()
	return ctx.StartEnter() //nolint:wrapcheck // StartEnter names libkrun and what failed.
}

// librarySHA256 hashes the libkrun the dynamic linker loaded and refuses it unless it is the one
// this binary was built with. It runs before the sandbox, which lets no file be read by its path.
func librarySHA256() (string, error) {
	path, err := krun.LibraryPath()
	if err != nil {
		return "", err //nolint:wrapcheck // LibraryPath says what is missing.
	}
	//nolint:gosec // G304: the path is the one the dynamic linker loaded libkrun from.
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read the libkrun it loaded: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read the libkrun it loaded: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != libkrunSHA256 {
		return "", fmt.Errorf("loaded %s, whose sha256 %s is not the %s it was built with", path, sum, libkrunSHA256)
	}
	return sum, nil
}

// configure enters the sandbox, then gives libkrun the VM of boot. Every file is named by
// /dev/fd, the only path the sandbox lets through.
func configure(boot vmmproto.Boot) (*krun.Ctx, error) {
	if err := confine.Enter(); err != nil {
		return nil, err //nolint:wrapcheck // Enter names the sandbox.
	}
	format := krun.FormatRaw
	if boot.KernelFormat == vmmproto.KernelELF {
		format = krun.FormatELF
	}
	ctx, err := krun.New()
	if err != nil {
		return nil, err //nolint:wrapcheck // krun names libkrun and the call.
	}
	if err := ctx.SetResources(boot.CPUs, boot.MemoryMiB); err != nil {
		return nil, err //nolint:wrapcheck // krun names libkrun and the call.
	}
	if err := ctx.SetKernel(fdPath(boot.Kernel), format, fdPath(boot.Initramfs), boot.Cmdline); err != nil {
		return nil, err //nolint:wrapcheck // krun names libkrun and the call.
	}
	for i, d := range boot.Disks {
		if err := ctx.AddDisk("d"+strconv.Itoa(i), fdPath(d.FD), d.ReadOnly); err != nil {
			return nil, err //nolint:wrapcheck // krun names libkrun and the disk.
		}
	}
	if err := ctx.SetConsoleOutput(fdPath(boot.Console)); err != nil {
		return nil, err //nolint:wrapcheck // krun names libkrun and the call.
	}
	return ctx, nil
}

// answer sends cove the Status of err, and returns err, or the failure to send it.
func answer(pipe io.Writer, err error) error {
	var s vmmproto.Status
	if err != nil {
		s.Error = err.Error()
	}
	if sendErr := vmmproto.Send(pipe, s); sendErr != nil {
		return errors.Join(err, sendErr)
	}
	return err
}

func fdPath(fd int) string {
	return "/dev/fd/" + strconv.Itoa(fd)
}
