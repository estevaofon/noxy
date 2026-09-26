//go:build windows

package vm

import (
	"os/exec"
	"syscall"
)

// shellCommand monta o processo que sys.exec e sys.exec_output rodam no
// Windows: `cmd /S /C "<linha>"` com a linha de comando montada por nos
// (SysProcAttr.CmdLine), nao pelo exec.Command. Com argv, o Go escapa cada
// `"` do comando como `\"` (syscall.EscapeArg); o cmd tira so as aspas
// externas e repassa os `\"` ao programa, que os le como aspas literais —
// `echo "a b"` imprimia `\"a b\"` e `git -C "D:\pasta com espaco"` chegava
// ao git em quatro pedacos (achado 10 do Noxy-Editor). Com /S o cmd remove
// a primeira e a ultima aspa da linha e entrega o resto intacto, entao
// aspas dentro do comando chegam ao programa como o autor escreveu.
func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + command + `"`}
	return cmd
}
