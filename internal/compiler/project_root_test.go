package compiler

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"os"
	"path/filepath"
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
