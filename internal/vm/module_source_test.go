package vm

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/compiler"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/modsrc"
	"github.com/estevaofon/noxy/internal/parser"
	"github.com/estevaofon/noxy/internal/value"
)

// captureStdout copia cmd/noxy/main_test.go:15-27: troca os.Stdout por um
// pipe ao redor de run() e devolve o que foi escrito.
func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	run()
	_ = writer.Close()
	os.Stdout = previous
	out, _ := io.ReadAll(reader)
	return string(out)
}

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

func TestVMLoadsModulesThroughTheSourceOnly(t *testing.T) {
	root := t.TempDir() // vazio no disco
	src := memSource{files: map[string]string{
		"/mem/helper.nx": "func twice(n: int) -> int\n    return n * 2\nend\n",
	}}
	machine := NewWithConfig(VMConfig{RootPath: root, Source: src})
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	p := parser.New(lexer.New("use helper\ntest_report(helper.twice(21))\n"))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root)
	c.SetModuleSource(machine.Config.Source)
	c.SetKnownGlobals(machine.GlobalNames())
	code, _, err := c.Compile(program)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("run: %v", err)
	}
	if captured.Type != value.VAL_INT || captured.Int() != 42 {
		t.Fatalf("got %#v, want 42", captured)
	}
}

func TestCompileModuleReportsErrorsWithoutRunning(t *testing.T) {
	src := memSource{files: map[string]string{
		"/mem/bad.nx":  "let x: int = \"s\"\n",
		"/mem/side.nx": "print(\"must not run\")\n",
	}}
	machine := NewWithConfig(VMConfig{RootPath: t.TempDir(), Source: src})
	bad, _ := src.Resolve("bad")
	if err := machine.CompileModule(bad); err == nil {
		t.Fatal("a type error in the module must surface")
	}
	side, _ := src.Resolve("side")
	out := captureStdout(t, func() {
		if err := machine.CompileModule(side); err != nil {
			t.Errorf("side: %v", err)
		}
	})
	if strings.Contains(out, "must not run") {
		t.Fatal("CompileModule must not execute the module body")
	}
}
