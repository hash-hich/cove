// Package vmlaunch starts cove-vmm, the process that runs the monitor of one VM, and hands it the
// VM as vmmproto describes: the files already open, a Boot on the pipe, and the Status back. It
// never starts a monitor itself, and cove links no monitor.
package vmlaunch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/internal/vmmproto"
)

// Program is the name of cove-vmm in Dir.
const Program = "cove-vmm"

// Dir returns the directory of the files cove runs a VM with, cove-vmm among them: libexec beside
// the bin directory cove was started from. It is found from the executable, never from PATH,
// since it decides what monitor runs the VM.
func Dir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find cove: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("find cove: %w", err)
	}
	return filepath.Join(filepath.Dir(filepath.Dir(exe)), "libexec"), nil
}

// Request is the VM to start. Its files are open, and stay the caller's to close: cove-vmm holds
// its own descriptors once Start returns.
type Request struct {
	CPUs      uint8
	MemoryMiB uint32
	// Kernel is the kernel, in KernelFormat.
	Kernel       *os.File
	KernelFormat vmmproto.KernelFormat
	// Initramfs is the init and the description of the run.
	Initramfs *os.File
	// Cmdline is the command line of the kernel.
	Cmdline string
	// Disks are attached in this order.
	Disks []Disk
	// Console receives the console of the guest, and what cove-vmm itself says on stderr.
	Console *os.File
}

// Disk is a disk of the VM.
type Disk struct {
	File     *os.File
	ReadOnly bool
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
	fds, err := socketpair()
	if err != nil {
		return nil, fmt.Errorf("create the pipe of cove-vmm: %w", err)
	}
	ours, theirs := os.NewFile(uintptr(fds[0]), "pipe"), os.NewFile(uintptr(fds[1]), "pipe")
	defer func() { _ = ours.Close() }()

	files, boot := layout(theirs, req)
	// Not CommandContext: the end of ctx must not kill a VM that started, and a VM that has not is
	// killed by Start itself.
	//nolint:gosec,noctx // G204: the program is cove-vmm beside cove, and it takes no argument.
	cmd := exec.Command(filepath.Join(dir, Program))
	cmd.Dir = "/"
	cmd.Env = []string{}
	cmd.Stdout, cmd.Stderr = req.Console, req.Console
	cmd.ExtraFiles = files
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

// layout returns the files cove-vmm inherits, the pipe first, and the Boot that names them by the
// descriptor each one lands on.
func layout(pipe *os.File, req Request) ([]*os.File, vmmproto.Boot) {
	files := []*os.File{pipe}
	fd := func(f *os.File) int {
		files = append(files, f)
		return vmmproto.Pipe + len(files) - 1
	}
	boot := vmmproto.Boot{
		CPUs: req.CPUs, MemoryMiB: req.MemoryMiB,
		Kernel: fd(req.Kernel), KernelFormat: req.KernelFormat,
		Initramfs: fd(req.Initramfs), Cmdline: req.Cmdline,
		Console: fd(req.Console),
	}
	for _, d := range req.Disks {
		boot.Disks = append(boot.Disks, vmmproto.Disk{FD: fd(d.File), ReadOnly: d.ReadOnly})
	}
	return files, boot
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
