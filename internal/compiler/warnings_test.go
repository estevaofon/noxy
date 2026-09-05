package compiler

import (
	"io"
	"os"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/parser"
)

// Issue #61 item 3: o aviso "rebinding ref parameter" saia em STDOUT via
// fmt.Printf — misturado a saida do programa, contra a regra do AGENTS.md
// (stdout e do programa, stderr e do diagnostico). O compilador nao escreve
// em lugar nenhum: acumula Warnings estruturados e quem chama Compile (CLI,
// REPL, loader de modulos da VM) decide o destino.

func captureCompilerStdout(t *testing.T, run func()) string {
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

func compileNamedSource(t *testing.T, fileName, input string) (*Compiler, error) {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	c := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), fileName)
	_, _, err := c.Compile(program)
	return c, err
}

const rebindRefParamSource = "func f(r: ref int)\n    let y: int = 10\n    r = ref y\nend\n"

func TestRebindingRefParameterIsAStructuredWarningNotStdout(t *testing.T) {
	var c *Compiler
	var err error
	stdout := captureCompilerStdout(t, func() { c, err = compileNamedSource(t, "prog.nx", rebindRefParamSource) })
	if err != nil {
		t.Fatalf("rebind de parametro ref continua valido: %v", err)
	}
	if stdout != "" {
		t.Fatalf("compilador escreveu em stdout: %q", stdout)
	}
	warnings := c.Warnings()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exatamente 1", warnings)
	}
	w := warnings[0]
	if w.Message != "rebinding ref parameter 'r' has no effect outside function" || w.File != "prog.nx" || w.Line != 3 {
		t.Fatalf("warning = %+v", w)
	}
	if got := w.String(); got != "warning: rebinding ref parameter 'r' has no effect outside function\n  --> prog.nx:3" {
		t.Fatalf("String() = %q", got)
	}
}

// O corpo de funcao e compilado por um compilador filho (NewChild): o aviso
// tem de subir ate o compilador raiz, que e o que a CLI consulta. Funcoes
// aninhadas (closure) idem.
func TestWarningsPropagateFromNestedFunctionCompilers(t *testing.T) {
	src := "func outer()\n    func inner(r: ref int)\n        let y: int = 1\n        r = ref y\n    end\nend\n"
	c, err := compileNamedSource(t, "nested.nx", src)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Warnings(); len(got) != 1 || got[0].Line != 4 {
		t.Fatalf("warnings = %v, want 1 na linha 4", got)
	}
}

// Programa com genericos passa pelo two-pass (pass 1 descartavel + pass 2):
// o aviso nao pode sair duas vezes.
func TestWarningsAreNotDuplicatedByGenericsTwoPass(t *testing.T) {
	src := "func id<T>(x: T) -> T\n    return x\nend\n" + rebindRefParamSource + "let v: int = id(1)\n"
	c, err := compileNamedSource(t, "gen.nx", src)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Warnings(); len(got) != 1 {
		t.Fatalf("warnings = %v, want 1", got)
	}
}

func TestProgramWithoutRebindHasNoWarnings(t *testing.T) {
	src := "func f(r: ref int)\n    *r = 2\nend\nfunc g(r: ref int)\n    let local: ref int = r\n    local = r\nend\n"
	c, err := compileNamedSource(t, "clean.nx", src)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Warnings(); len(got) != 0 {
		t.Fatalf("warnings = %v, want nenhum", got)
	}
}
