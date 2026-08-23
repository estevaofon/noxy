package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
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
