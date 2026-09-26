//go:build !windows

package vm

import "os/exec"

// shellCommand monta o processo que sys.exec e sys.exec_output rodam: a
// linha inteira vai para o shell da plataforma (spec §12, `sys`). Em Unix e
// `sh -c <linha>`, um unico argv — o shell faz o parse.
func shellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}
