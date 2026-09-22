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

// execSpawner executa o binario pelo caminho absoluto, sem argumentos, com
// o ambiente e o diretorio do host; stderr passa direto (spec §2.1).
//
// A guarda de morte da plataforma (pdeathsig no Linux) e best effort:
// sandboxes como o AWS Lambda recusam o prctl com EPERM, e o Go devolve
// esse errno como erro do proprio fork/exec. Nesse caso o binario e
// iniciado de novo sem a guarda — a regra de EOF (spec §4.5) continua
// sendo a guarda principal.
func execSpawner(path string) spawnFunc {
	return func(ctx context.Context) (procConn, error) {
		// spec §2.1: caminho absoluto, nunca busca no PATH — exec.Command
		// procuraria no PATH um nome sem separador.
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("extension binary path %q is not absolute", path)
		}
		cmd, stdin, stdout, err := startPlugin(path, true)
		if err != nil && deathGuardRefused(err) {
			cmd, stdin, stdout, err = startPlugin(path, false)
		}
		if err != nil {
			return nil, err
		}
		return &execConn{cmd: cmd, stdin: stdin, stdout: stdout, release: attachJobObject(cmd.Process.Pid)}, nil
	}
}

// startPlugin monta e inicia o comando; pipes pertencem ao comando, entao
// uma nova tentativa precisa de um comando novo.
func startPlugin(path string, deathGuard bool) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
	cmd := exec.Command(path)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr
	if deathGuard {
		applyDeathGuard(cmd)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	return cmd, stdin, stdout, nil
}

// deathGuardRefused diz se um erro de Start veio do sandbox recusando a
// guarda de morte: so faz sentido onde a plataforma aplica uma
// (hasDeathGuard) e so para EPERM — ENOENT, EACCES e os demais sao do
// binario, nao da guarda.
func deathGuardRefused(err error) bool {
	return hasDeathGuard && errors.Is(err, syscall.EPERM)
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
