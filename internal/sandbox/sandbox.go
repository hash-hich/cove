// Package sandbox creates a sandbox: it lays out the files of the sandbox on the host, the write
// disk, the initramfs of the run and the console, and hands cove-vmm the paths of the files the VM
// needs through vmmlaunch. It returns once the init of the guest says the image is mounted, or
// with what the console said when the VM ended before.
//
// It assembles the packages that do each job, in order, and does none of that work itself.
package sandbox

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

	"gitlab.com/hich-hich/cove/internal/erofs"
	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/rwdisk"
	"gitlab.com/hich-hich/cove/internal/vminit/initramfs"
	"gitlab.com/hich-hich/cove/internal/vminit/spec"
	"gitlab.com/hich-hich/cove/internal/vmm/vmmlaunch"
	"gitlab.com/hich-hich/cove/internal/vmm/vmmproto"
)

// The files of libexec a VM boots from, beside cove-vmm.
const (
	kernelFile = "kernel"
	initFile   = "cove-init"
)

// The files of a sandbox in its directory. They stay once the VM stopped, for the sandbox to start
// again: only the verb that removes it removes them. The console and the log of cove-vmm are the
// traces of the VM until a report is written.
const (
	writeDiskFile = "rw.ext4"
	initramfsFile = "initramfs"
	consoleFile   = "console.log"
	vmmLogFile    = "vmm.log"
	// removeFile marks a sandbox that goes when its VM is stopped, as run --rm asks.
	removeFile = "remove"
)

// cmdline is the command line of the kernel: the console libkrun gives, the init of the initramfs,
// and a panic that ends the VM rather than leaving it hung.
const cmdline = "console=hvc0 rdinit=/init panic=-1"

// bootTimeout bounds the wait for the init to say the image is mounted.
const bootTimeout = time.Minute

// Request is the sandbox to create.
type Request struct {
	// Description is the sandbox as the inventory describes it: its ID names its directory, and its Name
	// the VM, which the guest takes as hostname. Create fills its resources from those below.
	Description inventory.Description
	// CPUs and MemoryMiB are the resources of the VM.
	CPUs      uint8
	MemoryMiB uint32
	// Disk is the largest the write disk may be; the free space of the host lowers it.
	Disk rwdisk.Size
	// Plan is the layers of the image, the highest first.
	Plan erofs.Plan
	// Run is the description of the run, the disks aside, which Create fills from Plan and Disk.
	Run spec.Run
	// Remove marks the sandbox to be removed, its disk included, by the verb that stops its VM.
	Remove bool
}

// Sandbox is a VM that booted.
type Sandbox struct {
	// Dir is the directory of the sandbox, Console the file its console is written to.
	Dir, Console string
	// WriteDisk is the size the write disk was given.
	WriteDisk rwdisk.Size
	// VM is the cove-vmm that runs it.
	VM *vmmlaunch.VM
}

// Create makes the sandbox of req under root, the directory of the sandboxes, with the cove-vmm,
// kernel and init of libexec beside cove, and returns once its image is mounted. The sandbox enters
// the inventory first, and its cove-vmm holds its lock from then on. A sandbox that fails is
// removed, its VM included, and the error carries what cove-vmm and the console said.
func Create(ctx context.Context, root string, req Request) (_ *Sandbox, err error) {
	libexec, err := vmmlaunch.Dir()
	if err != nil {
		return nil, err //nolint:wrapcheck // Dir names libexec and how to get it.
	}
	// The disk is sized before the sandbox enters the inventory, for its description to say the size it
	// got.
	size, err := diskSize(root, req.Disk)
	if err != nil {
		return nil, err
	}
	desc := req.Description
	desc.CPUs, desc.MemoryMiB, desc.Disk = req.CPUs, req.MemoryMiB, int64(size)
	dir, lock, err := inventory.Add(ctx, root, desc)
	if err != nil {
		return nil, err //nolint:wrapcheck // Add names the sandbox or the name it carries.
	}
	defer func() {
		if err != nil {
			// The removal must happen whatever ended ctx, and while the lock is still held, so that
			// no one sees a stopped sandbox that is about to go.
			_ = inventory.Remove(context.WithoutCancel(ctx), root, req.Description.ID)
			lock.Release()
			return
		}
		// cove-vmm holds the lock from now on, and alone once cove ends.
		lock.Close()
	}()
	vmReq, err := write(libexec, dir, req, size)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // G304: the log of cove-vmm, in the directory of the sandbox.
	log, err := os.OpenFile(filepath.Join(dir, vmmLogFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create the log of cove-vmm: %w", err)
	}
	vmReq.Log, vmReq.Lock = log, lock.File()
	vm, err := vmmlaunch.Start(ctx, libexec, vmReq)
	_ = log.Close()
	if err != nil {
		return nil, withConsole(err, dir)
	}
	if err := waitReady(ctx, vm, vmReq.Console); err != nil {
		vm.Kill()
		return nil, withConsole(err, dir)
	}
	return &Sandbox{Dir: dir, Console: vmReq.Console, WriteDisk: size, VM: vm}, nil
}

// diskSize returns the size of the write disk of a sandbox of root, the largest up to capacity that
// the free space of root holds: the directory of the sandbox will be on its file system.
func diskSize(root string, capacity rwdisk.Size) (rwdisk.Size, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return 0, fmt.Errorf("create the directory of the sandboxes: %w", err)
	}
	free, err := rwdisk.Free(root)
	if err != nil {
		return 0, err //nolint:wrapcheck // Free names the directory.
	}
	//nolint:wrapcheck // Nominal names the free space and the margin.
	return rwdisk.Nominal(capacity, free, rwdisk.DefaultMargin)
}

// WriteDisk returns the path of the write disk of the sandbox in dir.
func WriteDisk(dir string) string {
	return filepath.Join(dir, writeDiskFile)
}

// write writes the files of the sandbox in dir, and returns the VM that boots from them.
func write(libexec, dir string, req Request, size rwdisk.Size) (vmmlaunch.Request, error) {
	vm := vmmlaunch.Request{
		CPUs: req.CPUs, MemoryMiB: req.MemoryMiB, Cmdline: cmdline,
		Kernel: filepath.Join(libexec, kernelFile), KernelFormat: kernelFormat(),
		Initramfs: filepath.Join(dir, initramfsFile), Console: filepath.Join(dir, consoleFile),
	}
	if req.Remove {
		if err := os.WriteFile(filepath.Join(dir, removeFile), nil, 0o600); err != nil {
			return vm, fmt.Errorf("mark the sandbox to be removed: %w", err)
		}
	}
	for _, l := range req.Plan.Layers {
		vm.Disks = append(vm.Disks, vmmproto.Disk{Path: l.Path, ReadOnly: true})
	}
	disk := WriteDisk(dir)
	if err := rwdisk.Create(disk, size); err != nil {
		return vm, err //nolint:wrapcheck // Create names the disk.
	}
	vm.Disks = append(vm.Disks, vmmproto.Disk{Path: disk})
	run, err := describe(req, size)
	if err != nil {
		return vm, err
	}
	if err := writeInitramfs(filepath.Join(libexec, initFile), vm.Initramfs, run); err != nil {
		return vm, err
	}
	// The monitor opens the console by its path, and may create no file.
	if err := os.WriteFile(vm.Console, nil, 0o600); err != nil {
		return vm, fmt.Errorf("create the console: %w", err)
	}
	return vm, nil
}

// describe returns the description of the run with its disks: the layers of the plan, sized by
// their files, and the write disk.
func describe(req Request, size rwdisk.Size) (*spec.Run, error) {
	run := req.Run
	run.Hostname = req.Description.Name
	run.MountOptions = slices.Clone(req.Plan.MountOptions)
	run.Layers = make([]spec.Layer, len(req.Plan.Layers))
	for i, l := range req.Plan.Layers {
		fi, err := os.Stat(l.Path)
		if err != nil {
			return nil, fmt.Errorf("size the disk of layer %s: %w", l.DiffID, err)
		}
		run.Layers[i] = spec.Layer{Disk: spec.Disk{Size: fi.Size()}, DiffID: l.DiffID, Mountpoint: l.Mountpoint}
	}
	run.Write = spec.Disk{Size: int64(size)}
	return &run, nil
}

// writeInitramfs writes at path the initramfs of run, the init read from init.
func writeInitramfs(init, path string, run *spec.Run) error {
	//nolint:gosec // G304: the init of libexec, beside cove-vmm.
	bin, err := os.ReadFile(init)
	if err != nil {
		return fmt.Errorf("read the init: %w", err)
	}
	var buf bytes.Buffer
	if err := initramfs.WriteBase(&buf, bin); err != nil {
		return err //nolint:wrapcheck // The archive names what it could not write.
	}
	if err := initramfs.WriteRun(&buf, run); err != nil {
		return err //nolint:wrapcheck // The archive names what it could not write.
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write the initramfs: %w", err)
	}
	return nil
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
