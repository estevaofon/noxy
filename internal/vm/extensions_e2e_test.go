package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/pkgmanager"
	"noxy-vm/internal/value"
)

const testExtManifest = `
name = "guest"
abi = 1

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
`

const testExtWrapper = `
func sha(data: bytes) -> bytes
    return guest_sha256(data)
end
`

func writeExtensionPackage(t *testing.T, root string) {
	t.Helper()
	pkg := filepath.Join(root, "noxy_libs", "guest")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(pkg, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("noxy_ext.toml", []byte(testExtManifest))
	writeFile("ext.wasm", exttest.BuildGuest(t, ""))
	writeFile("guest.nx", []byte(testExtWrapper))
}

func TestExtensionEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeExtensionPackage(t, root)
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	captured := captureVMSourceAtRoot(t, root, `
use guest as g
test_report(g.sha(to_bytes("abc")))
`)
	if captured.Type != value.VAL_BYTES || len(captured.Obj.(string)) != 32 {
		t.Fatalf("expected 32 sha bytes, got %#v", captured)
	}
}

func TestExtensionFailureIsRuntimeError(t *testing.T) {
	root := t.TempDir()
	writeExtensionPackage(t, root)
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	machine := NewWithConfig(VMConfig{RootPath: root})
	code := compileVMSourceAtRoot(t, root, `
use guest as g
let x: any = guest_fail()
`)
	err := machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "boom from guest") {
		t.Fatalf("guest failure must surface as runtime error, got %v", err)
	}
}

// testCollidingExtManifest declara um export cujo nome (time_now) ja existe
// como native de stdlib (builtins_time.go) — a extensao "time" nunca chega a
// registrar nada.
const testCollidingExtManifest = `
name = "time"
abi = 1

[[export]]
name = "time_now"
params = []
returns = "any"
`

func writeCollidingExtensionPackage(t *testing.T, root string) {
	t.Helper()
	pkg := filepath.Join(root, "noxy_libs", "time")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "noxy_ext.toml"), []byte(testCollidingExtManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// Os exports do wasm nao importam aqui: a pre-checagem de colisao de
	// nome dispara antes de ext.LoadModule sequer ser chamado.
	if err := os.WriteFile(filepath.Join(pkg, "ext.wasm"), exttest.BuildGuest(t, ""), 0o644); err != nil {
		t.Fatal(err)
	}
	// time.nx precisa existir (mesmo vazio) para o resolver tratar o pacote
	// como resolvedFileModule (nao resolvedDirectoryModule) — e so nesse caso
	// o hook de deteccao de extensao roda antes do arquivo ser lido.
	if err := os.WriteFile(filepath.Join(pkg, "time.nx"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionExportNameCollisionIsRejected(t *testing.T) {
	root := t.TempDir()
	writeCollidingExtensionPackage(t, root)
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	machine := NewWithConfig(VMConfig{RootPath: root})
	code := compileVMSourceAtRoot(t, root, `
use time as tm
`)
	err := machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "time_now") || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("expected a collision error mentioning time_now, got %v", err)
	}
}

func TestExtensionSumMismatchRefusesLoad(t *testing.T) {
	root := t.TempDir()
	writeExtensionPackage(t, root)
	// A E2E instala em noxy_libs/guest; um noxy.sum com hash errado para o
	// ext.wasm deve recusar a carga (spec §8).
	sum := "guest ext.wasm sha256:0000000000000000000000000000000000000000000000000000000000000000\n"
	if err := os.WriteFile(filepath.Join(root, "noxy.sum"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	machine := NewWithConfig(VMConfig{RootPath: root})
	code := compileVMSourceAtRoot(t, root, "use guest as g\n")
	err := machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("sum mismatch must refuse load, got %v", err)
	}
}

// Ida-e-volta escritor→leitor: grava via o MESMO RecordExtensionSums do
// --get, adultera o artefato, e exige que a carga acuse mismatch. So passa
// se caminho do noxy.sum E formato da chave coincidirem entre pkgmanager e
// vm (revisao do plano: divergencia cwd/RootPath falhava em silencio).
func TestExtensionSumRoundTripViaPkgmanager(t *testing.T) {
	root := t.TempDir()
	writeExtensionPackage(t, root)
	pkgDir := filepath.Join(root, "noxy_libs", "guest")
	if err := pkgmanager.RecordExtensionSums(root, pkgDir, "guest"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "ext.wasm"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	machine := NewWithConfig(VMConfig{RootPath: root})
	code := compileVMSourceAtRoot(t, root, "use guest as g\n")
	err := machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("writer/reader must agree on noxy.sum path and key, got %v", err)
	}
}

// Um atacante com acesso de escrita a noxy_libs/<pkg>/ pode trocar o wasm
// (evil.wasm) e apontar noxy_ext.toml para ele: a busca por (pkg,
// "evil.wasm") no noxy.sum nao acha entrada e cairia no TOFU-allow se o
// proprio manifesto nao fosse verificado primeiro (achado de revisao —
// bypass por rename do manifesto).
func TestExtensionSumManifestTamperRefusesLoad(t *testing.T) {
	root := t.TempDir()
	writeExtensionPackage(t, root)
	pkgDir := filepath.Join(root, "noxy_libs", "guest")
	if err := pkgmanager.RecordExtensionSums(root, pkgDir, "guest"); err != nil {
		t.Fatal(err)
	}
	origWasm, err := os.ReadFile(filepath.Join(pkgDir, "ext.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "evil.wasm"), origWasm, 0o644); err != nil {
		t.Fatal(err)
	}
	// A linha "wasm = ..." precisa entrar ANTES da primeira [[export]] — TOML
	// trataria uma chave solta apos uma tabela de array como pertencente a
	// ultima entrada de [[export]], nao ao nivel superior do manifesto.
	tampered := strings.Replace(testExtManifest, "abi = 1", "abi = 1\nwasm = \"evil.wasm\"", 1)
	if err := os.WriteFile(filepath.Join(pkgDir, "noxy_ext.toml"), []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })

	machine := NewWithConfig(VMConfig{RootPath: root})
	code := compileVMSourceAtRoot(t, root, "use guest as g\n")
	err = machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "mismatch") || !strings.Contains(err.Error(), "noxy_ext.toml") {
		t.Fatalf("manifest tamper (wasm rename) must refuse load with a noxy_ext.toml mismatch, got %v", err)
	}
}
