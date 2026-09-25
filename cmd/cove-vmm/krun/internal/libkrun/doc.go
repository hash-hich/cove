// Package libkrun drives libkrun, the virtual machine monitor cove-vmm-krun links: it configures
// one VM and hands the process over to it. It lives under cmd/cove-vmm/krun so that the compiler
// lets no other binary link it, cove above all: cove-vmm-krun is the process a guest that escapes
// lands in.
//
// The prototypes of the functions called are declared here rather than taken from the header of
// libkrun, which is not kept in the repository: they are the part of the API cove relies on, and
// a bump of libkrun checks them against its header.
package libkrun
