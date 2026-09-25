//go:build cgo && (darwin || linux)

package krun

/*
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>
#define _GNU_SOURCE
#include <dlfcn.h>

int32_t krun_create_ctx(void);
int32_t krun_set_vm_config(uint32_t ctx_id, uint8_t num_vcpus, uint32_t ram_mib);
int32_t krun_set_kernel(uint32_t ctx_id, const char *kernel_path, uint32_t kernel_format,
	const char *initramfs, const char *cmdline);
int32_t krun_add_disk3(uint32_t ctx_id, const char *block_id, const char *disk_path,
	uint32_t disk_format, bool read_only, bool direct_io, uint32_t sync_mode);
int32_t krun_set_console_output(uint32_t ctx_id, const char *c_filepath);
int32_t krun_disable_implicit_vsock(uint32_t ctx_id);
int32_t krun_start_enter(uint32_t ctx_id);

// libkrun_path returns the file the dynamic linker loaded libkrun from, NULL when it cannot tell.
static const char *libkrun_path(void) {
	Dl_info info;
	if (dladdr((void *)krun_create_ctx, &info) == 0) {
		return NULL;
	}
	return info.dli_fname;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// The constants of libkrun.h this package passes.
const (
	diskFormatRaw   = 0
	syncModeRelaxed = 1
)

// Format is the format of a kernel file, as krun_set_kernel takes it.
type Format uint32

// The formats of the kernels cove boots: the raw Image of arm64 and the ELF vmlinux of x86_64.
const (
	FormatRaw Format = 0
	FormatELF Format = 1
)

// Ctx is one VM being configured. Nothing runs until StartEnter.
type Ctx struct {
	id C.uint32_t
}

// New creates the configuration of a VM, without the vsock device libkrun adds by default: the
// channel of the turns is not libkrun's, and TSI is never enabled.
func New() (*Ctx, error) {
	id := C.krun_create_ctx()
	if id < 0 {
		return nil, callError("create the context", id)
	}
	c := &Ctx{id: C.uint32_t(id)}
	if err := check("disable the implicit vsock", C.krun_disable_implicit_vsock(c.id)); err != nil {
		return nil, err
	}
	return c, nil
}

// SetResources sets the vCPUs and the memory, in MiB, of the VM.
func (c *Ctx) SetResources(cpus uint8, memoryMiB uint32) error {
	return check("set the vCPUs and the memory", C.krun_set_vm_config(c.id, C.uint8_t(cpus), C.uint32_t(memoryMiB)))
}

// SetKernel sets the kernel the VM boots, its initramfs and its command line. libkrun appends its
// own variables to the command line, and loads no firmware of its own once a kernel is set.
func (c *Ctx) SetKernel(kernel string, format Format, initramfs, cmdline string) error {
	ck, ci, cl := C.CString(kernel), C.CString(initramfs), C.CString(cmdline)
	defer free(ck, ci, cl)
	return check("set the kernel", C.krun_set_kernel(c.id, ck, C.uint32_t(format), ci, cl))
}

// AddDisk attaches the raw disk at path, under the name id, which the guest reads as its serial.
// A disk is always raw: a format probed on a file the guest wrote could be one it forged.
func (c *Ctx) AddDisk(id, path string, readOnly bool) error {
	cid, cp := C.CString(id), C.CString(path)
	defer free(cid, cp)
	return check("attach disk "+id, C.krun_add_disk3(c.id, cid, cp, diskFormatRaw, C.bool(readOnly), false,
		syncModeRelaxed))
}

// SetConsoleOutput writes what the guest writes on its console into the file at path.
func (c *Ctx) SetConsoleOutput(path string) error {
	cp := C.CString(path)
	defer free(cp)
	return check("set the console output", C.krun_set_console_output(c.id, cp))
}

// StartEnter starts the VM and gives the process to it: libkrun exits the process when the guest
// powers off. It returns only when the VM could not start.
func (c *Ctx) StartEnter() error {
	return callError("start the VM", C.krun_start_enter(c.id))
}

// LibraryPath returns the file libkrun was loaded from.
func LibraryPath() (string, error) {
	p := C.libkrun_path()
	if p == nil {
		return "", errors.New("the dynamic linker does not say where libkrun was loaded from")
	}
	return C.GoString(p), nil
}

func check(what string, ret C.int32_t) error {
	if ret < 0 {
		return callError(what, ret)
	}
	return nil
}

// callError names what failed with the errno libkrun returns negated.
func callError(what string, ret C.int32_t) error {
	return fmt.Errorf("libkrun: %s: %w", what, syscall.Errno(-ret))
}

func free(ps ...*C.char) {
	for _, p := range ps {
		C.free(unsafe.Pointer(p))
	}
}
