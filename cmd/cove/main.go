//go:debug tarinsecurepath=0

// Command cove runs a coding agent unsupervised in a micro-VM.
//
// tarinsecurepath=0 is the belt on the names a layer carries: archive/tar then reports a name
// that is not local instead of waving it through, and internal/rootfs/disk, whose job is to
// bound those names, cannot miss one in silence. The setting belongs to the main package, which
// is why it sits here and not next to the loop it serves.
package main

import (
	"os"

	"gitlab.com/hich-hich/cove/internal/cli"
)

func main() {
	app := &cli.App{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	os.Exit(app.Run(os.Args[1:]))
}
