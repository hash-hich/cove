//go:build !(cgo && (darwin || linux))

// Command cove-vmm runs the virtual machine monitor of one VM of cove. It links libkrun through
// cgo, for macOS and Linux only: built any other way, it says so and exits.
package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "cove-vmm: built without cgo, or for a platform libkrun does not run on")
	os.Exit(1)
}
