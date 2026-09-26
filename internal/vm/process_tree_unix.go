//go:build unix

package vm

import (
	"os"
	"os/exec"
	"syscall"
)

// processTree e o grupo de processos do lider (pgid == pid, Setpgid):
// terminate/kill alcancam a arvore inteira, nao so o shell (spec de design
// do modulo process, §4.2).
type processTree struct{ leader *os.Process }

func newProcessTree(leader *os.Process) processTree { return processTree{leader: leader} }

// signal envia SIGTERM (force=false) ou SIGKILL ao grupo; se o grupo ja
// nao existe, ao lider. false quando nada foi sinalizado.
func (t processTree) signal(force bool) bool {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(-t.leader.Pid, sig); err == nil {
		return true
	}
	return t.leader.Signal(sig) == nil
}

func (t processTree) release() {}

func configureProcessGroupUnguarded(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
