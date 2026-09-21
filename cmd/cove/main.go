// Command cove runs a coding agent unsupervised in a micro-VM.
//
// The reader of tar archives flags a name that is absolute or climbs above the root of the
// archive. The conversion of a layer bounds such a name itself and takes the header anyway; the
// flag is a belt over that, and what a future Go will do by default.
//
//go:debug tarinsecurepath=0
package main

import (
	"os"

	"gitlab.com/hich-hich/cove/internal/cli"
)

func main() {
	app := &cli.App{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	os.Exit(app.Run(os.Args[1:]))
}
