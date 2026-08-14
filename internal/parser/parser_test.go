package parser

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"strings"
	"testing"
)

func TestParseDeferCallStatement(t *testing.T) {
	p := New(lexer.New("defer cleanup(1)\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	stmt, ok := program.Statements[0].(*ast.DeferStmt)
	if !ok || stmt.Call == nil || stmt.Call.Function.String() != "cleanup" || len(stmt.Call.Arguments) != 1 {
		t.Fatalf("statement=%#v, want deferred cleanup call", program.Statements[0])
	}
}

func TestParseDeferRejectsNonCallExpression(t *testing.T) {
	for _, source := range []string{"defer value\n", "defer 1 + 2\n"} {
		p := New(lexer.New(source))
		_ = p.ParseProgram()
		if len(p.Errors()) == 0 || !strings.Contains(strings.Join(p.Errors(), "\n"), "defer expects a call") {
			t.Fatalf("source=%q errors=%v", source, p.Errors())
		}
	}
}

func TestLetStatements(t *testing.T) {
	input := `
let x: int = 5
let y: int = 10
let foobar: int = 838383
`
	l := lexer.New(input)
	p := New(l)

	program := p.ParseProgram()
	checkParserErrors(t, p)

	if program == nil {
		t.Fatalf("ParseProgram() returned nil")
	}
	if len(program.Statements) != 3 {
		t.Fatalf("program.Statements does not contain 3 statements. got=%d",
			len(program.Statements))
	}

	tests := []struct {
		expectedIdentifier string
	}{
		{"x"},
		{"y"},
		{"foobar"},
	}

	for i, tt := range tests {
		stmt := program.Statements[i]
		if !testLetStatement(t, stmt, tt.expectedIdentifier) {
			return
		}
	}
}

func testLetStatement(t *testing.T, s ast.Statement, name string) bool {
	if s.TokenLiteral() != "let" {
		t.Errorf("s.TokenLiteral not 'let'. got=%q", s.TokenLiteral())
		return false
	}

	letStmt, ok := s.(*ast.LetStmt)
	if !ok {
		t.Errorf("s not *ast.LetStmt. got=%T", s)
		return false
	}

	if letStmt.Name.Value != name {
		t.Errorf("letStmt.Name.Value not '%s'. got=%s", name, letStmt.Name.Value)
		return false
	}

	if letStmt.Name.TokenLiteral() != name {
		t.Errorf("s.Name not '%s'. got=%s", name, letStmt.Name.TokenLiteral())
		return false
	}

	return true
}

func checkParserErrors(t *testing.T, p *Parser) {
	errors := p.Errors()
	if len(errors) == 0 {
		return
	}

	t.Errorf("parser has %d errors", len(errors))
	for _, msg := range errors {
		t.Errorf("parser error: %q", msg)
	}
	t.FailNow()
}
func TestParseMap(t *testing.T) {
	input := `
	let m: map[string, int] = {
		"one": 1,
		"two": 2
	}
	`
	l := lexer.New(input)
	p := New(l)
	program := p.ParseProgram()
	checkParserErrors(t, p)

	if len(program.Statements) != 1 {
		t.Fatalf("program.Statements does not contain 1 statement. got=%d",
			len(program.Statements))
	}

	stmt, ok := program.Statements[0].(*ast.LetStmt)
	if !ok {
		t.Fatalf("stmt is not LetStmt. got=%T", program.Statements[0])
	}

	mapLit, ok := stmt.Value.(*ast.MapLiteral)
	if !ok {
		t.Fatalf("stmt.Value is not MapLiteral. got=%T", stmt.Value)
	}

	if len(mapLit.Keys) != 2 {
		t.Fatalf("map.Keys has wrong length. got=%d", len(mapLit.Keys))
	}
}
