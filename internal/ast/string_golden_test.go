package ast_test

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/parser"
)

// ast.Node.String() é o texto que aparece em diagnósticos ("addr(ref x)",
// "operador '+' não definido", hints de deref) e em dumps de depuração.
// Este golden fixa o formato de cada nó e confere que CloneStatement produz
// uma cópia independente com o mesmo texto — os dois caminhos não tinham
// teste para a maioria dos nós.

func parseProgram(t *testing.T, source string) *ast.Program {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	return program
}

const goldenSource = `use strings
struct P
    x: int
    y: float
end
func add(a: int, b: int) -> int
    return a + b
end
let n: int = add(1, 2)
n = n + 1
if n > 1 then
    print(-n)
else
    print(null)
end
while n < 3 do
    break
end
for item in [1, 2] do
    continue
end
let m: map[string, int] = {"a": 1}
let f: func(int) -> int = func(v: int) -> int
    return v
end
let p: P = P(1, 2.5)
let z: int[] = zeros(3)
print(p.x, m["a"], true, b"ab")
defer print(1)
let c: chan int = make_chan(1)
when
    case chan_recv(c) then
        print(1)
    default
        print(2)
end
`

var goldenStrings = []string{
	"use strings",
	"struct P x: int, y: float end",
	"func add(a: int, b: int) return (a + b)",
	"let n: int = add(1, 2)",
	"n = (n + 1)",
	"if (n > 1) print((-n)) else print(null)",
	"while (n < 3) break",
	"for item in [1, 2] continue",
	"let m: map[string, int] = {a: 1}",
	"let f: func(int) -> int = func(v: int) return v",
	"let p: P = P(1, 2.5)",
	"let z: int[] = zeros(3)",
	"print((p.x), (m[a]), true, ab)",
	"defer print(1)",
	"let c: chan int = make_chan(1)",
	"when case chan_recv(c) then print(1)default print(2)end",
}

func TestNodeStringGolden(t *testing.T) {
	program := parseProgram(t, goldenSource)
	if len(program.Statements) != len(goldenStrings) {
		t.Fatalf("statements=%d, want %d", len(program.Statements), len(goldenStrings))
	}
	for i, statement := range program.Statements {
		if got := statement.String(); got != goldenStrings[i] {
			t.Errorf("statement %d: String()=%q, want %q", i, got, goldenStrings[i])
		}
	}
	if program.TokenLiteral() != "use" {
		t.Fatalf("Program.TokenLiteral()=%q, want use", program.TokenLiteral())
	}
	if program.String() != strings.Join(goldenStrings, "") {
		t.Fatalf("Program.String() diverge da concatenação dos statements")
	}
	empty := &ast.Program{}
	if empty.TokenLiteral() != "" || empty.String() != "" {
		t.Fatalf("programa vazio: TokenLiteral=%q String=%q", empty.TokenLiteral(), empty.String())
	}
}

func TestCloneStatementProducesIndependentCopyWithSameText(t *testing.T) {
	program := parseProgram(t, goldenSource)
	for i, statement := range program.Statements {
		clone := ast.CloneStatement(statement)
		if clone == statement {
			t.Fatalf("statement %d: clone devolveu o mesmo ponteiro", i)
		}
		if clone.String() != statement.String() {
			t.Fatalf("statement %d: clone=%q, original=%q", i, clone.String(), statement.String())
		}
	}
	if ast.CloneStatement(nil) != nil || ast.CloneExpression(nil) != nil || ast.CloneBlock(nil) != nil {
		t.Fatal("clone de nil deveria ser nil")
	}
}

func TestTypeStringsForRefAndChanWithoutElement(t *testing.T) {
	if got := (&ast.RefType{}).String(); got != "ref any" {
		t.Fatalf("RefType sem elemento: %q, want 'ref any'", got)
	}
	if got := (&ast.ChanType{}).String(); got != "chan any" {
		t.Fatalf("ChanType sem elemento: %q, want 'chan any'", got)
	}
	if got := (&ast.Parameter{Name: "x"}).String(); got != "x: any" {
		t.Fatalf("Parameter sem tipo: %q, want 'x: any'", got)
	}
	if got := (&ast.LetStmt{Name: &ast.Identifier{Value: "x"}}).String(); got != " x: any" {
		t.Fatalf("LetStmt sem token/tipo: %q", got)
	}
}
