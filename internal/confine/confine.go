// Package confine takes from cove-vmm every right the virtual machine monitor does not need, so
// that a guest that escapes into it lands in a process that can reach nothing of the user: no
// file it was not handed, no network, no program to start. Enter is called once, after the
// descriptors are inherited and before the monitor runs.
package confine
