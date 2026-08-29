// internal/ext/process_spawn_other.go
//go:build !linux && !windows

package ext

import "os/exec"

// macOS e os demais nao tem sinal de morte do pai: vale a regra de EOF
// (spec §2.7, §4.5).
func applyDeathGuard(*exec.Cmd) {}

func attachJobObject(int) func() { return nil }
