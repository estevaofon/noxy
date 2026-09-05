package compiler

import (
	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/parser"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModuleResolvesThroughProjectRootFromSubdirectory verifica o espelho
// do compilador para a raiz do projeto (spec §3.0): um script num
// subdiretorio de um projeto com noxy.mod deve enxergar
// <projectRoot>/noxy_libs, nao so <moduleRoot>/noxy_libs.
func TestModuleResolvesThroughProjectRootFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	sub := filepath.Join(root, "noxy_examples")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noxy.mod"), []byte("module p\n\nrequire github.com/acme/pkg v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(root, "noxy_libs", "github_com", "acme", "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "pkg.nx"), []byte("func seven() -> int\n    return 7\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	source := "use github_com.acme.pkg.pkg select *\nlet n: int = seven()\n"
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	c := NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(sub, "main.nx"), sub)
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("compile error: %v", err)
	}
}

// TestWildcardModuleNotFoundHintsSyncWhenRequiredByNoxyMod: `use pkg select
// *` resolve em tempo de compilacao (discoverModuleExports), nunca chega ao
// resolveModule/syncHint da VM (spec §6) — sem o espelho em
// module_exports.go, um pacote faltando nesta forma falhava com a mensagem
// generica "failed to resolve wildcard module", sem apontar 'noxy --sync'.
func TestWildcardModuleNotFoundHintsSyncWhenRequiredByNoxyMod(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	sub := filepath.Join(root, "noxy_examples")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noxy.mod"), []byte("module p\n\nrequire github.com/acme/pkg v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nenhum pacote em noxy_libs: o require existe, mas nada foi sincronizado.

	compileAt := func(source string) error {
		l := lexer.New(source)
		p := parser.New(l)
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatalf("parser errors: %v", p.Errors())
		}
		c := NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(sub, "main.nx"), sub)
		_, _, err := c.Compile(program)
		return err
	}

	err := compileAt("use github_com.acme.pkg.pkg select *\n")
	if err == nil || !strings.Contains(err.Error(), "(required by noxy.mod) — run 'noxy --sync'") {
		t.Fatalf("got %v", err)
	}

	err = compileAt("use github_com.other.thing select *\n")
	if err == nil || strings.Contains(err.Error(), "noxy --sync") {
		t.Fatalf("unrelated module keeps the plain message, got %v", err)
	}
}
