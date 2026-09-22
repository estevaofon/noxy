// internal/ext/process_spawn.go
package ext

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
)

// execConn e o processo real do plugin. Wait e memoizado: die (leitor),
// expire (timeout) e Close podem todos esperar a mesma saida.
type execConn struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	waitOnce sync.Once
	waitErr  error
	release  func()
}

// deathGuard e a guarda de morte da plataforma (spec §4.5), aplicada ao
// comando antes do Start; devolve se aplicou alguma. Variavel para os
// testes injetarem uma guarda que o kernel recusa.
var deathGuard = applyDeathGuard

// execSpawner executa o binario pelo caminho absoluto, sem argumentos, com
// o ambiente e o diretorio do host; stderr passa direto (spec §2.1).
//
// A guarda de morte e best effort: sandboxes como o AWS Lambda recusam o
// prctl(PR_SET_PDEATHSIG) com EPERM, e o Go devolve esse errno como erro do
// proprio fork/exec. Nesse caso o binario e iniciado de novo sem a guarda,
// em silencio — uma linha por cold start no CloudWatch seria ruido, e a
// regra de EOF (spec §4.5) continua sendo a guarda principal.
func execSpawner(path string) spawnFunc {
	return func(ctx context.Context) (procConn, error) {
		// spec §2.1: caminho absoluto, nunca busca no PATH — exec.Command
		// procuraria no PATH um nome sem separador.
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("extension binary path %q is not absolute", path)
		}
		cmd, stdin, stdout, guarded, err := startPlugin(path, deathGuard)
		if err != nil && deathGuardRefused(guarded, err) {
			cmd, stdin, stdout, _, err = startPlugin(path, nil)
		}
		if err != nil {
			return nil, err
		}
		return &execConn{cmd: cmd, stdin: stdin, stdout: stdout, release: attachJobObject(cmd.Process.Pid)}, nil
	}
}

// startPlugin monta e inicia o comando; pipes pertencem ao comando, entao
// uma nova tentativa precisa de um comando novo. guard nil = sem guarda.
func startPlugin(path string, guard func(*exec.Cmd) bool) (cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, guarded bool, err error) {
	cmd = exec.Command(path)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr
	if guard != nil {
		guarded = guard(cmd)
	}
	if stdin, err = cmd.StdinPipe(); err != nil {
		return nil, nil, nil, guarded, err
	}
	if stdout, err = cmd.StdoutPipe(); err != nil {
		return nil, nil, nil, guarded, err
	}
	if err = cmd.Start(); err != nil {
		return nil, nil, nil, guarded, err
	}
	return cmd, stdin, stdout, guarded, nil
}

// deathGuardRefused diz se vale repetir o Start sem a guarda: so quando ela
// foi aplicada e o erro e EPERM — o errno observado no Lambda. EPERM tambem
// pode vir do proprio execve (binario setuid em mount nosuid); nesse caso a
// repeticao e um fork a mais que falha do mesmo jeito, e o segundo erro e o
// devolvido. Outros errnos (ENOSYS, EINVAL) nao repetem: nunca foram vistos,
// e um sandbox que os devolva mantem o sintoma antigo — decisao registrada
// aqui para quem encontrar um.
func deathGuardRefused(guarded bool, err error) bool {
	return guarded && errors.Is(err, syscall.EPERM)
}

func (c *execConn) Stdin() io.WriteCloser { return c.stdin }
func (c *execConn) Stdout() io.Reader     { return c.stdout }

func (c *execConn) Wait() error {
	c.waitOnce.Do(func() {
		c.waitErr = c.cmd.Wait()
		if c.release != nil {
			c.release()
		}
	})
	return c.waitErr
}

func (c *execConn) Kill() error { return c.cmd.Process.Kill() }
