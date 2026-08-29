// internal/ext/process_spawn_linux.go
//go:build linux

package ext

import (
	"os/exec"
	"syscall"
)

// Pdeathsig: se o host morrer sem passar por Close, o kernel mata o filho
// (spec §4.5). A regra de EOF continua sendo a guarda principal.
func applyDeathGuard(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

func attachJobObject(int) func() { return nil }
