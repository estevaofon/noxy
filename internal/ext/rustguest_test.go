package ext

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

//go:embed testdata/rustguest/rustguest.wasm
var rustGuestWasm []byte

func rustManifest(tb testing.TB) *Manifest {
	tb.Helper()
	m, err := ParseManifest([]byte(`
name = "rust"
abi = 1
concurrency = "single"

[[export]]
name = "rust_echobytes"
params = ["bytes"]
returns = "bytes"

[[export]]
name = "rust_fail"
params = []
returns = "any"

[[export]]
name = "rust_trap"
params = []
returns = "any"

[[export]]
name = "rust_sha256"
params = ["bytes"]
returns = "bytes"
`))
	if err != nil {
		tb.Fatalf("manifest: %v", err)
	}
	return m
}

// Gate positivo REAL: guest sem WASI carrega com LoaderConfig{} — nenhum
// import fora de noxy:host/v1.
func loadRustGuest(tb testing.TB) *Module {
	tb.Helper()
	m, err := LoadModule(context.Background(), rustGuestWasm, rustManifest(tb), LoaderConfig{})
	if err != nil {
		tb.Fatalf("load rust guest without permits: %v", err)
	}
	tb.Cleanup(func() { m.Close(context.Background()) })
	return m
}

func TestRustGuestLoadsWithoutPermits(t *testing.T) {
	loadRustGuest(t)
}

func TestRustGuestEchoBytes(t *testing.T) {
	m := loadRustGuest(t)
	got, err := m.Call(context.Background(), 0, []value.Value{value.NewBytes("héllo")})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if got.Type != value.VAL_BYTES || got.Obj.(string) != "héllo" {
		t.Fatalf("echo: %#v", got)
	}
}

func TestRustGuestFailAndTrap(t *testing.T) {
	m := loadRustGuest(t)
	_, err := m.Call(context.Background(), 1, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'rust' failed: boom from rust guest") {
		t.Fatalf("fail: %v", err)
	}
	_, err = m.Call(context.Background(), 2, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'rust' trapped") {
		t.Fatalf("trap: %v", err)
	}
}

func TestRustGuestSha256MatchesNative(t *testing.T) {
	m := loadRustGuest(t)
	arg := value.NewBytes("abc")
	got, err := m.Call(context.Background(), 3, []value.Value{arg})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// O guest faz sha256 do payload cru de args (mesma convencao do guest Go).
	raw, _ := EncodeArgs([]value.Value{arg}, DefaultLimits())
	want := sha256.Sum256(raw)
	if got.Type != value.VAL_BYTES || got.Obj.(string) != string(want[:]) {
		t.Fatalf("sha256 mismatch")
	}
}

// Numeros honestos da spec §11 para um guest de qualidade nativa (compare
// com BenchmarkExt* do guest Go).
func BenchmarkRustRoundTrip1KB(b *testing.B) {
	m := loadRustGuest(b)
	payload := value.NewBytes(strings.Repeat("a", 1024))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Call(context.Background(), 0, []value.Value{payload}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRustSHA256_1MB(b *testing.B) {
	m := loadRustGuest(b)
	payload := value.NewBytes(strings.Repeat("a", 1<<20))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Call(context.Background(), 3, []value.Value{payload}); err != nil {
			b.Fatal(err)
		}
	}
}
