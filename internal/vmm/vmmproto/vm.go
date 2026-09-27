package vmmproto

// VM is a VM to boot.
type VM struct {
	// CPUs and MemoryMiB are the resources of the VM.
	CPUs      uint8  `json:"cpus"`
	MemoryMiB uint32 `json:"memoryMib"`
	// Kernel is the kernel the VM boots, in KernelFormat.
	Kernel       string       `json:"kernel"`
	KernelFormat KernelFormat `json:"kernelFormat"`
	// Initramfs is the initramfs of the run, the init and the description of the run.
	Initramfs string `json:"initramfs"`
	// Cmdline is the command line of the kernel.
	Cmdline string `json:"cmdline"`
	// Disks are attached in this order, which is the order the init finds them in.
	Disks []Disk `json:"disks"`
	// Console receives what the guest writes on its console.
	Console string `json:"console"`
	// Vsock are the ports of the guest the host connects to, each through a Unix socket the
	// monitor creates and listens on. The sockets share one directory.
	Vsock []VsockPort `json:"vsock,omitempty"`
}

// VsockPort is a port of the guest reached from the host through a Unix socket.
type VsockPort struct {
	// Port is the vsock port the guest listens on.
	Port uint32 `json:"port"`
	// Socket is the path of the Unix socket; nothing may be there when the VM starts.
	Socket string `json:"socket"`
}

// Files are the files a cove-vmm confines its monitor to.
type Files struct {
	// Read are the files the monitor reads, Write those it writes as well, Listen the Unix
	// sockets it creates and accepts connections on.
	Read, Write, Listen []string
}

// Files returns the files of vm.
func (vm VM) Files() Files {
	f := Files{Read: []string{vm.Kernel, vm.Initramfs}}
	for _, d := range vm.Disks {
		if d.ReadOnly {
			f.Read = append(f.Read, d.Path)
		} else {
			f.Write = append(f.Write, d.Path)
		}
	}
	f.Write = append(f.Write, vm.Console)
	for _, v := range vm.Vsock {
		f.Listen = append(f.Listen, v.Socket)
	}
	return f
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
	// Path is the file of the disk.
	Path string `json:"path"`
	// ReadOnly is true for the layers of the image, false for the disk the run writes on.
	ReadOnly bool `json:"readOnly"`
}
