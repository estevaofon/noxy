// internal/ext/process_spawn_linux.go
//go:build linux

package ext

import (
	"os/exec"
	"syscall"
)

// Pdeathsig: se o host morrer sem passar por Close, o kernel mata o filho
// (spec §4.5). A regra de EOF continua sendo a guarda principal — e a unica
// onde o sandbox recusa o prctl (AWS Lambda): execSpawner repete sem ela.
func applyDeathGuard(cmd *exec.Cmd) bool {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	return true
}

func attachJobObject(int) func() { return nil }
