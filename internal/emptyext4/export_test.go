package emptyext4

import "io/fs"

// The internals of the package, exposed to its tests.
var (
	Decode       = decode
	CheckSparse  = checkSparse
	ErrNotSparse = errNotSparse
)

// Magic exposes magic, so that the tests build files of their own.
const Magic = magic

// Files returns the files of generated/, as the package embeds them.
func Files() fs.FS {
	sub, err := fs.Sub(files, "generated")
	if err != nil {
		panic(err)
	}
	return sub
}
