// Package exttest compila o guest de teste (testdata/guest) para
// wasip1/wasm sob demanda, para que os testes de internal/ext e
// internal/vm nao precisem de toolchain alem do proprio Go.
package exttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	mu    sync.Mutex
	cache = map[string][]byte{}
)

func repoRoot(tb testing.TB) string {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("exttest: cannot locate caller")
	}
	// internal/ext/exttest/exttest.go -> raiz do repo
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(self))))
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
		tb.Fatalf("exttest: go build guest: %v\n%s", err, output)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		tb.Fatalf("exttest: read guest: %v", err)
	}
	cache[ldflags] = data
	return data
}
