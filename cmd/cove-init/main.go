//go:build linux

// Command cove-init is the init of the VMs of cove, PID 1 from their initramfs. It is built for
// Linux alone, static, for the architecture of the guest:
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/cove-init ./cmd/cove-init
package main

import "gitlab.com/hich-hich/cove/internal/vminit/boot"

func main() {
	boot.Main()
}
