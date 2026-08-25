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

func TestParseContinueStatementKeepsInlineEnd(t *testing.T) {
	p := New(lexer.New("while true do\n    if x then continue end\n    print(1)\nend\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	loop, ok := program.Statements[0].(*ast.WhileStatement)
	if !ok || len(loop.Body.Statements) != 2 {
		t.Fatalf("while body should have 2 statements, got %#v", program.Statements[0])
	}
	cond := loop.Body.Statements[0].(*ast.IfStatement)
	if _, ok := cond.Consequence.Statements[0].(*ast.ContinueStmt); !ok {
		t.Fatalf("expected ContinueStmt, got %T", cond.Consequence.Statements[0])
	}
}

func TestParseVoidReturnBeforeInlineEndElseElif(t *testing.T) {
	for _, source := range []string{
		"func f(x: int)\n    if x > 0 then return end\n    print(1)\nend\n",
		"func f(x: int)\n    if x > 0 then return else print(2) end\nend\n",
		"func f(x: int)\n    if x > 0 then return elif x < 0 then print(3) end\nend\n",
		"func f(x: int)\n    if x > 0 then\n        return\n    end\nend\n",
	} {
		p := New(lexer.New(source))
		p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatalf("source %q: %v", source, p.Errors())
		}
	}
}

func TestParseScientificFloatLiteral(t *testing.T) {
	p := New(lexer.New("1.5e3\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	literal, ok := program.Statements[0].(*ast.ExpressionStmt).Expression.(*ast.FloatLiteral)
	if !ok || literal.Value != 1500.0 {
		t.Fatalf("got %#v, want FloatLiteral 1500", program.Statements[0])
	}
}

func TestFStringBraceEscapesAndTrailingTokenError(t *testing.T) {
	// A ultima variante usa f-string de aspas simples porque o mapa literal
	// `{"a": 1}` contem aspas duplas: o lexer nao e brace-aware, entao aspas
	// duplas dentro de `{...}` exigem f-string delimitada por aspas simples
	// (mesma regra do caso `f'{"a"}'` logo antes) — ver §9 da spec.
	for _, source := range []string{"f\"{{x}}\"\n", "f\"{{{x}}}\"\n", "f'{\"a\"}'\n", "f'{ {\"a\": 1}[\"a\"] }'\n"} {
		p := New(lexer.New(source))
		p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatalf("source %q: %v", source, p.Errors())
		}
	}
	p := New(lexer.New("f\"{name:>10}|\"\n"))
	p.ParseProgram()
	if len(p.Errors()) == 0 || !strings.Contains(p.Errors()[0], "unexpected \":\" in f-string expression") || !strings.Contains(p.Errors()[0], "format specs are not supported") {
		t.Fatalf("errors=%v, want format-spec rejection with hint", p.Errors())
	}
	p = New(lexer.New("f\"{a b}\"\n"))
	p.ParseProgram()
	if len(p.Errors()) == 0 || !strings.Contains(p.Errors()[0], "unexpected \"b\" in f-string expression") {
		t.Fatalf("errors=%v, want trailing-token rejection", p.Errors())
	}
}

// R12 (issue #83): `own` e modificador de PARAMETRO, nao tipo — `own ref T` e
// `ref T` sao o mesmo *ast.RefType, e o que muda e o booleano Owned. Ver a §2.3
// da spec 2026-08-25-issue-83-exclusive-access-design.
func TestParseOwnRefParameter(t *testing.T) {
	input := "func liga(pai: ref No, filho: own ref No) -> void\n    pai.prox = filho\nend\n"
	p := New(lexer.New(input))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("erro de parse inesperado: %v", errs)
	}
	fn, ok := program.Statements[0].(*ast.FunctionStatement)
	if !ok {
		t.Fatalf("statement[0]=%T, want *ast.FunctionStatement", program.Statements[0])
	}
	if len(fn.Parameters) != 2 {
		t.Fatalf("len(Parameters)=%d, want 2", len(fn.Parameters))
	}
	if fn.Parameters[0].Owned {
		t.Fatalf("'pai: ref No' nao e own")
	}
	if !fn.Parameters[1].Owned {
		t.Fatalf("'filho: own ref No' tem de ser own")
	}
	// O TIPO e o mesmo dos dois lados: `own` nao entra no tipo.
	if _, isRef := fn.Parameters[1].Type.(*ast.RefType); !isRef {
		t.Fatalf("Parameters[1].Type=%T, want *ast.RefType", fn.Parameters[1].Type)
	}
	if got, want := fn.Parameters[1].Type.String(), fn.Parameters[0].Type.String(); got != want {
		t.Fatalf("tipo de 'own ref No'=%q, tipo de 'ref No'=%q — tem de ser o mesmo", got, want)
	}
}

// `own` e contextual: so vale imediatamente antes de `ref`. Sobre qualquer
// outro tipo e erro de sintaxe — o modificador declara que o parametro
// sobrevive a chamada, e isso so tem sentido para uma referencia.
func TestParseOwnOnNonRefIsSyntaxError(t *testing.T) {
	p := New(lexer.New("func f(x: own int) -> void\n    print(x)\nend\n"))
	p.ParseProgram()
	errs := p.Errors()
	if len(errs) == 0 {
		t.Fatalf("'own int' tem de ser erro de sintaxe")
	}
	if !strings.Contains(strings.Join(errs, "\n"), "'own' applies only to a 'ref' parameter") {
		t.Fatalf("erros=%v", errs)
	}
}

// Contrapartida da regra contextual: um struct chamado `own` continua legal
// como tipo. Nenhum programa existente quebra por causa da adicao.
func TestOwnRemainsUsableAsTypeName(t *testing.T) {
	p := New(lexer.New("struct own\n    v: int\nend\nfunc f(x: own) -> int\n    return x.v\nend\n"))
	p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("'own' como nome de struct tem de continuar valendo: %v", errs)
	}
}
