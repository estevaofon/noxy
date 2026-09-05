package parser

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/lexer"
)

// Diagnósticos de sintaxe com texto próprio (não o genérico "expected X,
// found Y"). Nenhum deles passava por teste Go nem pela suíte de exemplos —
// os exemplos são programas válidos — e são a primeira coisa que um iniciante
// vê: `let x = 5` sem tipo, `x: int = 5` sem let, bloco sem `end`, parâmetro
// sem tipo, f-string com chave aberta.
func TestSyntaxErrorMessages(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		want    []string
		notWant []string
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
			want:   []string{"SyntaxError: unclosed brace in f-string", "hint: every '{' that starts an expression"},
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
		{
			name:   "ref of ref type",
			source: "let q: ref ref int\n",
			want:   []string{"SyntaxError: 'ref ref' is not a type", "hint: a reference is never taken to a reference"},
		},
		{
			name:   "unterminated string is a SyntaxError with the lexer reason",
			source: "let s: string = \"abc\n",
			want:   []string{"SyntaxError: unterminated string"},
		},
		{
			name:   "unknown character keeps the generic invalid-syntax message",
			source: "let x: int = 1 @ 2\n",
			want:   []string{`invalid syntax "@"`},
		},
		{
			// Issue #126/#134: aspas curvas coladas de um editor de texto.
			// Cada caractere e UM token ILLEGAL com a runa inteira como
			// literal e vira `invalid syntax "“"` — nunca o byte cru
			// impresso sem aspas (o sintoma do #126).
			name:    "pasted curly quotes report invalid syntax, not a bare byte",
			source:  "let s: string = \u201cabc\u201d\n",
			want:    []string{`invalid syntax "“"`, `invalid syntax "”"`},
			notWant: []string{"SyntaxError: \u00e2", `""`},
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
			for _, notWant := range tc.notWant {
				if strings.Contains(joined, notWant) {
					t.Fatalf("source %q: errors=%q, must not contain %q", tc.source, joined, notWant)
				}
			}
		})
	}
}

// Issue #126 item 5 / #134: keyword em posicao de TIPO onde se espera um
// nome (`struct map`, `struct Box<map>`, `func f<int>()`) e `ref` em
// qualquer posicao de nome dizem UM erro que nomeia a keyword, e o parser
// pula o resto da linha (recuperacao em modo panico com ponto de
// sincronizacao — Crafting Interpreters §6.3.3). As keywords de tipo em
// posicao de VALOR (`let map`, `use src.map as map`, `func f(any: int)`)
// sao nomes desde a #134 e nao chegam aqui.
func TestKeywordAsNameIsASingleError(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"struct named map", "struct map end\nlet y: int = 2\n", "[1:8] SyntaxError: 'map' is a keyword and cannot be used as a name"},
		{"struct type parameter named map", "struct Box<map> end\nlet y: int = 2\n", "[1:12] SyntaxError: 'map' is a keyword and cannot be used as a name"},
		{"func type parameter named int", "func f<int>() end\nlet y: int = 2\n", "[1:8] SyntaxError: 'int' is a keyword and cannot be used as a name"},
		{"let named ref", "let ref: int = 1\nlet y: int = 2\n", "[1:5] SyntaxError: 'ref' is a keyword and cannot be used as a name"},
		{"param named ref", "func f(ref: int)\nend\n", "'ref' is a keyword and cannot be used as a name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.source))
			_ = p.ParseProgram()
			errs := p.Errors()
			if len(errs) != 1 {
				t.Fatalf("want exactly 1 error, got %d: %v", len(errs), errs)
			}
			if !strings.Contains(errs[0], tc.want) {
				t.Fatalf("error %q does not contain %q", errs[0], tc.want)
			}
			if !strings.Contains(errs[0], "hint: rename it") {
				t.Fatalf("error %q has no rename hint", errs[0])
			}
		})
	}
}

// Revisão da issue #126 item 5: resyncToLine só pode valer para o statement
// que a ligou. peekError liga a marca quando um expectPeek(IDENTIFIER)
// aninhado falha por causa de keyword de tipo — em `let x: Caixa<io.map> = 5`
// quem falha é o ramo de tipo qualificado dentro do argumento genérico, e
// parseLetStatement ainda volta non-nil (com Type incompleto); em
// `let x: io.map = 5` quem falha é o ramo de tipo qualificado dentro de
// parseValueType, e parseLetStatement também volta non-nil (com Type
// incompleto). Em nenhum dos dois casos ParseProgram vê o statement como
// nil, então resyncAfterFailedStatement nunca roda para ELE — sem o reset no
// topo de parseStatement, a marca ficava ligada e vazava para o PRÓXIMO
// statement, fazendo-o (por engano) pular até o fim da linha em vez de
// seguir seu próprio caminho de erro, comendo o resto do seu diagnóstico.
func TestResyncFlagDoesNotLeakAcrossStatements(t *testing.T) {
	const nextStmt = "let : int = 1\n"

	baseline := New(lexer.New(nextStmt))
	_ = baseline.ParseProgram()
	baselineErrs := baseline.Errors()
	if len(baselineErrs) == 0 {
		t.Fatalf("baseline sem erro: %q deveria falhar sozinho", nextStmt)
	}

	cases := []struct {
		name        string
		leakingStmt string
		keywordWant string
	}{
		{
			"qualified type inside generic argument",
			"let x: Caixa<io.map> = 5\n",
			"'map' is a keyword and cannot be used as a name",
		},
		{
			"qualified type inside let annotation",
			"let x: io.map = 5\n",
			"'map' is a keyword and cannot be used as a name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := New(lexer.New(tc.leakingStmt + nextStmt))
			_ = p.ParseProgram()
			errs := p.Errors()

			joined := strings.Join(errs, "\n")
			if !strings.Contains(joined, tc.keywordWant) {
				t.Fatalf("errors=%q: falta o erro de keyword %q", errs, tc.keywordWant)
			}

			// O que sobra depois do(s) erro(s) da primeira linha tem que
			// bater exatamente com o baseline do segundo statement rodando
			// sozinho (ignorando o prefixo [linha:coluna], que muda porque
			// "let : int = 1" está na linha 2 aqui e na linha 1 no
			// baseline). Se a marca vazou, resyncAfterFailedStatement pula
			// direto até a NEWLINE e engole os erros em cascata de
			// "let : int = 1" — a lista final fica mais curta que o
			// baseline.
			if len(errs) < len(baselineErrs) {
				t.Fatalf("errors=%q: tem menos erros que o baseline sozinho %q — resyncToLine vazou e pulou a linha seguinte", errs, baselineErrs)
			}
			got := errs[len(errs)-len(baselineErrs):]
			for i, want := range baselineErrs {
				if stripPosition(got[i]) != stripPosition(want) {
					t.Fatalf("erro final [%d] = %q, want %q (deveria ser igual ao statement rodando sozinho, exceto a posição) — resyncToLine vazou", i, got[i], want)
				}
			}
		})
	}
}

// stripPosition remove o prefixo "[linha:coluna] " de uma mensagem de erro,
// para comparar o conteúdo do diagnóstico ignorando a posição (que muda
// conforme a linha em que o statement aparece).
func stripPosition(err string) string {
	_, rest, found := strings.Cut(err, "] ")
	if !found {
		return err
	}
	return rest
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

// Issue #134: coluna em caracteres, e um diagnostico por caractere ilegal.
func TestIllegalCharacterDiagnosticsCountRunes(t *testing.T) {
	p := New(lexer.New("let s: string = “abc”\n"))
	_ = p.ParseProgram()
	if got := len(p.Errors()); got != 2 {
		t.Fatalf("want 2 errors (one per pasted quote), got %d: %q", got, p.Errors())
	}

	p = New(lexer.New("let café = 1 @\n"))
	_ = p.ParseProgram()
	joined := strings.Join(p.Errors(), "\n")
	if !strings.Contains(joined, `[1:14] SyntaxError: invalid syntax "@"`) {
		t.Fatalf("errors=%q: want the '@' reported at column 14 (characters, not bytes)", p.Errors())
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
