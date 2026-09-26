//go:build unix && !linux

package vm

import "os/exec"

// Sem Pdeathsig fora do Linux: so o grupo (spec de design do modulo
// process, §4.3).
func configureProcessGroup(cmd *exec.Cmd) bool {
	configureProcessGroupUnguarded(cmd)
	return false
}

func deathGuardRefused(bool, error) bool { return false }
