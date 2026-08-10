package compiler

import (
	"noxy-vm/internal/ast"
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedWildcardExportsIncludeDurableWrapperBindings(t *testing.T) {
	compiler := New()
	direct, loadable := compiler.discoverModuleExports("http_client")
	if !loadable {
		t.Fatal("http_client was not loadable")
	}
	if _, ok := direct["delete"]; !ok {
		t.Fatal("http_client direct delete export was not discovered")
	}
	wrapper, loadable := compiler.discoverModuleExports("http")
	if !loadable {
		t.Fatal("http wrapper was not loadable")
	}
	if _, ok := wrapper["delete"]; !ok {
		t.Fatal("http wildcard delete export was not discovered")
	}
}

func TestEmbeddedModuleTerminalCompilesWithTypedExports(t *testing.T) {
	compiler := New()
	exports, loadable := compiler.discoverModuleExports("terminal")
	if !loadable {
		t.Fatal("terminal was not loadable")
	}
	for _, name := range []string{"TerminalResult", "KeyEvent", "is_terminal", "open_raw", "read_key", "close"} {
		if _, ok := exports[name]; !ok {
			t.Fatalf("terminal export %q was not discovered", name)
		}
	}

	_, err := compileFunctionSource(t, `
use terminal
let available: bool = terminal.is_terminal()
let opened: terminal.TerminalResult = terminal.open_raw()
let event: terminal.KeyEvent = terminal.read_key()
let closed: bool = terminal.close()
`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFileModuleDirectExportsAreDiscovered(t *testing.T) {
	root := t.TempDir()
	modulePath := filepath.Join(root, "collision.nx")
	if err := os.WriteFile(modulePath, []byte("func delete(url: string) -> void\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
	exports, loadable := compiler.discoverModuleExports("collision")
	if !loadable {
		t.Fatal("direct file module was not loadable")
	}
	if _, ok := exports["delete"]; !ok {
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
	exports, loadable := compiler.discoverModuleExports("bundle")
	if !loadable {
		t.Fatal("directory module was not loadable")
	}
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
	if exports, loadable := compiler.discoverModuleExports("broken"); loadable || len(exports) != 0 {
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
			if exports, loadable := compiler.discoverModuleExports(tt.root); loadable || len(exports) != 0 {
				t.Fatalf("cyclic module exports=%v, want none", exports)
			}
		})
	}
}

func TestCompilerRejectsTopLevelWildcardImportCycles(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		dependency string
	}{
		{
			name: "self cycle",
			files: map[string]string{
				"cycle.nx": "use cycle select *\n",
			},
			dependency: "cycle",
		},
		{
			name: "mutual cycle",
			files: map[string]string{
				"left.nx":  "use right select *\n",
				"right.nx": "use left select *\n",
			},
			dependency: "left",
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
			t.Setenv("NOXY_PATH", root)
			_, err := compileFunctionSource(t, "use "+tt.dependency+" select *")
			if err == nil {
				t.Fatal("compiler accepted a cyclic top-level wildcard import")
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
	if exports, loadable := compiler.discoverModuleExports("collision"); loadable || len(exports) != 0 {
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
	if exports, loadable := compiler.discoverModuleExports("collision"); loadable || len(exports) != 0 {
		t.Fatalf("module with missing selected export exports=%v, want none", exports)
	}
}

func TestTopLevelWildcardDependencyMustBeLoadable(t *testing.T) {
	tests := []struct {
		name       string
		dependency string
		wantDelete bool
	}{
		{name: "missing", dependency: "definitely_missing_task5_module"},
		{name: "compile invalid", dependency: "broken"},
		{name: "valid empty", dependency: "empty", wantDelete: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "broken.nx"), []byte("let marker: int = \"wrong\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "empty.nx"), []byte("// deliberately empty\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			wrapper := "use " + tt.dependency + " select *\nfunc delete(url: string) -> void\nend\n"
			if err := os.WriteFile(filepath.Join(root, "wrapper.nx"), []byte(wrapper), 0o600); err != nil {
				t.Fatal(err)
			}

			compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
			exports, loadable := compiler.discoverModuleExports("wrapper")
			_, hasDelete := exports["delete"]
			if loadable != tt.wantDelete || hasDelete != tt.wantDelete {
				t.Fatalf("exports=%v, loadable=%v, delete present=%v, want %v", exports, loadable, hasDelete, tt.wantDelete)
			}
		})
	}
}

func TestFunctionBodyOnlyWildcardDoesNotAffectModuleLoadability(t *testing.T) {
	tests := []struct {
		name       string
		dependency string
	}{
		{name: "missing", dependency: "definitely_missing_task5_module"},
		{name: "self cycle", dependency: "safe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			source := "func unused() -> void\n    use " + tt.dependency + " select *\nend\nfunc delete(url: string) -> void\nend\n"
			if err := os.WriteFile(filepath.Join(root, "safe.nx"), []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
			exports, loadable := compiler.discoverModuleExports("safe")
			if !loadable {
				t.Fatal("function-body-only wildcard invalidated module loadability")
			}
			if _, ok := exports["delete"]; !ok {
				t.Fatalf("function-body-only wildcard invalidated module exports: %v", exports)
			}
		})
	}
}

func TestNestedModuleDiscoveryUsesProgramRoot(t *testing.T) {
	tests := []struct {
		name      string
		shadowDep string
	}{
		{name: "dependency only at root"},
		{name: "invalid divergent nested dependency", shadowDep: "let marker: int = \"wrong\"\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			pkgDir := filepath.Join(root, "pkg")
			if err := os.MkdirAll(pkgDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "dep.nx"), []byte("func delete(url: string) -> void\nend\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pkgDir, "wrapper.nx"), []byte("use dep select *\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.shadowDep != "" {
				if err := os.WriteFile(filepath.Join(pkgDir, "dep.nx"), []byte(tt.shadowDep), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
			exports, loadable := compiler.discoverModuleExports("pkg.wrapper")
			if !loadable {
				t.Fatal("nested wrapper was not loadable from the program root")
			}
			if _, ok := exports["delete"]; !ok {
				t.Fatalf("nested wrapper exports=%v, want root dependency delete", exports)
			}
		})
	}
}
