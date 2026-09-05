package parser

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/lexer"
)

// Issue #41: `let x = expr` e aceito sem anotacao — o tipo fica para o
// compilador inferir do RHS. O parser so precisa devolver Type == nil com o
// valor parseado.
func TestLetWithoutAnnotationParses(t *testing.T) {
	p := New(lexer.New("let x = 5\nlet s = \"a\" + \"b\"\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	if len(program.Statements) != 2 {
		t.Fatalf("want 2 statements, got %d", len(program.Statements))
	}
	for i, name := range []string{"x", "s"} {
		let, ok := program.Statements[i].(*ast.LetStmt)
		if !ok {
			t.Fatalf("stmt %d: want *ast.LetStmt, got %T", i, program.Statements[i])
		}
		if let.Name.Value != name {
			t.Errorf("stmt %d: want name %q, got %q", i, name, let.Name.Value)
		}
		if let.Type != nil {
			t.Errorf("stmt %d: want Type nil (inferred), got %s", i, let.Type.String())
		}
		if let.Value == nil {
			t.Errorf("stmt %d: want Value parsed, got nil", i)
		}
	}
	if _, ok := program.Statements[0].(*ast.LetStmt).Value.(*ast.IntegerLiteral); !ok {
		t.Errorf("x: want IntegerLiteral value, got %T", program.Statements[0].(*ast.LetStmt).Value)
	}
}

// `let x` sem tipo NEM valor nao tem o que inferir: erro didatico com as duas
// formas validas no hint.
func TestLetWithoutAnnotationOrInitializerIsSyntaxError(t *testing.T) {
	p := New(lexer.New("let x\n"))
	p.ParseProgram()
	errs := strings.Join(p.Errors(), "\n")
	for _, want := range []string{
		"SyntaxError: missing type annotation or initializer for 'x'",
		"hint: use 'let x: <type>' or 'let x = <value>'",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("errors %q missing %q", errs, want)
		}
	}
}
