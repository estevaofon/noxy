package ext

import (
	"context"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
)

func testManifest(t *testing.T, concurrency string) *Manifest {
	t.Helper()
	m, err := ParseManifest([]byte(`
name = "guest"
abi = 1
concurrency = "` + concurrency + `"

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
`))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	return m
}

var wasiPermits = LoaderConfig{PermittedImports: []string{"wasi_snapshot_preview1"}}

func TestLoadModuleRejectsUngrantedImports(t *testing.T) {
	wasm := exttest.BuildGuest(t, "")
	_, err := LoadModule(context.Background(), wasm, testManifest(t, "single"), LoaderConfig{})
	if err == nil || !strings.Contains(err.Error(), "wasi_snapshot_preview1") {
		t.Fatalf("import gate must name the offending module, got %v", err)
	}
}

func TestLoadModuleHappyPath(t *testing.T) {
	wasm := exttest.BuildGuest(t, "")
	m, err := LoadModule(context.Background(), wasm, testManifest(t, "single"), wasiPermits)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer m.Close(context.Background())
}

func TestLoadModuleRejectsWrongABIVersion(t *testing.T) {
	wasm := exttest.BuildGuest(t, "-X main.abiVersionStr=99")
	_, err := LoadModule(context.Background(), wasm, testManifest(t, "single"), wasiPermits)
	if err == nil || !strings.Contains(err.Error(), "99") {
		t.Fatalf("handshake must report both versions, got %v", err)
	}
}

// Gate positivo: um modulo sem import NENHUM (o modulo wasm vazio de 8
// bytes) passa pelo gate e falha adiante, na checagem de exports — prova
// que o gate nao exige WASI nem host module para um guest limpo.
func TestLoadModuleImportGatePassesCleanModule(t *testing.T) {
	emptyWasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	_, err := LoadModule(context.Background(), emptyWasm, testManifest(t, "single"), LoaderConfig{})
	if err == nil || strings.Contains(err.Error(), "ungranted") {
		t.Fatalf("clean module must pass the import gate, got %v", err)
	}
	if !strings.Contains(err.Error(), "nx_abi_version") {
		t.Fatalf("expected missing-export error, got %v", err)
	}
}
