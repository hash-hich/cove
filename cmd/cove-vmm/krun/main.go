//go:build cgo && (darwin || linux)

// Command cove-vmm-krun is the cove-vmm that runs a VM with libkrun, and nothing else. cove names
// the files of the VM, as vmmproto describes, and it holds no right to open any other: a guest that
// escapes the monitor lands here, in a process that has nothing of the user to give.
//
// It links libkrun, whose sha256 make records in it; a library that does not match is refused
// before a word is said to cove. It is built by make cove-vmm alone, which signs it with the right
// to create VMs that cove itself does not hold.
package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"gitlab.com/hich-hich/cove/cmd/cove-vmm/krun/internal/libkrun"
	"gitlab.com/hich-hich/cove/cmd/internal/confine"
	"gitlab.com/hich-hich/cove/internal/vmm/vmmproto"
)

// profile is the Seatbelt profile of cove-vmm-krun: everything denied, then only what libkrun was
// seen to need, each rule with the reason it is there.
//
//go:embed vmm.sb
var profile string

// libkrunVersion and libkrunSHA256 are set by the linker from third_party/libkrun.lock and from
// the library the build produced.
var (
	libkrunVersion string
	libkrunSHA256  string
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cove-vmm-krun: %v\n", err)
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
	ctx, err := configure(boot.VM)
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
// this binary was built with. It runs before the sandbox, which lets no file but those of the VM
// be read.
func librarySHA256() (string, error) {
	path, err := libkrun.LibraryPath()
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

// configure enters the sandbox, confined to the files of vm, then gives libkrun vm.
func configure(vm vmmproto.VM) (*libkrun.Ctx, error) {
	if err := enterSocketDir(vm.Vsock); err != nil {
		return nil, err
	}
	files := vm.Files()
	paths := confine.Paths{Read: files.Read, Write: files.Write, Listen: files.Listen}
	if err := confine.Enter(profile, paths); err != nil {
		return nil, err //nolint:wrapcheck // Enter names the sandbox.
	}
	format := libkrun.FormatRaw
	if vm.KernelFormat == vmmproto.KernelELF {
		format = libkrun.FormatELF
	}
	ctx, err := libkrun.New()
	if err != nil {
		return nil, err //nolint:wrapcheck // The binding names libkrun and the call.
	}
	if err := ctx.SetResources(vm.CPUs, vm.MemoryMiB); err != nil {
		return nil, err //nolint:wrapcheck // The binding names libkrun and the call.
	}
	if err := ctx.SetKernel(vm.Kernel, format, vm.Initramfs, vm.Cmdline); err != nil {
		return nil, err //nolint:wrapcheck // The binding names libkrun and the call.
	}
	if err := attach(ctx, vm); err != nil {
		return nil, err
	}
	return ctx, nil
}

// attach gives libkrun the devices of vm: its disks, its console and its vsock.
func attach(ctx *libkrun.Ctx, vm vmmproto.VM) error {
	for i, d := range vm.Disks {
		if err := ctx.AddDisk("d"+strconv.Itoa(i), d.Path, d.ReadOnly); err != nil {
			return err //nolint:wrapcheck // The binding names libkrun and the disk.
		}
	}
	if err := ctx.SetConsoleOutput(vm.Console); err != nil {
		return err //nolint:wrapcheck // The binding names libkrun and the call.
	}
	return addVsock(ctx, vm.Vsock)
}

// enterSocketDir makes the directory of the sockets of ports the working directory. A Unix socket
// is bound by a path of 104 bytes at most on macOS, and the directory of a sandbox is longer: the
// sockets are bound by their names, relative to it. The sandbox still matches their whole paths.
func enterSocketDir(ports []vmmproto.VsockPort) error {
	if len(ports) == 0 {
		return nil
	}
	dir := filepath.Dir(ports[0].Socket)
	for _, p := range ports[1:] {
		if filepath.Dir(p.Socket) != dir {
			return fmt.Errorf("the sockets of the VM are in %s and %s, not in one directory", dir, filepath.Dir(p.Socket))
		}
	}
	if err := os.Chdir(dir); err != nil {
		return fmt.Errorf("enter the directory of the sockets: %w", err)
	}
	return nil
}

// addVsock attaches the vsock device and a listener for each of ports, by the name of its socket
// in the working directory.
func addVsock(ctx *libkrun.Ctx, ports []vmmproto.VsockPort) error {
	if len(ports) == 0 {
		return nil
	}
	if err := ctx.AddVsock(); err != nil {
		return err //nolint:wrapcheck // The binding names libkrun and the call.
	}
	for _, p := range ports {
		if err := ctx.AddVsockListener(p.Port, filepath.Base(p.Socket)); err != nil {
			return err //nolint:wrapcheck // The binding names libkrun and the port.
		}
	}
	return nil
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
