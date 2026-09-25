package main

// seekData and seekHole are the whence of lseek that find the data and the holes of a sparse
// file, which package syscall does not name. macOS numbers them the other way round.
const (
	seekData = 3
	seekHole = 4
)
