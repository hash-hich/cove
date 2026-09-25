package main

// seekData and seekHole are the whence of lseek that find the data and the holes of a sparse
// file, which package syscall does not name. Linux numbers them the other way round.
const (
	seekData = 4
	seekHole = 3
)
