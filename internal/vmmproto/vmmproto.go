// Package vmmproto is the contract between cove and cove-vmm, the process that runs the virtual
// machine monitor of one VM. cove opens every file the VM needs and hands cove-vmm descriptors,
// never paths: cove-vmm holds no right to open anything of the user, and whatever VMM it drives,
// the contract stays the same seen from cove.
//
// cove starts cove-vmm with one end of a socket pair on descriptor Pipe and the files of the VM on
// the descriptors after it. Three JSON messages follow, one each way then one back: cove-vmm says
// who it is in a Hello, cove describes the VM in a Boot, and cove-vmm answers with a Status once
// the VMM holds the VM, before it starts it. A cove-vmm of another build than cove is refused on
// its Hello, since the two change together.
package vmmproto

import (
	"encoding/json"
	"fmt"
	"io"
)

// Pipe is the descriptor of the socket cove-vmm talks to cove on, the first after the standard
// streams. The files of the VM come after it, in the order the Boot names them.
const Pipe = 3

// build names the build of cove this binary comes from, set by the linker when make builds cove
// and cove-vmm together; empty when go builds one alone.
var build string

// Build returns the build of cove this binary comes from, empty when it was not built by make.
func Build() string { return build }

// Hello is what cove-vmm says first: which build it comes from and which VMM it loaded.
type Hello struct {
	// Build is the build of cove-vmm, which must be the build of cove.
	Build string `json:"build"`
	// VMM names the monitor and its version, as the report gives them.
	VMM string `json:"vmm"`
	// LibrarySHA256 is the sha256 of the library of the monitor cove-vmm loaded, checked against
	// the one it was built with before it said anything.
	LibrarySHA256 string `json:"librarySha256"`
}

// Boot is the VM cove asks for. Each file is a descriptor cove-vmm inherited.
type Boot struct {
	// CPUs and MemoryMiB are the resources of the VM.
	CPUs      uint8  `json:"cpus"`
	MemoryMiB uint32 `json:"memoryMib"`
	// Kernel is the kernel the VM boots, in KernelFormat.
	Kernel       int          `json:"kernel"`
	KernelFormat KernelFormat `json:"kernelFormat"`
	// Initramfs is the initramfs of the run, the init and the description of the run.
	Initramfs int `json:"initramfs"`
	// Cmdline is the command line of the kernel.
	Cmdline string `json:"cmdline"`
	// Disks are attached in this order, which is the order the init finds them in.
	Disks []Disk `json:"disks"`
	// Console receives what the guest writes on its console.
	Console int `json:"console"`
}

// KernelFormat is the format of the file of a kernel.
type KernelFormat string

// The formats of the kernels cove boots: the raw Image of arm64 and the ELF vmlinux of x86_64.
const (
	KernelRaw KernelFormat = "raw"
	KernelELF KernelFormat = "elf"
)

// Disk is a disk of the VM.
type Disk struct {
	// FD is the descriptor of the file of the disk.
	FD int `json:"fd"`
	// ReadOnly is true for the layers of the image, false for the disk the run writes on.
	ReadOnly bool `json:"readOnly"`
}

// Status is the answer of cove-vmm to a Boot: the VMM holds the VM and starts it, or it could not.
type Status struct {
	// Error says why the VM could not be started; empty when it is starting.
	Error string `json:"error,omitempty"`
}

// Send writes m on w as one message.
func Send(w io.Writer, m any) error {
	if err := json.NewEncoder(w).Encode(m); err != nil {
		return fmt.Errorf("send %T: %w", m, err)
	}
	return nil
}

// Receiver reads the messages of one side, in order.
type Receiver struct {
	dec *json.Decoder
}

// NewReceiver returns a receiver of the messages r carries. An unknown field is refused: the two
// sides come from one build.
func NewReceiver(r io.Reader) *Receiver {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	return &Receiver{dec: dec}
}

// Receive reads the next message into m. It returns io.EOF when the other side closed its end
// before sending it.
func (r *Receiver) Receive(m any) error {
	if err := r.dec.Decode(m); err != nil {
		// Decode returns io.EOF itself, unwrapped, when nothing came before the end.
		if err == io.EOF {
			return io.EOF
		}
		return fmt.Errorf("receive %T: %w", m, err)
	}
	return nil
}
