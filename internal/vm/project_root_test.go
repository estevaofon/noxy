package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/pkgmanager"
)

func writeProject(t *testing.T) (root, sub string) {
	t.Helper()
	root = t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	sub = filepath.Join(root, "noxy_examples")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noxy.mod"), []byte("module p\n\nrequire github.com/acme/pkg v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, sub
}

func TestUseResolvesThroughProjectRootFromSubdirectory(t *testing.T) {
	root, sub := writeProject(t)
	pkg := filepath.Join(root, "noxy_libs", "github_com", "acme", "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "pkg.nx"), []byte("func seven() -> int\n    return 7\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: sub})
	if machine.Config.ProjectRoot != root {
		t.Fatalf("ProjectRoot = %q, want %q", machine.Config.ProjectRoot, root)
	}
	got := captureVMSourceAtRoot(t, sub, "use github_com.acme.pkg.pkg as p\ntest_report(p.seven())\n")
	if got.Int() != 7 {
		t.Fatalf("got %#v", got)
	}
}

func TestModuleNotFoundHintsSyncWhenRequiredByNoxyMod(t *testing.T) {
	_, sub := writeProject(t)
	machine := NewWithConfig(VMConfig{RootPath: sub})
	err := machine.Interpret(compileVMSourceAtRoot(t, sub, "use github_com.acme.pkg.pkg as p\n"))
	if err == nil || !strings.Contains(err.Error(), "module not found: github_com.acme.pkg.pkg (required by noxy.mod) — run 'noxy --sync'") {
		t.Fatalf("got %v", err)
	}
	err = machine.Interpret(compileVMSourceAtRoot(t, sub, "use github_com.other.thing as x\n"))
	if err == nil || strings.Contains(err.Error(), "noxy --sync") {
		t.Fatalf("unrelated module keeps the plain message, got %v", err)
	}
}

func TestExtensionSumIsVerifiedForScriptInSubdirectory(t *testing.T) {
	root, sub := writeProject(t)
	// pacote de extensao em <root>/noxy_libs/guest, noxy.sum na raiz, script em sub/
	writeExtensionPackage(t, root)
	pkgDir := filepath.Join(root, "noxy_libs", "guest")
	if err := pkgmanager.RecordExtensionSums(root, pkgDir, "guest", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "ext.wasm"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })
	machine := NewWithConfig(VMConfig{RootPath: sub})
	err := machine.Interpret(compileVMSourceAtRoot(t, sub, "use guest as g\n"))
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("a script in a subdirectory must be verified against the project's noxy.sum (spec §3.0), got %v", err)
	}
}
