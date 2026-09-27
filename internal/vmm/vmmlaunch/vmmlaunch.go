// Package vmmlaunch starts cove-vmm, the process that runs the monitor of one VM, and hands it the
// VM as vmmproto describes: a Boot on the pipe that names the files of the VM by their resolved
// paths, and the Status back. It never starts a monitor itself, and cove links no monitor.
package vmmlaunch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/internal/vmm/vmmproto"
)

// Program is the name in Dir of the cove-vmm that runs libkrun, the one monitor cove drives yet.
const Program = "cove-vmm-krun"

// Dir returns the directory of the files cove runs a VM with, cove-vmm among them: libexec beside
// the bin directory cove was started from. It is found from the executable, never from PATH,
// since it decides what monitor runs the VM. A missing libexec is named with how to get it, since
// the error comes after the image was pulled and a bare missing file would not say why.
func Dir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find cove: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("find cove: %w", err)
	}
	dir := filepath.Join(filepath.Dir(filepath.Dir(exe)), "libexec")
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("no libexec beside cove, at %s: make build builds cove with it", dir)
	} else if err != nil {
		return "", fmt.Errorf("find libexec: %w", err)
	}
	return dir, nil
}

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
	// Log receives what cove-vmm itself says, on its standard output and error. It is a file of its
	// own: the monitor empties the console as it opens it, and would write over it.
	Log *os.File
	// Lock, when not nil, is a file under a lock of flock that cove-vmm inherits on vmmproto.Lock
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

// Start starts the cove-vmm of dir and gives it req. It returns once the monitor holds the VM and
// starts it, or with the reason it could not. cove-vmm runs in a session of its own, without the
// environment of cove and in the root directory, and outlives cove: the VM is stopped by other
// means than the death of whoever created it.
func Start(ctx context.Context, dir string, req Request) (*VM, error) {
	boot, err := bootOf(req)
	if err != nil {
		return nil, err
	}
	fds, err := socketpair()
	if err != nil {
		return nil, fmt.Errorf("create the pipe of cove-vmm: %w", err)
	}
	ours, theirs := os.NewFile(uintptr(fds[0]), "pipe"), os.NewFile(uintptr(fds[1]), "pipe")
	defer func() { _ = ours.Close() }()

	// Not CommandContext: the end of ctx must not kill a VM that started, and a VM that has not is
	// killed by Start itself.
	//nolint:gosec,noctx // G204: the program is cove-vmm beside cove, and it takes no argument.
	cmd := exec.Command(filepath.Join(dir, Program))
	cmd.Dir = "/"
	cmd.Env = []string{}
	cmd.Stdout, cmd.Stderr = req.Log, req.Log
	// ExtraFiles puts its first file on descriptor 3, vmmproto.Pipe, and the next on vmmproto.Lock.
	cmd.ExtraFiles = []*os.File{theirs}
	if req.Lock != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, req.Lock)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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

// socketpair returns a pair of connected sockets that no other program started meanwhile inherits.
// macOS has no SOCK_CLOEXEC, so the flag is set after, under the lock os/exec forks under.
func socketpair() ([2]int, error) {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return fds, err //nolint:wrapcheck // The caller names the pipe.
	}
	unix.CloseOnExec(fds[0])
	unix.CloseOnExec(fds[1])
	return fds, nil
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
	return boot, err
}

// handshake checks who cove-vmm is, sends it the VM, and waits for its answer.
func (vm *VM) handshake(pipe io.ReadWriter, boot vmmproto.Boot) error {
	r := vmmproto.NewReceiver(pipe)
	if err := r.Receive(&vm.Hello); err != nil {
		return vm.failed(err)
	}
	if vm.Hello.Build != vmmproto.Build() {
		return fmt.Errorf("%s comes from build %q and cove from build %q: build both with make",
			Program, vm.Hello.Build, vmmproto.Build())
	}
	if err := vmmproto.Send(pipe, boot); err != nil {
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
