package ext

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

// benchModule reusa o manifesto de testManifest (Task 4) em vez de duplicar
// o literal TOML — testManifest ja aceita testing.TB desde essa mudanca.
func benchModule(b *testing.B) *Module {
	b.Helper()
	m, err := LoadModule(context.Background(), exttest.BuildGuest(b, ""), testManifest(b, "single"),
		LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { m.Close(context.Background()) })
	return m
}

// Gate da spec §11: ida-e-volta (bytes 1KB) abaixo de 5 µs no runner amd64.
func BenchmarkExtRoundTrip1KB(b *testing.B) {
	m := benchModule(b)
	payload := value.NewBytes(strings.Repeat("a", 1024))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Call(context.Background(), 3, []value.Value{payload}); err != nil {
			b.Fatal(err)
		}
	}
}

// Gate da spec §11: sha256 de 1 MB no guest dentro de 3x do nativo.
func BenchmarkExtSHA256_1MB(b *testing.B) {
	m := benchModule(b)
	payload := value.NewBytes(strings.Repeat("a", 1<<20))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Call(context.Background(), 3, []value.Value{payload}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNativeSHA256_1MB(b *testing.B) {
	payload := []byte(strings.Repeat("a", 1<<20))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sha256.Sum256(payload)
	}
}

// Custo de LoadModule com o cache de compilacao persistente quente
// (revisao do plano: sem cache, todo `noxy script.nx` recompila o wasm —
// para CLI isso pode dominar scripts curtos). Registre no PR tambem o
// tempo FRIO: apague o diretorio de cache (os.UserCacheDir()/noxy/wazero),
// rode uma iteracao, e compare.
func BenchmarkLoadModuleWarm(b *testing.B) {
	wasm := exttest.BuildGuest(b, "")
	manifest := testManifest(b, "single")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m, err := LoadModule(context.Background(), wasm, manifest,
			LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}})
		if err != nil {
			b.Fatal(err)
		}
		m.Close(context.Background())
	}
}
