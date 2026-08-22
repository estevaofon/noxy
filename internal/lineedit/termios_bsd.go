//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package lineedit

import "golang.org/x/sys/unix"

// BSDs e macOS: tcgetattr/tcsetattr sao TIOCGETA/TIOCSETA.
const (
	ioctlReadTermios  = unix.TIOCGETA
	ioctlWriteTermios = unix.TIOCSETA
)
