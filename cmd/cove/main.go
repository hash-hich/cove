// Command cove runs a coding agent unsupervised in a micro-VM.
package main

import (
	"os"

	"gitlab.com/hich-hich/cove/internal/cli"
)

func main() {
	app := &cli.App{Stdout: os.Stdout, Stderr: os.Stderr}
	os.Exit(app.Run(os.Args[1:]))
}
