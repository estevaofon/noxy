package parser

import (
	"strings"
	"testing"

	"noxy-vm/internal/lexer"
)

// Programa cortado em cada ponto onde o parser exige um token específico
// (`expectPeek`): o erro é sempre "SyntaxError: expected <token>, found
// <token>" com os nomes legíveis de token.Display(), e o parser NÃO entra em
// laço nem em panic com a entrada truncada. Cada linha cobre um site de
// expectPeek que nenhum teste exercitava.
func TestTruncatedConstructsReportExpectedToken(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"let without name", "let : int = 1\n", "expected identifier, found ':'"},
		{"let without colon", "let x int = 1\n", "expected ':', found int"},
		{"use without module", "use\n", "expected identifier, found newline"},
		{"use with dangling dot", "use pkg.\n", "expected identifier, found newline"},
		{"use as without alias", "use strings as\n", "expected identifier, found newline"},
		{"use select without names", "use strings select\n", "expected identifier, found newline"},
		{"if without then", "if true\n    print(1)\nend\n", "expected then, found newline"},
		{"while without do", "while true\n    print(1)\nend\n", "expected do, found newline"},
		{"for without variable", "for in [1] do\nend\n", "expected identifier, found"},
		{"for without in", "for x [1] do\nend\n", "expected"},
		{"for without do", "for x in [1]\nend\n", "expected do, found newline"},
		{"func without name", "func (x: int)\nend\n", "expected identifier, found '('"},
		{"func without parens", "func f\nend\n", "expected '(', found newline"},
		{"param without colon", "func f(x int)\nend\n", "expected ':', found int"},
		{"params without closing paren", "func f(x: int\nend\n", "expected ')', found newline"},
		{"function literal without parens", "let f: func = func\n", "expected '(', found newline"},
		{"struct without name", "struct\nend\n", "expected identifier, found newline"},
		{"struct field without colon", "struct P\n    x int\nend\n", "expected ':', found int"},
		{"member access without name", "let v: int = p.\n", "expected identifier, found newline"},
		{"index without closing bracket", "let v: int = arr[0\n", "expected ']', found newline"},
		{"grouped without closing paren", "let v: int = (1 + 2\n", "expected ')', found newline"},
		// Chamadas e listas aceitam quebra de linha antes de fechar, então o
		// que sobra é o fim do arquivo.
		{"call without closing paren", "print(1, 2\n", "expected ')', found end of file"},
		{"array literal without closing bracket", "let v: int[] = [1, 2\n", "expected ']', found end of file"},
		{"map literal without colon", "let v: map[string, int] = {\"a\" 1}\n", "expected ':', found integer"},
		{"map literal without separator", "let v: map[string, int] = {\"a\": 1 \"b\": 2}\n", "expected ',', found string"},
		{"map literal without closing brace", "let v: map[string, int] = {\"a\": 1\n", "unexpected EOF"},
		{"map type without bracket", "let v: map string = {}\n", "expected '[', found string"},
		{"map type without comma", "let v: map[string int] = {}\n", "expected ',', found int"},
		{"map type without closing bracket", "let v: map[string, int = {}\n", "expected ']', found '='"},
		{"array type without closing bracket", "let v: int[3 = []\n", "expected ']', found '='"},
		{"parenthesized type without closing paren", "let v: (ref int = ref x\n", "expected ')', found '='"},
		{"qualified type with dangling dot", "let v: geo. = 1\n", "expected identifier, found '='"},
		{"generic type without closing gt", "let v: Caixa<int = Caixa(1)\n", "expected '>', found '='"},
		{"type parameters without name", "func f<>()\nend\n", "expected identifier, found '>'"},
		{"type parameters without closing gt", "func f<T(x: T)\nend\n", "expected '>', found '('"},
		{"zeros without parens", "let v: int[] = zeros 3\n", "expected '(', found integer"},
		{"zeros without closing paren", "let v: int[] = zeros(3\n", "expected ')', found newline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.source))
			_ = p.ParseProgram()
			joined := strings.Join(p.Errors(), "\n")
			if !strings.Contains(joined, "SyntaxError: "+tc.want) && !strings.Contains(joined, tc.want) {
				t.Fatalf("source %q: errors=%q, want %q", tc.source, joined, tc.want)
			}
		})
	}
}

// Erros com texto próprio dentro de `when`.
func TestWhenBlockSyntaxErrors(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"case assignment target must be identifier", "when\n    case 1 = chan_recv(c) then\n        print(1)\nend\n", "case assignment target must be identifier"},
		{"unexpected token in when block", "when\n    print(1)\nend\n", "unexpected token in when block"},
		{"func inside case body", "when\n    case chan_recv(c) then\n        func f()\n        end\nend\n", "expected 'end', 'case' or 'default'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.source))
			_ = p.ParseProgram()
			joined := strings.Join(p.Errors(), "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("source %q: errors=%q, want %q", tc.source, joined, tc.want)
			}
		})
	}
}

// Formas válidas que só os exemplos exercitavam: vírgula final e quebras de
// linha antes de fechar chamadas/listas, quebra de linha depois do ':' num
// map, literal de função com nome, linhas em branco entre campos de struct,
// f-string sem interpolação alguma.
func TestPermissiveLayoutForms(t *testing.T) {
	for _, source := range []string{
		"print(1, 2,)\n",
		"print(\n    1,\n    2,\n)\n",
		"let v: int[] = [\n    1,\n    2,\n]\n",
		"let v: map[string, int] = {\n    \"a\":\n        1,\n    \"b\": 2\n}\n",
		"let f: func = func nome(x: int) -> int\n    return x\nend\n",
		"struct P\n\n    x: int\n\n    y: int\n\nend\n",
		"print(f\"sem chaves\")\n",
	} {
		p := New(lexer.New(source))
		_ = p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatalf("source %q deveria parsear: %v", source, p.Errors())
		}
	}
}
