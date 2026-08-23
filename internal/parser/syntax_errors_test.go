package parser

import (
	"strings"
	"testing"

	"noxy-vm/internal/lexer"
)

// Diagnósticos de sintaxe com texto próprio (não o genérico "expected X,
// found Y"). Nenhum deles passava por teste Go nem pela suíte de exemplos —
// os exemplos são programas válidos — e são a primeira coisa que um iniciante
// vê: `let x = 5` sem tipo, `x: int = 5` sem let, bloco sem `end`, parâmetro
// sem tipo, f-string com chave aberta.
func TestSyntaxErrorMessages(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "missing let keyword",
			source: "x: int = 5\n",
			want:   []string{"SyntaxError: missing 'let' keyword for variable declaration", "hint: use 'let x: ...'"},
		},
		{
			name:   "let without type or initializer",
			source: "let x\n",
			want:   []string{"SyntaxError: missing type annotation or initializer for 'x'", "hint: use 'let x: <type>' or 'let x = <value>'"},
		},
		{
			name:   "if without end",
			source: "if true then\n    print(1)\n",
			want:   []string{"SyntaxError: expected 'end', 'else' or 'elif', found EOF"},
		},
		{
			name:   "if else without end",
			source: "if true then\n    print(1)\nelse\n    print(2)\n",
			want:   []string{"SyntaxError: expected 'end', found EOF"},
		},
		{
			name:   "while without end",
			source: "while true do\n    print(1)\n",
			want:   []string{"SyntaxError: expected 'end', found EOF"},
		},
		{
			name:   "func without end",
			source: "func f()\n    print(1)\n",
			want:   []string{"SyntaxError: expected 'end', found EOF"},
		},
		{
			name:   "function literal without end",
			source: "let f: func = func(x: int) -> int\n    return x\n",
			want:   []string{"SyntaxError: expected 'end', found EOF"},
		},
		{
			name:   "struct without end",
			source: "struct P\n    x: int\n",
			want:   []string{"SyntaxError: expected 'end', found EOF"},
		},
		{
			name:   "for without end",
			source: "for x in [1] do\n    print(x)\n",
			want:   []string{"SyntaxError: expected 'end' after for loop, found EOF"},
		},
		{
			name:   "first parameter without type",
			source: "func f(a, b: int)\nend\n",
			want:   []string{"SyntaxError: missing type annotation for parameter 'a'", "hint: use 'a: <type>'"},
		},
		{
			name:   "later parameter without type",
			source: "func f(a: int, b)\nend\n",
			want:   []string{"SyntaxError: missing type annotation for parameter 'b'", "hint: use 'b: <type>'"},
		},
		{
			name:   "f-string with unclosed brace",
			source: "print(f\"{x\")\n",
			want:   []string{"SyntaxError: unclosed brace in f-string"},
		},
		{
			name:   "f-string with broken inner expression",
			source: "print(f\"{1 +}\")\n",
			want:   []string{"f-string expr error"},
		},
		{
			name:   "expression cut at end of file",
			source: "let x: int =",
			want:   []string{"SyntaxError: unexpected EOF"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.source))
			_ = p.ParseProgram()
			joined := strings.Join(p.Errors(), "\n")
			if len(p.Errors()) == 0 {
				t.Fatalf("source %q: esperava erro de sintaxe", tc.source)
			}
			for _, want := range tc.want {
				if !strings.Contains(joined, want) {
					t.Fatalf("source %q: errors=%q, want %q", tc.source, joined, want)
				}
			}
		})
	}
}

// Cada diagnóstico carrega a posição [linha:coluna] do ponto do erro — é o
// que o usuário usa para achar a linha; garante que a linha não é sempre 1.
func TestSyntaxErrorsCarryLineAndColumn(t *testing.T) {
	p := New(lexer.New("let a: int = 1\nlet b: int = 2\nlet x\n"))
	_ = p.ParseProgram()
	joined := strings.Join(p.Errors(), "\n")
	if !strings.HasPrefix(joined, "[3:") {
		t.Fatalf("erro deveria apontar a linha 3: %q", joined)
	}
}

// Os programas corretos correspondentes não produzem erro — guarda contra um
// diagnóstico novo disparar em código válido.
func TestSyntaxErrorCounterpartsParse(t *testing.T) {
	for _, source := range []string{
		"let x: int = 5\n",
		"if true then\n    print(1)\nelse\n    print(2)\nend\n",
		"while true do\n    print(1)\nend\n",
		"func f(a: int, b: int)\n    print(1)\nend\n",
		"let f: func = func(x: int) -> int\n    return x\nend\n",
		"struct P\n    x: int\nend\n",
		"for x in [1] do\n    print(x)\nend\n",
		"print(f\"{{x}} = {1 + 2}\")\n",
	} {
		p := New(lexer.New(source))
		_ = p.ParseProgram()
		if len(p.Errors()) != 0 {
			t.Fatalf("source %q deveria parsear: %v", source, p.Errors())
		}
	}
}
