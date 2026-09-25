// Package krun drives libkrun, the virtual machine monitor cove-vmm links: it configures one VM
// and hands the process over to it. It is linked by cove-vmm alone, the process a guest that
// escapes lands in, and never by cove.
//
// The prototypes of the functions called are declared here rather than taken from the header of
// libkrun, which is not kept in the repository: they are the part of the API cove relies on, and
// a bump of libkrun checks them against its header.
package krun
