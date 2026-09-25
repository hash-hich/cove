// Package vmcreate makes the VM of a sandbox: it lays out the files of the sandbox on the host,
// the write disk, the initramfs of the run and the console, opens each file the VM needs, and
// hands them to cove-vmm through vmlaunch. It returns once the init of the guest says the image
// is mounted, or with what the console said when the VM ended before.
package vmcreate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"gitlab.com/hich-hich/cove/internal/emptyext4"
	"gitlab.com/hich-hich/cove/internal/erofs"
	"gitlab.com/hich-hich/cove/internal/vminit/initramfs"
	"gitlab.com/hich-hich/cove/internal/vminit/spec"
	"gitlab.com/hich-hich/cove/internal/vmlaunch"
	"gitlab.com/hich-hich/cove/internal/vmmproto"
	"gitlab.com/hich-hich/cove/internal/writedisk"
)

// The files of libexec a VM boots from, beside cove-vmm.
const (
	kernelFile = "kernel"
	initFile   = "cove-init"
)

// The files of a sandbox in its directory. The write disk and the initramfs lose their name as soon
// as cove-vmm holds them, so that nothing is left of them once it is gone, killed or not; the
// console stays, the one trace of the VM until a report is written.
const (
	writeDiskFile = "rw.ext4"
	initramfsFile = "initramfs"
	consoleFile   = "console.log"
)

// cmdline is the command line of the kernel: the console libkrun gives, the init of the initramfs,
// and a panic that ends the VM rather than leaving it hung.
const cmdline = "console=hvc0 rdinit=/init panic=-1"

// bootTimeout bounds the wait for the init to say the image is mounted.
const bootTimeout = time.Minute

// Request is the sandbox to create.
type Request struct {
	// ID names the sandbox and its directory, Name its VM, which the guest takes as hostname.
	ID, Name string
	// CPUs and MemoryMiB are the resources of the VM.
	CPUs      uint8
	MemoryMiB uint32
	// Disk is the largest the write disk may be; the free space of the host lowers it.
	Disk writedisk.Size
	// Plan is the layers of the image, the highest first.
	Plan erofs.Plan
	// Run is the description of the run, the disks aside, which Create fills from Plan and Disk.
	Run spec.Run
}

// Sandbox is a VM that booted.
type Sandbox struct {
	// Dir is the directory of the sandbox, Console the file its console is written to.
	Dir, Console string
	// WriteDisk is the size the write disk was given.
	WriteDisk writedisk.Size
	// VM is the cove-vmm that runs it.
	VM *vmlaunch.VM
}

// Create makes the sandbox of req under root, the directory of the sandboxes, with the cove-vmm,
// kernel and init of libexec, and returns once its image is mounted. A sandbox that fails is
// removed, its VM included, and the error carries what its console said.
func Create(ctx context.Context, libexec, root string, req Request) (_ *Sandbox, err error) {
	dir := filepath.Join(root, req.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the directory of the sandbox: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	free, err := writedisk.Free(dir)
	if err != nil {
		return nil, err //nolint:wrapcheck // Free names the directory.
	}
	size, err := writedisk.Nominal(req.Disk, free, writedisk.DefaultMargin)
	if err != nil {
		return nil, err //nolint:wrapcheck // Nominal names the free space and the margin.
	}
	files, err := open(libexec, dir, req, size)
	if err != nil {
		return nil, err
	}
	defer files.close()

	vm, err := vmlaunch.Start(ctx, libexec, vmlaunch.Request{
		CPUs: req.CPUs, MemoryMiB: req.MemoryMiB,
		Kernel: files.kernel, KernelFormat: kernelFormat(), Initramfs: files.initramfs,
		Cmdline: cmdline, Disks: files.disks, Console: files.console,
	})
	// cove-vmm holds its own descriptors, or is gone: the names go either way.
	_ = os.Remove(filepath.Join(dir, writeDiskFile))
	_ = os.Remove(filepath.Join(dir, initramfsFile))
	console := filepath.Join(dir, consoleFile)
	if err != nil {
		return nil, withConsole(err, console)
	}
	if err := waitReady(ctx, vm, console); err != nil {
		vm.Kill()
		return nil, withConsole(err, console)
	}
	return &Sandbox{Dir: dir, Console: console, WriteDisk: size, VM: vm}, nil
}

// files are the files of a VM, open.
type files struct {
	kernel, initramfs, console *os.File
	disks                      []vmlaunch.Disk
}

// open writes the files of the sandbox in dir and opens every file the VM needs.
func open(libexec, dir string, req Request, size writedisk.Size) (_ *files, err error) {
	f := &files{}
	defer func() {
		if err != nil {
			f.close()
		}
	}()
	for _, l := range req.Plan.Layers {
		d, err := os.Open(l.Path)
		if err != nil {
			return nil, fmt.Errorf("open the disk of layer %s: %w", l.DiffID, err)
		}
		f.disks = append(f.disks, vmlaunch.Disk{File: d, ReadOnly: true})
	}
	disk := filepath.Join(dir, writeDiskFile)
	if err := emptyext4.Write(disk, int64(size)); err != nil {
		return nil, err //nolint:wrapcheck // Write names the disk.
	}
	//nolint:gosec // G304: the disk was just written in the directory of the sandbox.
	w, err := os.OpenFile(disk, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open the write disk: %w", err)
	}
	f.disks = append(f.disks, vmlaunch.Disk{File: w})

	run, err := describe(f.disks, req, size)
	if err != nil {
		return nil, err
	}
	if f.initramfs, err = writeInitramfs(filepath.Join(libexec, initFile), filepath.Join(dir, initramfsFile),
		run); err != nil {
		return nil, err
	}
	//nolint:gosec // G304: the kernel of libexec, beside cove-vmm.
	if f.kernel, err = os.Open(filepath.Join(libexec, kernelFile)); err != nil {
		return nil, fmt.Errorf("open the kernel: %w", err)
	}
	//nolint:gosec // G304: the console of the sandbox, in its directory.
	if f.console, err = os.OpenFile(filepath.Join(dir, consoleFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600); err != nil {
		return nil, fmt.Errorf("create the console: %w", err)
	}
	return f, nil
}

func (f *files) close() {
	for _, c := range []*os.File{f.kernel, f.initramfs, f.console} {
		if c != nil {
			_ = c.Close()
		}
	}
	for _, d := range f.disks {
		_ = d.File.Close()
	}
}

// describe returns the description of the run with its disks: the layers of the plan, sized by
// the files opened for them, and the write disk.
func describe(disks []vmlaunch.Disk, req Request, size writedisk.Size) (*spec.Run, error) {
	run := req.Run
	run.Hostname = req.Name
	run.MountOptions = slices.Clone(req.Plan.MountOptions)
	run.Layers = make([]spec.Layer, len(req.Plan.Layers))
	for i, l := range req.Plan.Layers {
		fi, err := disks[i].File.Stat()
		if err != nil {
			return nil, fmt.Errorf("size the disk of layer %s: %w", l.DiffID, err)
		}
		run.Layers[i] = spec.Layer{Disk: spec.Disk{Size: fi.Size()}, DiffID: l.DiffID, Mountpoint: l.Mountpoint}
	}
	run.Write = spec.Disk{Size: int64(size)}
	return &run, nil
}

// writeInitramfs writes at path the initramfs of run, the init read from init, and returns it open
// for reading.
func writeInitramfs(init, path string, run *spec.Run) (*os.File, error) {
	//nolint:gosec // G304: the init of libexec, beside cove-vmm.
	bin, err := os.ReadFile(init)
	if err != nil {
		return nil, fmt.Errorf("read the init: %w", err)
	}
	var buf bytes.Buffer
	if err := initramfs.WriteBase(&buf, bin); err != nil {
		return nil, err //nolint:wrapcheck // The archive names what it could not write.
	}
	if err := initramfs.WriteRun(&buf, run); err != nil {
		return nil, err //nolint:wrapcheck // The archive names what it could not write.
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("write the initramfs: %w", err)
	}
	//nolint:gosec // G304: the initramfs just written in the directory of the sandbox.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open the initramfs: %w", err)
	}
	return f, nil
}

// kernelFormat returns the format of the kernel make builds for the host: the raw Image on arm64,
// the ELF vmlinux on x86_64.
func kernelFormat() vmmproto.KernelFormat {
	if runtime.GOARCH == "arm64" {
		return vmmproto.KernelRaw
	}
	return vmmproto.KernelELF
}

// errEnded reports a VM that ended before its init said it was ready.
var errEnded = errors.New("the VM ended before its image was mounted")
