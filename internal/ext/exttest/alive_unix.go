// internal/ext/exttest/alive_unix.go
//go:build !windows

package exttest

import "syscall"

// ProcessAlive diz se o pid existe (kill 0). EPERM tambem significa vivo.
func ProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
