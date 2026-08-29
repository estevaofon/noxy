// internal/ext/exttest/processguest.go
package exttest

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	procMu   sync.Mutex
	procPath string
)

// BuildProcessGuest compila testdata/processguest (modulo aninhado que usa
// o SDK via replace) uma vez por processo de teste e devolve o caminho do
// executavel. Roda o binario uma vez logo apos o build (warm, abaixo) so
// para confirmar que ele ainda esta la: na maquina do dono um .exe
// recem-compilado pode ser apagado pelo antivirus nos primeiros segundos —
// se sumir, reconstroi uma vez antes de falhar.
func BuildProcessGuest(tb testing.TB) string {
	tb.Helper()
	procMu.Lock()
	defer procMu.Unlock()
	if procPath != "" {
		if _, err := os.Stat(procPath); err == nil {
			return procPath
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		path := buildProcessGuest(tb)
		if warm(path) == nil {
			procPath = path
			return path
		}
	}
	tb.Fatal("exttest: processguest binary vanished right after build, twice")
	return ""
}

func buildProcessGuest(tb testing.TB) string {
	tb.Helper()
	dir, err := os.MkdirTemp("", "noxy-processguest-")
	if err != nil {
		tb.Fatal(err)
	}
	name := "processguest"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = filepath.Join(repoRoot(tb), "internal", "ext", "testdata", "processguest")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("exttest: go build processguest: %v\n%s", err, output)
	}
	return out
}

// warm roda o binario com stdin vazio. O SDK espera o HELLO do host antes de
// mais nada: sem host do outro lado, ele ve EOF esperando o HELLO e sai com
// erro de protocolo (nao 0) — isso prova que o binario RODOU, so nao fala
// sozinho. So um erro ao INICIAR o processo (arquivo sumido, permissao
// negada pela antivirus) e o sinal de que o .exe recem-compilado sumiu.
func warm(path string) error {
	cmd := exec.Command(path)
	cmd.Stdin = strings.NewReader("")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err == nil || errors.As(err, &exitErr) {
		return nil
	}
	return err
}
