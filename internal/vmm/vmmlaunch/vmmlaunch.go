// Package vmmlaunch starts cove-vmm, the process that runs the monitor of one VM, and hands it the
// VM as vmmproto describes: a Boot on the pipe that names the files of the VM by their resolved
// paths, and the Status back. It never starts a monitor itself, and cove links no monitor.
package vmmlaunch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"gitlab.com/hich-hich/cove/internal/process"
	"gitlab.com/hich-hich/cove/internal/vmm/vmmproto"
)

// Program is the name in Dir of the cove-vmm that runs libkrun, the one monitor cove drives yet.
const Program = "cove-vmm-krun"

// Request is the VM to start. Its files exist, the console included: the monitor opens each one by
// its path, and may create none.
type Request struct {
	CPUs      uint8
	MemoryMiB uint32
	// Kernel is the kernel, in KernelFormat.
	Kernel       string
	KernelFormat vmmproto.KernelFormat
	// Initramfs is the init and the description of the run.
	Initramfs string
	// Cmdline is the command line of the kernel.
	Cmdline string
	// Disks are attached in this order.
	Disks []vmmproto.Disk
	// Console receives the console of the guest.
	Console string
	// Vsock are the ports of the guest the host connects to, each through a Unix socket that must
	// not exist yet, in one directory.
	Vsock []vmmproto.VsockPort
	// Log receives what cove-vmm itself says, on its standard output and error. It is a file of its
	// own: the monitor empties the console as it opens it, and would write over it.
	Log *os.File
	// Lock, when not nil, is a file under a lock of flock that cove-vmm inherits on process.Lock
	// and never touches: the lock is held for as long as cove-vmm lives, and the kernel releases it
	// when it ends, by whatever means, which is how cove tells a VM that runs from one that ended.
	Lock *os.File
}

// VM is a cove-vmm that holds a VM.
type VM struct {
	// Hello is what cove-vmm said of itself.
	Hello vmmproto.Hello
	cmd   *exec.Cmd
	done  chan struct{}
	err   error
}

// Start starts the cove-vmm of dir, the libexec beside cove, and gives it req. It returns once the
// monitor holds the VM and starts it, or with the reason it could not. cove-vmm outlives cove, as
// the package process starts it: the VM is stopped by other means than the death of whoever
// created it, and a VM that has not started is killed by Start itself.
func Start(ctx context.Context, dir string, req Request) (*VM, error) {
	boot, err := bootOf(req)
	if err != nil {
		return nil, err
	}
	pair, err := process.Socketpair()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", Program, err)
	}
	ours, theirs := pair[0], pair[1]
	defer func() { _ = ours.Close() }()
	cmd := process.Command(dir, Program, req.Log, theirs, req.Lock)
	err = cmd.Start()
	_ = theirs.Close()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", Program, err)
	}
	vm := &VM{cmd: cmd, done: make(chan struct{})}
	go func() {
		vm.err = cmd.Wait()
		close(vm.done)
	}()
	stop := context.AfterFunc(ctx, func() { _ = ours.Close() })
	defer stop()
	if err := vm.handshake(ours, boot); err != nil {
		vm.Kill()
		// Closing the pipe is how the end of ctx stops the handshake, and what it reads then says
		// nothing of the reason.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("start %s: %w", Program, context.Cause(ctx))
		}
		return nil, err
	}
	return vm, nil
}

// bootOf returns the Boot of req, each file named by its absolute path with no link left in it:
// the sandbox of cove-vmm matches the path a file is reached by, and a link in it is a path it
// does not allow.
func bootOf(req Request) (vmmproto.Boot, error) {
	var err error
	resolve := func(path string) string {
		if err != nil {
			return ""
		}
		var resolved string
		if resolved, err = filepath.EvalSymlinks(path); err == nil {
			resolved, err = filepath.Abs(resolved)
		}
		if err != nil {
			err = fmt.Errorf("resolve a file of the VM: %w", err)
		}
		return resolved
	}
	boot := vmmproto.Boot{VM: vmmproto.VM{
		CPUs: req.CPUs, MemoryMiB: req.MemoryMiB,
		Kernel: resolve(req.Kernel), KernelFormat: req.KernelFormat,
		Initramfs: resolve(req.Initramfs), Cmdline: req.Cmdline,
		Console: resolve(req.Console),
	}}
	for _, d := range req.Disks {
		boot.VM.Disks = append(boot.VM.Disks, vmmproto.Disk{Path: resolve(d.Path), ReadOnly: d.ReadOnly})
	}
	// A socket is created by the monitor, so its directory is what is resolved.
	for _, v := range req.Vsock {
		socket := filepath.Join(resolve(filepath.Dir(v.Socket)), filepath.Base(v.Socket))
		boot.VM.Vsock = append(boot.VM.Vsock, vmmproto.VsockPort{Port: v.Port, Socket: socket})
	}
	return boot, err
}

// handshake checks who cove-vmm is, sends it the VM, and waits for its answer.
func (vm *VM) handshake(pipe io.ReadWriter, boot vmmproto.Boot) error {
	r := process.NewReceiver(pipe)
	if err := r.Receive(&vm.Hello); err != nil {
		return vm.failed(err)
	}
	if vm.Hello.Build != process.Build() {
		return fmt.Errorf("%s comes from build %q and cove from build %q: build both with make",
			Program, vm.Hello.Build, process.Build())
	}
	if err := process.Send(pipe, boot); err != nil {
		return vm.failed(err)
	}
	var status vmmproto.Status
	if err := r.Receive(&status); err != nil {
		return vm.failed(err)
	}
	if status.Error != "" {
		return fmt.Errorf("%s: %s", Program, status.Error)
	}
	return nil
}

// failed returns err, or when cove-vmm closed the pipe by exiting, how it exited: what it said on
// the way is in the console.
func (vm *VM) failed(err error) error {
	if !errors.Is(err, io.EOF) {
		return err
	}
	<-vm.done
	return fmt.Errorf("%s ended before it held the VM: %w", Program, vm.err)
}

// Pid returns the process of cove-vmm.
func (vm *VM) Pid() int { return vm.cmd.Process.Pid }

// Process returns what names the cove-vmm of vm for good, to be killed later by another cove.
func (vm *VM) Process() (process.Process, error) {
	p, ok, err := process.Find(vm.Pid())
	if err == nil && !ok {
		// A process gone from the table was reaped, so how it exited is known.
		<-vm.done
		err = fmt.Errorf("%s %d ended", Program, vm.Pid())
		if vm.err != nil {
			err = fmt.Errorf("%w: %w", err, vm.err)
		}
	}
	return p, err
}

// Done is closed when cove-vmm has exited, while the process that started it is still there to
// see it.
func (vm *VM) Done() <-chan struct{} { return vm.done }

// Err returns how cove-vmm exited, once Done is closed.
func (vm *VM) Err() error { return vm.err }

// Kill ends cove-vmm and the VM with it, and waits for it.
func (vm *VM) Kill() {
	_ = vm.cmd.Process.Kill()
	<-vm.done
}
