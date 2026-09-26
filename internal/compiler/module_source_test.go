package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/modsrc"
	"github.com/estevaofon/noxy/internal/parser"
)

// memSource: modulos so na memoria, chave "/mem/<a/b>.nx". Nenhum arquivo
// no disco — prova que o compilador nao resolve `use` fora da Source.
type memSource struct{ files map[string]string }

func (m memSource) Key() string { return "mem" }

func (m memSource) Resolve(name string) (modsrc.Module, error) {
	p := "/mem/" + strings.ReplaceAll(name, ".", "/") + ".nx"
	if _, ok := m.files[p]; ok {
		return modsrc.Module{Name: name, Kind: modsrc.KindFile, Path: p}, nil
	}
	return modsrc.Module{}, modsrc.ErrNotFound
}

func (m memSource) ReadFile(p string) ([]byte, error) {
	if s, ok := m.files[p]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}

func (m memSource) ReadDir(string) ([]modsrc.Entry, error) { return nil, os.ErrNotExist }

func compileWithSource(t *testing.T, src modsrc.Source, program string) error {
	t.Helper()
	root := t.TempDir()
	p := parser.New(lexer.New(program))
	parsed := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	c := NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root)
	c.SetModuleSource(src)
	_, _, err := c.Compile(parsed)
	return err
}

func TestCompilerDiscoversModulesThroughTheSourceOnly(t *testing.T) {
	src := memSource{files: map[string]string{
		"/mem/helper.nx":     "use util.twice select twice\nfunc quad(n: int) -> int\n    return twice(twice(n))\nend\n",
		"/mem/util/twice.nx": "func twice(n: int) -> int\n    return n * 2\nend\n",
	}}
	if err := compileWithSource(t, src, "use helper select quad\nlet x: int = quad(2)\n"); err != nil {
		t.Fatalf("modules from memory must compile: %v", err)
	}
	if err := compileWithSource(t, src, "use missing select *\n"); err == nil || !strings.Contains(err.Error(), "failed to resolve wildcard module 'missing'") {
		t.Fatalf("unknown module: %v", err)
	}
}

func TestCompilerDefaultSourceIsTheDisk(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "disk.nx"), []byte("func one() -> int\n    return 1\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := parser.New(lexer.New("use disk select one\nlet x: int = one()\n"))
	parsed := p.ParseProgram()
	c := NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root)
	if _, _, err := c.Compile(parsed); err != nil {
		t.Fatalf("without SetModuleSource the compiler reads the disk: %v", err)
	}
}
