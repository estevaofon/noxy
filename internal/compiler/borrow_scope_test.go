package compiler_test

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

// Emprestimo escopado (issue #83, spec 2026-08-25-issue-83-borrow-scope).
// ETAPA 1: a regra emite AVISO, nao erro — nada aqui espera falha de
// compilacao. Quando os avisos virarem erro (v0.21.0), estes testes viram
// fixtures de erro e ganham a checagem de hint por posicao (spec §4).

func warningsFor(t *testing.T, src string) []compiler.Warning {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("erro de parse inesperado: %v", errs)
	}
	c := compiler.NewWithState(map[string]ast.NoxyType{}, map[string]*ast.StructStatement{}, "t.nx")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("erro de compilacao inesperado: %v", err)
	}
	return c.Warnings()
}

func borrowWarnings(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, w := range warningsFor(t, src) {
		if strings.Contains(w.Message, "reference into a container") ||
			strings.Contains(w.Message, "no ref-parameter contract") {
			out = append(out, w.Message)
		}
	}
	return out
}

// R11: emprestimo que escapa da chamada. Cada caso e um repro do #83 ou uma
// forma que permitiria construir um.
func TestBorrowEscapeWarns(t *testing.T) {
	cases := []struct {
		nome string
		src  string
		want string
	}{
		{
			nome: "ligado a nome (repro A da spec §1.1)",
			src:  "let a: int[] = [1, 2, 3]\nlet r: ref int = ref a[0]\n",
			want: "'ref a[0]'",
		},
		{
			nome: "campo ligado a nome",
			src:  "struct P\n    x: int\nend\nlet p: P = P(1)\nlet r: ref int = ref p.x\n",
			want: "'ref p.x'",
		},
		{
			nome: "entrada de map ligada a nome",
			src:  "let m: map[string, int] = {\"k\": 1}\nlet r: ref int = ref m[\"k\"]\n",
			want: "'ref m[\"k\"]'",
		},
		{
			nome: "retornado (repro do K&R ch06)",
			src: "func f(a: int[]) -> ref int\n" +
				"    return ref a[0]\n" +
				"end\n",
			want: "'ref a[0]'",
		},
		{
			nome: "guardado em campo",
			src: "struct Box\n    r: ref int\nend\n" +
				"let a: int[] = [1]\n" +
				"let b: Box = Box(null)\n" +
				"b.r = ref a[0]\n",
			want: "'ref a[0]'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.nome, func(t *testing.T) {
			ws := borrowWarnings(t, tc.src)
			if len(ws) == 0 {
				t.Fatalf("esperava aviso de emprestimo, nao veio nenhum")
			}
			if !strings.Contains(ws[0], "escapes the call") || !strings.Contains(ws[0], tc.want) {
				t.Fatalf("aviso = %q, queria 'escapes the call' com %s", ws[0], tc.want)
			}
			if !strings.Contains(ws[0], "hint:") {
				t.Fatalf("aviso sem hint: %q", ws[0])
			}
		})
	}
}

// R10: referencia de CELULA nunca avisa — e o que sustenta lista ligada,
// arvore e grafo. Se algum destes comecar a avisar, a regra vazou para o lado
// errado da fronteira e o idioma da linguagem quebrou.
func TestCellReferenceNeverWarns(t *testing.T) {
	cases := []struct {
		nome string
		src  string
	}{
		{"ref a variavel local", "let x: int = 1\nlet r: ref int = ref x\n*r = 2\n"},
		{"ref a global, guardado em campo", "struct N\n    next: ref N\nend\n" +
			"let a: N = N(null)\nlet b: N = N(null)\nb.next = ref a\n"},
		{"ref a local retornado", "func f() -> ref int\n    let x: int = 1\n    return ref x\nend\n"},
		{"emprestimo como argumento de builtin", "struct P\n    items: int[]\nend\n" +
			"let p: P = P([])\nappend(ref p.items, 1)\n"},
		{"emprestimo como argumento de funcao com parametro ref", "" +
			"func inc(v: ref int) -> void\n    *v = *v + 1\nend\n" +
			"let a: int[] = [1, 2]\ninc(ref a[0])\n"},
		{"emprestimo aninhado como argumento", "struct P\n    items: int[]\nend\n" +
			"struct Q\n    p: P\nend\n" +
			"let q: Q = Q(P([]))\nappend(ref q.p.items, 1)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.nome, func(t *testing.T) {
			if ws := borrowWarnings(t, tc.src); len(ws) != 0 {
				t.Fatalf("nao esperava aviso, veio: %q", ws)
			}
		})
	}
}

// R12 check 3: emprestimo em posicao de ARGUMENTO cujo callee nao tem contrato
// `ref` inspecionavel. Nao e escape — e recusa conservadora, e a mensagem tem
// de dizer isso. Separar as duas mensagens foi consequencia direta do gate do
// corpus: `addr(ref p.x)` aparecia como "escapes", que e falso.
func TestBorrowToUninspectableCalleeWarnsDifferently(t *testing.T) {
	src := "struct P\n    x: int\nend\nlet p: P = P(1)\nlet s: string = addr(ref p.x)\n"
	ws := borrowWarnings(t, src)
	if len(ws) == 0 {
		t.Fatalf("esperava aviso de recusa conservadora, nao veio nenhum")
	}
	if strings.Contains(ws[0], "escapes the call") {
		t.Fatalf("mensagem errada: argumento de native nao escapa; aviso = %q", ws[0])
	}
	if !strings.Contains(ws[0], "no ref-parameter contract") {
		t.Fatalf("aviso = %q, queria a mensagem de check 3", ws[0])
	}
}

// O aviso e diagnostico: nao muda o comportamento do programa nem o resultado
// da compilacao. Guardrail da spec §5.4 (a VM nao recebe uma linha).
func TestBorrowWarningIsNotAnError(t *testing.T) {
	src := "let a: int[] = [1, 2, 3]\nlet r: ref int = ref a[0]\n*r = 9\nprint(a)\n"
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	c := compiler.NewWithState(map[string]ast.NoxyType{}, map[string]*ast.StructStatement{}, "t.nx")
	chunk, _, err := c.Compile(program)
	if err != nil {
		t.Fatalf("aviso nao pode virar erro na etapa 1: %v", err)
	}
	if chunk == nil {
		t.Fatalf("compilacao devia produzir chunk")
	}
	if len(borrowWarnings(t, src)) == 0 {
		t.Fatalf("o aviso devia ter sido emitido")
	}
}
