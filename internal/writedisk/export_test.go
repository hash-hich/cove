package writedisk

import "io"

// ErrNotSparse exposes errNotSparse to the tests of the package.
var ErrNotSparse = errNotSparse

// TemplateMagic exposes templateMagic, so that the tests build templates of their own.
const TemplateMagic = templateMagic

// Template opens the embedded template of size, as Create reads it.
func Template(size Size) (io.ReadCloser, error) {
	return templates.Open("templates/" + size.String() + ".gz") //nolint:wrapcheck // The tests read the error.
}

// Decode exposes decode to the tests of the package.
var Decode = decode

// CheckSparse exposes checkSparse: no file system a test can reach keeps no sparse files, so the
// refusal is exercised on a file written dense by hand.
var CheckSparse = checkSparse
