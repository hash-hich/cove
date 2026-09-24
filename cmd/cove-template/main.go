// Command cove-template turns the empty ext4 file systems mkfs.ext4 made into the templates of
// the write disk cove embeds, and writes a template back to a file for e2fsck to check. It runs in
// the build of internal/writedisk/Dockerfile, never on a host:
//
//	cove-template extract DISK TEMPLATE
//	cove-template write SIZE TEMPLATE DISK
package main

import (
	"fmt"
	"os"

	"gitlab.com/hich-hich/cove/internal/writedisk"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "cove-template:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	switch {
	case len(args) == 3 && args[0] == "extract":
		return extract(args[1], args[2])
	case len(args) == 4 && args[0] == "write":
		size, err := writedisk.ParseSize(args[1])
		if err != nil {
			return err //nolint:wrapcheck // ParseSize names the sizes.
		}
		return writedisk.CreateFrom(args[3], size, args[2]) //nolint:wrapcheck // CreateFrom names the disk.
	default:
		return fmt.Errorf("usage: cove-template extract DISK TEMPLATE | write SIZE TEMPLATE DISK, not %q", args)
	}
}

func extract(disk, template string) error {
	src, err := os.Open(disk) //nolint:gosec // G304: the disk mkfs.ext4 made in the build.
	if err != nil {
		return err //nolint:wrapcheck // The error names the file.
	}
	defer func() { _ = src.Close() }()
	dst, err := os.Create(template) //nolint:gosec // G304: the template the build writes.
	if err != nil {
		return err //nolint:wrapcheck // The error names the file.
	}
	if err := writedisk.Extract(src, dst); err != nil {
		_ = dst.Close()
		return err //nolint:wrapcheck // Extract names the disk.
	}
	return dst.Close() //nolint:wrapcheck // The error names the file.
}
