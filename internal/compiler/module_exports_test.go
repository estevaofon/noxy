package compiler

import (
	"github.com/estevaofon/noxy/internal/ast"
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

// C1: loadModuleDeclarations e memoizado por moduleDiscoveryState, e o
// estado vive no compilador (discoveryState()) em vez de ser fabricado
// descartavel a cada chamada. Sem o memo, um unico `use` pagava um parse +
// um Compile completo de validacao POR CHAMADOR — predeclareImportedTemplates
// (discoverModuleExports + moduleTopLevelBindings), predeclareImport e o case
// *ast.UseStmt de compiler.go —, tudo dobrado pelo two-pass dos genericos:
// `use http select *` foi de 1,4s para 12,3s de startup.
//
// Duas provas: identidade de ponteiro do AST devolvido e sobrevivencia a
// remocao do arquivo entre as duas chamadas (um reparse falharia).
func TestModuleDeclarationsAreMemoizedPerCompiler(t *testing.T) {
	root := t.TempDir()
	modulePath := filepath.Join(root, "dep.nx")
	if err := os.WriteFile(modulePath, []byte("func delete(url: string) -> void\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))

	first, _, loadable := compiler.loadModuleDeclarations("dep", compiler.discoveryState())
	if !loadable || first == nil {
		t.Fatal("primeira carga de 'dep' falhou")
	}
	if err := os.Remove(modulePath); err != nil {
		t.Fatal(err)
	}

	second, _, loadable := compiler.loadModuleDeclarations("dep", compiler.discoveryState())
	if !loadable {
		t.Fatal("segunda carga foi ao disco (arquivo removido) em vez de acertar o memo")
	}
	if first != second {
		t.Fatal("segunda carga devolveu outro AST — o memo nao e compartilhado entre chamadas")
	}
	exports, loadable := compiler.discoverModuleExports("dep")
	if !loadable {
		t.Fatal("discoverModuleExports nao aproveitou o memo")
	}
	if _, hasDelete := exports["delete"]; !hasDelete {
		t.Fatalf("exports memoizados = %v, quer 'delete'", exports)
	}
}

// C1, segunda metade: o compilador descartavel do pass 1 dos genericos
// compartilha o cache com o pass 2 (newPass1Compiler usa discoveryState()).
// Se cada passada tivesse cache proprio, todo modulo importado seria
// carregado duas vezes num programa com genericos. A prova e de identidade: o
// template registrado no fim da compilacao aponta para uma declaracao DENTRO
// do Program memoizado, ou seja, so existiu um AST do modulo.
func TestGenericsTwoPassSharesModuleCache(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dep.nx"), []byte("func ident<T>(x: T) -> T\n    return x\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	compiler := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"))
	if _, _, err := compiler.Compile(parse("use dep select ident\nident(1)")); err != nil {
		t.Fatal(err)
	}

	cached, memoized := compiler.discoveryState().loaded["dep"]
	if !memoized || cached.program == nil {
		t.Fatal("modulo 'dep' nao ficou memoizado apos a compilacao")
	}
	template, registered := compiler.registryOrInit().Funcs["ident"]
	if !registered {
		t.Fatal("template importado 'ident' nao chegou ao registry")
	}
	found := false
	for _, statement := range cached.program.Statements {
		if statement == ast.Statement(template.Decl) {
			found = true
		}
	}
	if !found {
		t.Fatal("o template registrado nao aponta para o AST memoizado — pass 1 e pass 2 recarregaram o modulo separadamente")
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
