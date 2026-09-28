// Package tty puts the terminal of the host in the mode an attached agent needs: raw, every key
// sent as it is typed, and sized as the window is.
package tty

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// MakeRaw puts the terminal fd in raw mode, as cfmakeraw does: no echo, no line editing, no
// signal from a key, no translation of what is read or written. The keys reach the agent, whose
// own terminal in the guest does all that. It returns the function that puts the terminal back as
// it was.
func MakeRaw(fd int) (func() error, error) {
	old, err := unix.IoctlGetTermios(fd, getTermios)
	if err != nil {
		return nil, fmt.Errorf("read the mode of the terminal: %w", err)
	}
	raw := *old
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR |
		unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, setTermios, &raw); err != nil {
		return nil, fmt.Errorf("put the terminal in raw mode: %w", err)
	}
	return func() error {
		if err := unix.IoctlSetTermios(fd, setTermios, old); err != nil {
			return fmt.Errorf("put the terminal back: %w", err)
		}
		return nil
	}, nil
}

// Size is the size of a terminal.
type Size struct {
	Rows, Cols uint16
}

// SizeOf returns the size of the terminal fd.
func SizeOf(fd int) (Size, error) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return Size{}, fmt.Errorf("read the size of the terminal: %w", err)
	}
	return Size{Rows: ws.Row, Cols: ws.Col}, nil
}
