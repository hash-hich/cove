// Package confine takes from cove-vmm every right the virtual machine monitor does not need, so
// that a guest that escapes into it lands in a process that can reach nothing of the user: no
// file but those of its VM, no network, no program to start. Enter is called once, with the files
// of the VM, before the monitor runs. It lives under cmd/cove-vmm, the only binaries it means
// something in.
package confine
