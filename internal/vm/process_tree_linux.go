//go:build linux

package vm

import (
	"errors"
	"os/exec"
	"syscall"
)

// configureProcessGroup: grupo proprio (Setpgid) e guarda de morte
// (Pdeathsig): se o Noxy morrer sem passar por CloseProcesses, o kernel
// mata o lider (spec de design do modulo process, §4.3 — best effort:
// netos sobrevivem). Devolve se a guarda foi aplicada.
func configureProcessGroup(cmd *exec.Cmd) bool {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	return true
}

// deathGuardRefused: repetir o Start sem a guarda so quando ela foi
// aplicada e o erro e EPERM (sandbox como o AWS Lambda recusa o prctl) —
// a mesma regra de internal/ext/process_spawn.go.
func deathGuardRefused(guarded bool, err error) bool {
	return guarded && errors.Is(err, syscall.EPERM)
}
