package compiler

import (
	"noxy-vm/internal/ast"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedWildcardExportsOnlyDirectModuleGlobals(t *testing.T) {
	compiler := New()
	direct := compiler.discoverModuleExports("http_client")
	if _, ok := direct["delete"]; !ok {
		t.Fatal("http_client direct delete export was not discovered")
	}
	wrapper := compiler.discoverModuleExports("http")
	if _, ok := wrapper["delete"]; ok {
		t.Fatal("http wildcard side effect was incorrectly modeled as a durable module export")
	}
}

func TestFileModuleDirectExportsAreDiscovered(t *testing.T) {
	root := t.TempDir()
	modulePath := filepath.Join(root, "collision.nx")
	if err := os.WriteFile(modulePath, []byte("func delete(url: string) -> void\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
	if _, ok := compiler.discoverModuleExports("collision")["delete"]; !ok {
		t.Fatal("direct file-module export was not discovered")
	}
}

func TestDirectoryModuleExportsOnlyLoadableChildren(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "unloadable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "delete.nx"), []byte("let marker: int = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "unloadable", "broken.nx"), []byte("let marker: int = \"wrong\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
	exports := compiler.discoverModuleExports("bundle")
	if _, ok := exports["delete"]; !ok {
		t.Fatal("loadable file child was not exported")
	}
	if _, ok := exports["unloadable"]; ok {
		t.Fatal("unloadable directory child was incorrectly exported")
	}
}

func TestDirectoryModuleWithUnloadableFileHasNoExports(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "broken")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "good.nx"), []byte("let marker: int = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "bad.nx"), []byte("let marker: int = \"wrong\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
	if exports := compiler.discoverModuleExports("broken"); len(exports) != 0 {
		t.Fatalf("unloadable directory module exports=%v, want none", exports)
	}
}

func TestModuleExportDiscoveryRejectsWildcardImportCycles(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		root  string
	}{
		{
			name: "self cycle",
			files: map[string]string{
				"cycle.nx": "use cycle select *\nfunc delete(url: string) -> void\nend\n",
			},
			root: "cycle",
		},
		{
			name: "mutual cycle",
			files: map[string]string{
				"left.nx":  "use right select *\nfunc delete(url: string) -> void\nend\n",
				"right.nx": "use left select *\nfunc helper() -> void\nend\n",
			},
			root: "left",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for name, source := range tt.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
			if exports := compiler.discoverModuleExports(tt.root); len(exports) != 0 {
				t.Fatalf("cyclic module exports=%v, want none", exports)
			}
		})
	}
}

func TestFileModuleWithMissingDirectImportHasNoExports(t *testing.T) {
	root := t.TempDir()
	source := "use definitely_missing_task5_module as dependency\nfunc delete(url: string) -> void\nend\n"
	if err := os.WriteFile(filepath.Join(root, "collision.nx"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
	if exports := compiler.discoverModuleExports("collision"); len(exports) != 0 {
		t.Fatalf("module with missing direct import exports=%v, want none", exports)
	}
}

func TestFileModuleWithMissingSelectedExportHasNoExports(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dependency.nx"), []byte("let present: int = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "use dependency select missing\nfunc delete(url: string) -> void\nend\n"
	if err := os.WriteFile(filepath.Join(root, "collision.nx"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
	if exports := compiler.discoverModuleExports("collision"); len(exports) != 0 {
		t.Fatalf("module with missing selected export exports=%v, want none", exports)
	}
}
