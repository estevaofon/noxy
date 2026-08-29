package vm

import (
	"strings"
	"testing"
)

// Issue #61 item 3: um modulo carregado por `use` e compilado em RUNTIME
// (modules.go) — o aviso do compilador que ele gerar e diagnostico da VM e
// vai para stderr (AGENTS.md, regra "Saida"), nunca para stdout nem para o silencio.

func TestModuleCompileWarningsGoToStderr(t *testing.T) {
	root := t.TempDir()
	write(t, root, "avisos.nx", "func rebind(r: ref int)\n    let y: int = 10\n    r = ref y\nend\n")
	var stdout string
	stderr := captureConcurrencyStderr(t, func() {
		stdout = captureConcurrencyStdout(t, func() {
			got := captureVMSourceAtRoot(t, root, "use avisos\ntest_report(1)\n")
			if got.Int() != 1 {
				t.Errorf("programa nao rodou ate o fim: %v", got)
			}
		})
	})
	want := "warning: rebinding ref parameter 'r' has no effect outside function\n  --> "
	if !strings.Contains(stderr, want) || !strings.Contains(stderr, "avisos.nx:3") {
		t.Fatalf("stderr = %q, want aviso com arquivo:linha do modulo", stderr)
	}
	if strings.Contains(stdout, "warning") {
		t.Fatalf("aviso vazou para stdout: %q", stdout)
	}
}
