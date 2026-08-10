//go:build !windows

package main

import "os"

func openTerminalDevice() (terminalDevice, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}
