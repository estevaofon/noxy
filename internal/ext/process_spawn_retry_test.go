// internal/ext/process_spawn_retry_test.go
package ext

import (
	"os"
	"syscall"
	"testing"
)

// deathGuardRefused: so EPERM do Start, e so quando a guarda de morte foi
// aplicada, justifica repetir o Start sem ela (AWS Lambda recusa o prctl
// de pdeathsig). Erros do binario em si nunca disparam a repeticao.
func TestDeathGuardRefused(t *testing.T) {
	eperm := &os.PathError{Op: "fork/exec", Path: "/var/task/plugin", Err: syscall.EPERM}
	if !deathGuardRefused(true, eperm) {
		t.Fatal("EPERM with the guard applied must retry")
	}
	if deathGuardRefused(false, eperm) {
		t.Fatal("EPERM without a guard is the binary's own error: no retry")
	}
	for name, err := range map[string]error{
		"ENOENT": &os.PathError{Op: "fork/exec", Path: "/x", Err: syscall.ENOENT},
		"EACCES": &os.PathError{Op: "fork/exec", Path: "/x", Err: syscall.EACCES},
		"nil":    nil,
	} {
		if deathGuardRefused(true, err) {
			t.Fatalf("%s must not trigger the retry", name)
		}
	}
}
