// internal/ext/process_spawn_retry_linux_test.go
//go:build linux

package ext

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// O caminho de repeticao de verdade: uma guarda que o kernel recusa antes
// do exec (setuid para root sem privilegio devolve EPERM no filho, como o
// prctl recusado no Lambda) faz o primeiro Start falhar; execSpawner
// repete sem a guarda e o plugin auxiliar responde ao CALL. Como root o
// setuid(0) e permitido e a recusa nao se simula: o teste pula.
func TestExecSpawnerRetriesWithoutRefusedDeathGuard(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("setuid(0) succeeds as root; the refused guard cannot be simulated")
	}
	prev := deathGuard
	deathGuard = func(cmd *exec.Cmd) bool {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 0, Gid: 0}}
		return true
	}
	t.Cleanup(func() { deathGuard = prev })

	p := helperProcess(t, "single", io.Discard)
	got, err := p.Call(context.Background(), 0, []value.Value{value.NewString("hi")})
	if err != nil || got.Obj.(string) != "hi" {
		t.Fatalf("plugin must come up on the retry without the guard: %#v %v", got, err)
	}
}
