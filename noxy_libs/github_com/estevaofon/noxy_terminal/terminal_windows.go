//go:build windows

package main

import "os"

func openTerminalDevice() (terminalDevice, error) {
	return os.OpenFile("CONIN$", os.O_RDWR, 0)
}
