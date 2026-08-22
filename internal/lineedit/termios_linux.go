//go:build linux

package lineedit

import "golang.org/x/sys/unix"

// Linux: tcgetattr/tcsetattr sao TCGETS/TCSETS.
const (
	ioctlReadTermios  = unix.TCGETS
	ioctlWriteTermios = unix.TCSETS
)
