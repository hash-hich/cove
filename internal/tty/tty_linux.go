package tty

import "golang.org/x/sys/unix"

const (
	getTermios = unix.TCGETS
	setTermios = unix.TCSETS
)
