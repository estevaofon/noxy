package parser

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/lexer"
)

// Issue #134 (spec §1.2): as keywords de tipo sao contextuais — reservadas
// so em posicao de tipo. Em toda posicao de VALOR sao nomes comuns. Cada
// programa aqui era erro de sintaxe (ou, no campo de struct, descarte em
// silencio) e passa a parsear sem erro.
func TestContextualTypeKeywordsAreNames(t *testing.T) {
	cases := []struct{ name, source string }{
		{"let with annotation named int", "let int: int = 5\nprint(int)\n"},
		{"let inferred named map", "let map = 1\n"},
		{"assignment to name named int", "let int = 1\nint = 2\n"},
		{"func named any", "func any() -> int\n    return 1\nend\n"},
		{"params named map and chan", "func f(map: int[][], chan: int) -> int\n    return chan\nend\n"},
		{"for variable named string", "for string in [1] do\n    print(string)\nend\n"},
		{"use path segment and alias named map", "use src.map as map\nprint(map.tile())\n"},
		{"use select names int and float", "use src.util select int, float\n"},
		{"struct field named map", "struct S\n    map: int\n    n: int\nend\n"},
		{"member read write ref and f-string", "print(s.map)\ns.map = 3\nlet r = ref s.map\nprint(f\"{s.map}\")\n"},
		{"function literal named any", "let f = func any(x: int) -> int\n    return x\nend\n"},
		{"generic func named map", "func map<A, B>(xs: A[], fn: func(A) -> B) -> B[]\n    let out: B[] = []\n    return out\nend\n"},
		{"every contextual keyword as a let name", "let float = 1\nlet string = 2\nlet bool = 3\nlet bytes = 4\nlet void = 5\nlet any = 6\nlet chan = 7\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.source))
			_ = p.ParseProgram()
			if len(p.Errors()) != 0 {
				t.Fatalf("source %q deveria parsear: %v", tc.source, p.Errors())
			}
		})
	}
}

// O no produzido e o mesmo Identifier de um nome comum: o compilador nao
// distingue `int` de `x`.
func TestContextualKeywordNodesAreIdentifiers(t *testing.T) {
	p := New(lexer.New("let int: int = 5\nstruct S\n    map: int\n    n: int\nend\nprint(s.map, int)\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)

	let, ok := program.Statements[0].(*ast.LetStmt)
	if !ok || let.Name.Value != "int" {
		t.Fatalf("statement 0 = %#v, want let named \"int\"", program.Statements[0])
	}
	st, ok := program.Statements[1].(*ast.StructStatement)
	if !ok || len(st.FieldsList) != 2 || st.FieldsList[0].Name != "map" || st.FieldsList[1].Name != "n" {
		t.Fatalf("statement 1 = %#v, want struct with fields [map n]", program.Statements[1])
	}
	es, ok := program.Statements[2].(*ast.ExpressionStmt)
	if !ok {
		t.Fatalf("statement 2 = %#v, want expression statement", program.Statements[2])
	}
	call, ok := es.Expression.(*ast.CallExpression)
	if !ok || len(call.Arguments) != 2 {
		t.Fatalf("expression = %#v, want call with 2 arguments", es.Expression)
	}
	member, ok := call.Arguments[0].(*ast.MemberAccessExpression)
	if !ok || member.Member != "map" {
		t.Fatalf("argument 0 = %#v, want member access .map", call.Arguments[0])
	}
	ident, ok := call.Arguments[1].(*ast.Identifier)
	if !ok || ident.Value != "int" {
		t.Fatalf("argument 1 = %#v, want identifier int", call.Arguments[1])
	}
}

// design §1.5: com `use src.map as map` legal, `map.Tile` em anotacao e' um
// tipo qualificado — `map.` e `map[` se distinguem por um token de
// lookahead. `chan T` e `map[K, V]` continuam construtores de tipo.
func TestContextualKeywordAliasQualifiesAType(t *testing.T) {
	p := New(lexer.New("let t: map.Tile = map.tile()\nlet u: map.Box<int> = map.box(1)\nlet m: map[string, int] = {}\nlet c: chan int = make_chan()\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)

	let, ok := program.Statements[0].(*ast.LetStmt)
	if !ok || let.Type == nil || let.Type.String() != "map.Tile" {
		t.Fatalf("statement 0 = %#v, want let with type map.Tile", program.Statements[0])
	}
	generic, ok := program.Statements[1].(*ast.LetStmt)
	if !ok || generic.Type == nil {
		t.Fatalf("statement 1 = %#v, want let with generic type", program.Statements[1])
	}
	if gt, isGeneric := generic.Type.(*ast.GenericType); !isGeneric || gt.Name != "map.Box" || len(gt.Args) != 1 {
		t.Fatalf("statement 1 type = %#v, want GenericType map.Box<int>", generic.Type)
	}
}

// `map` seguido de outra coisa que nao '.' nem '[' continua a ser o erro de
// construtor de tipo, nao um tipo chamado map.
func TestMapTypeStillRequiresBracket(t *testing.T) {
	p := New(lexer.New("let v: map string = {}\n"))
	_ = p.ParseProgram()
	joined := strings.Join(p.Errors(), "\n")
	if !strings.Contains(joined, "expected '[', found string") {
		t.Fatalf("errors=%q, want expected '[', found string", p.Errors())
	}
}

// Campo de struct cujo token nao e nome: antes era pulado em silencio (o
// campo sumia e o erro so aparecia no construtor); agora e erro de sintaxe.
// `ref` nao e nome em posicao alguma.
func TestStructFieldThatIsNotANameIsAnError(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"integer literal as field", "struct P\n    x: int\n    5: int\nend\n", "expected identifier, found integer"},
		{"ref as field name", "struct P\n    ref: int\nend\n", "expected identifier, found ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.source))
			_ = p.ParseProgram()
			errs := p.Errors()
			if len(errs) != 1 {
				t.Fatalf("source %q: want exactly 1 error, got %d: %q", tc.source, len(errs), errs)
			}
			if !strings.Contains(errs[0], tc.want) {
				t.Fatalf("source %q: error %q does not contain %q", tc.source, errs[0], tc.want)
			}
		})
	}
}
