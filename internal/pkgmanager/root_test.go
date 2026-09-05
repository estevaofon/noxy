package pkgmanager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRootWalksUpToNoxyMod(t *testing.T) {
	base := t.TempDir()
	base, _ = filepath.EvalSymlinks(base)
	if err := os.WriteFile(filepath.Join(base, "noxy.mod"), []byte("module p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(base, "noxy_examples", "sub")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, start := range []string{base, deep, filepath.Join(base, "noxy_examples")} {
		if root, ok := FindRoot(start); !ok || root != base {
			t.Fatalf("FindRoot(%s) = %q %v, want %s", start, root, ok, base)
		}
	}
	other := t.TempDir()
	if root, ok := FindRoot(other); ok {
		t.Fatalf("no noxy.mod above %s, got %q", other, root)
	}
}
