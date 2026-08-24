package ext

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

// benchManifest espelha o literal de testManifest (Task 4), mas com
// memory_max_mb no teto do host (256, vs o default de 64 usado pelos testes
// funcionais). Motivo: BenchmarkExtRoundTrip1KB e barato o bastante para o
// calibrador de -benchtime=2s escalar b.N para a casa das centenas de
// milhares de chamadas na mesma instancia "single" — nesse volume o
// alocador do guest (Go puro em wasip1, sem GC em segundo plano de verdade)
// nao reciclou memoria a tempo em alguns runs e o modulo afundou em
// runtime.throw (heap exhaustion) por volta de ~150k chamadas com o teto
// default. Nao duplicar isso no testManifest global: os testes funcionais
// nao fazem esse volume de chamadas e nao devem herdar um teto maior sem
// necessidade.
func benchManifest(b *testing.B) *Manifest {
	b.Helper()
	m, err := ParseManifest([]byte(`
name = "guest"
abi = 1
concurrency = "single"
memory_max_mb = 256

[[export]]
name = "guest_echo"
params = ["any"]
returns = "any"

[[export]]
name = "guest_fail"
params = []
returns = "any"

[[export]]
name = "guest_trap"
params = []
returns = "any"

[[export]]
name = "guest_sha256"
params = ["bytes"]
returns = "bytes"

[[export]]
name = "guest_loop"
params = []
returns = "any"

[[export]]
name = "guest_badtype"
params = []
returns = "int"

[[export]]
name = "guest_echobytes"
params = ["bytes"]
returns = "bytes"
`))
	if err != nil {
		b.Fatal(err)
	}
	return m
}

func benchModule(b *testing.B) *Module {
	b.Helper()
	m, err := LoadModule(context.Background(), exttest.BuildGuest(b, ""), benchManifest(b),
		LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { m.Close(context.Background()) })
	return m
}

// Gate da spec §11: ida-e-volta (bytes 1KB) abaixo de 5 µs no runner amd64.
// fn_index 6 e copia pura no guest (sem sha256): mede so o custo de
// fronteira (encode + call + decode), que e o que o gate de 5 µs cobre —
// ao contrario de BenchmarkExtSHA256_1MB abaixo, que inclui compute do
// guest de proposito (esse e o gate de 3x).
func BenchmarkExtRoundTrip1KB(b *testing.B) {
	m := benchModule(b)
	payload := value.NewBytes(strings.Repeat("a", 1024))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Call(context.Background(), 6, []value.Value{payload}); err != nil {
			b.Fatal(err)
		}
	}
}

// Gate da spec §11: sha256 de 1 MB no guest dentro de 3x do nativo. Ao
// contrario do benchmark acima, este inclui de proposito o compute do
// guest (sha256), pois e essa a comparacao que o gate de 3x pede.
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
