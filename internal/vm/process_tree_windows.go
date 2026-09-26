//go:build windows

package vm

import (
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// shellCommand ja preencheu SysProcAttr.CmdLine; nao ha grupo nem guarda
// no Start — o job object entra depois (newProcessTree).
func configureProcessGroup(*exec.Cmd) bool     { return false }
func configureProcessGroupUnguarded(*exec.Cmd) {}
func deathGuardRefused(bool, error) bool       { return false }

// processTree e um job object com KILL_ON_JOB_CLOSE: tudo o que o lider
// criar entra no job, terminate/kill terminam o job e fechar o handle
// (release, ou a morte do Noxy) mata o que restou (spec de design do
// modulo process, §4.2). Sem job (falha de API) sobra Process.Kill no
// lider. Mesmo mecanismo de internal/ext/process_spawn_windows.go.
type processTree struct {
	leader *os.Process
	job    windows.Handle
}

func newProcessTree(leader *os.Process) processTree {
	tree := processTree{leader: leader}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return tree
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return tree
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(leader.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return tree
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return tree
	}
	tree.job = job
	return tree
}

// signal: nao ha SIGTERM no Windows — terminate e kill terminam o job
// (exit code 1 no lider).
func (t processTree) signal(bool) bool {
	if t.job != 0 {
		return windows.TerminateJobObject(t.job, 1) == nil
	}
	return t.leader.Kill() == nil
}

func (t processTree) release() {
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
	}
}
