// internal/ext/process_spawn_retry_test.go
package ext

import (
	"os"
	"syscall"
	"testing"
)

// deathGuardRefused: so EPERM do Start, e so onde a plataforma aplica a
// guarda de morte, justifica repetir o Start sem ela (AWS Lambda recusa o
// prctl de pdeathsig). Erros do binario em si nunca disparam a repeticao.
func TestDeathGuardRefused(t *testing.T) {
	eperm := &os.PathError{Op: "fork/exec", Path: "/var/task/plugin", Err: syscall.EPERM}
	if got := deathGuardRefused(eperm); got != hasDeathGuard {
		t.Fatalf("EPERM: got %v, want hasDeathGuard=%v", got, hasDeathGuard)
	}
	for name, err := range map[string]error{
		"ENOENT": &os.PathError{Op: "fork/exec", Path: "/x", Err: syscall.ENOENT},
		"EACCES": &os.PathError{Op: "fork/exec", Path: "/x", Err: syscall.EACCES},
		"nil":    nil,
	} {
		if deathGuardRefused(err) {
			t.Fatalf("%s must not trigger the retry", name)
		}
	}
}
