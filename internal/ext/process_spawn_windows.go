// internal/ext/process_spawn_windows.go
//go:build windows

package ext

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func applyDeathGuard(*exec.Cmd) {}

// attachJobObject poe o filho num job object com KILL_ON_JOB_CLOSE: o
// handle do job morre com o host e o kernel mata o filho (spec §4.5). Best
// effort — qualquer falha devolve nil e fica a regra de EOF.
func attachJobObject(pid int) func() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	return func() { _ = windows.CloseHandle(job) }
}
