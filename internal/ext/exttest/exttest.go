// Package exttest compila o guest de teste (testdata/guest) para
// wasip1/wasm sob demanda, para que os testes de internal/ext e
// internal/vm nao precisem de toolchain alem do proprio Go.
package exttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	mu    sync.Mutex
	cache = map[string][]byte{}
)

// unsupportedToolchainMarkers sao trechos de saida do "go build" que indicam
// que o toolchain local nao suporta wasip1/wasmexport, em vez de um erro no
// guest em si. Mantido estreito de proposito: qualquer outra falha continua
// derrubando o teste com tb.Fatalf.
var unsupportedToolchainMarkers = []string{
	"unsupported GOOS/GOARCH",
	"invalid buildmode",
	"//go:wasmexport",
}

func isUnsupportedToolchainOutput(output string) bool {
	for _, marker := range unsupportedToolchainMarkers {
		if strings.Contains(output, marker) {
			return true
		}
	}
	return false
}

// repoRoot sobe a partir do diretorio do pacote em teste (o cwd de todo
// "go test") ate o go.mod deste modulo. Nao usa runtime.Caller: com
// GOFLAGS=-trimpath (configuracao de maquina) o caminho gravado no binario
// e relativo ao modulo ("github.com/estevaofon/noxy/...") e o chdir do go build dos guests
// falhava.
func repoRoot(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	if err != nil {
		tb.Fatalf("exttest: getwd: %v", err)
	}
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
			first := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
			if first == "module github.com/estevaofon/noxy" {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("exttest: go.mod of module github.com/estevaofon/noxy not found above the test directory")
		}
		dir = parent
	}
}

// BuildGuest compila testdata/guest com os ldflags dados e devolve os bytes
// do .wasm. O resultado e cacheado por ldflags dentro do processo.
//
// -buildmode=c-shared e o modo documentado para modulos reactor com
// //go:wasmexport em wasip1/wasm no Go 1.24; sem ele o linker falha ao
// resolver os exports.
func BuildGuest(tb testing.TB, ldflags string) []byte {
	tb.Helper()
	mu.Lock()
	defer mu.Unlock()
	if data, ok := cache[ldflags]; ok {
		return data
	}
	root := repoRoot(tb)
	out := filepath.Join(tb.TempDir(), "guest.wasm")
	args := []string{"build", "-buildmode=c-shared", "-o", out}
	if ldflags != "" {
		args = append(args, "-ldflags", ldflags)
	}
	args = append(args, "./internal/ext/testdata/guest")
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isUnsupportedToolchainOutput(string(output)) {
			tb.Skipf("exttest: toolchain local sem suporte a wasip1/wasmexport: %v\n%s", err, output)
		}
		tb.Fatalf("exttest: go build guest: %v\n%s", err, output)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		tb.Fatalf("exttest: read guest: %v", err)
	}
	cache[ldflags] = data
	return data
}
