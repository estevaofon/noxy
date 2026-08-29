// internal/ext/process_spawn.go
package ext

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
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
func execSpawner(path string) spawnFunc {
	return func(ctx context.Context) (procConn, error) {
		cmd := exec.Command(path)
		cmd.Env = os.Environ()
		cmd.Stderr = os.Stderr
		applyDeathGuard(cmd)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return &execConn{cmd: cmd, stdin: stdin, stdout: stdout, release: attachJobObject(cmd.Process.Pid)}, nil
	}
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
