package vm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

const testProcessExtManifest = `
name = "guest"
abi = 1
kind = "process"

[binaries]
%s = "%s"

[[export]]
name = "guest_add"
params = ["int", "int"]
returns = "int"

[[export]]
name = "guest_fail"
params = ["string"]
returns = "void"

[[export]]
name = "guest_pid"
params = []
returns = "int"

[[export]]
name = "guest_exit"
params = ["int"]
returns = "void"
`

const testProcessExtWrapper = `
func add(a: int, b: int) -> int
    return guest_add(a, b)
end

func pid() -> int
    return guest_pid()
end
`

// writeProcessExtensionPackage instala noxy_libs/guest com o guest do SDK
// copiado para bin/<asset>; devolve o nome do asset.
func writeProcessExtensionPackage(t *testing.T, root string) string {
	t.Helper()
	guest := exttest.BuildProcessGuest(t)
	asset := filepath.Base(guest)
	pkg := filepath.Join(root, "noxy_libs", "guest")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(testProcessExtManifest, runtime.GOOS+"-"+runtime.GOARCH, asset)
	if err := os.WriteFile(filepath.Join(pkg, "noxy_ext.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "guest.nx"), []byte(testProcessExtWrapper), 0o644); err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(guest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "bin", asset), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	return asset
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestProcessExtensionEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeProcessExtensionPackage(t, root)
	got := captureVMSourceAtRoot(t, root, `
use guest as g
test_report(g.add(2, 3))
`)
	if got.Type != value.VAL_INT || got.Int() != 5 {
		t.Fatalf("expected 5 through the process extension, got %#v", got)
	}
}

func TestProcessExtensionFailureIsRuntimeError(t *testing.T) {
	root := t.TempDir()
	writeProcessExtensionPackage(t, root)
	machine := NewWithConfig(VMConfig{RootPath: root})
	t.Cleanup(machine.CloseExtensions)
	code := compileVMSourceAtRoot(t, root, `
use guest as g
guest_fail("boom")
`)
	err := machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' failed: boom") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessExtensionMissingBinaryErrorsAtUse(t *testing.T) {
	root := t.TempDir()
	writeProcessExtensionPackage(t, root)
	if err := os.RemoveAll(filepath.Join(root, "noxy_libs", "guest", "bin")); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	err := machine.Interpret(compileVMSourceAtRoot(t, root, "use guest as g\n"))
	if err == nil || !strings.Contains(err.Error(), "run 'noxy --get'") {
		t.Fatalf("missing binary must fail at use with a --get hint, got %v", err)
	}
}

func TestProcessExtensionUnpublishedPlatformErrorsAtUse(t *testing.T) {
	root := t.TempDir()
	writeProcessExtensionPackage(t, root)
	manifest := fmt.Sprintf(testProcessExtManifest, "plan9-mips", "guest-plan9-mips")
	if err := os.WriteFile(filepath.Join(root, "noxy_libs", "guest", "noxy_ext.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	err := machine.Interpret(compileVMSourceAtRoot(t, root, "use guest as g\n"))
	if err == nil || !strings.Contains(err.Error(), "has no binary for "+runtime.GOOS+"/"+runtime.GOARCH) || !strings.Contains(err.Error(), "plan9/mips") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessExtensionSumMismatchRefusesLoad(t *testing.T) {
	root := t.TempDir()
	asset := writeProcessExtensionPackage(t, root)
	sum := "guest bin/" + asset + " sha256:0000000000000000000000000000000000000000000000000000000000000000\n"
	if err := os.WriteFile(filepath.Join(root, "noxy.sum"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	err := machine.Interpret(compileVMSourceAtRoot(t, root, "use guest as g\n"))
	if err == nil || !strings.Contains(err.Error(), "mismatch") || !strings.Contains(err.Error(), "bin/"+asset) {
		t.Fatalf("got %v", err)
	}
}

func TestProcessExtensionSumMatchLoads(t *testing.T) {
	root := t.TempDir()
	asset := writeProcessExtensionPackage(t, root)
	pkg := filepath.Join(root, "noxy_libs", "guest")
	sum := "guest noxy_ext.toml sha256:" + sha256File(t, filepath.Join(pkg, "noxy_ext.toml")) + "\n" +
		"guest bin/" + asset + " sha256:" + sha256File(t, filepath.Join(pkg, "bin", asset)) + "\n"
	if err := os.WriteFile(filepath.Join(root, "noxy.sum"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	got := captureVMSourceAtRoot(t, root, "use guest as g\ntest_report(g.add(1, 1))\n")
	if got.Int() != 2 {
		t.Fatalf("verified load must work: %#v", got)
	}
}
