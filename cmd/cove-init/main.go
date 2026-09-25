//go:build linux

// Command cove-init is the init of the VMs of cove, PID 1 from their initramfs. It is built for
// Linux alone, static, for the architecture of the guest: make cove-init builds it for each one.
package main

import "gitlab.com/hich-hich/cove/internal/vminit/boot"

func main() {
	boot.Main()
}
