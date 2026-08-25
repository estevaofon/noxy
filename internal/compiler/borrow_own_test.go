package compiler_test

import (
	"strings"
	"testing"
)

// R12 (issue #83, spec 2026-08-25-issue-83-exclusive-access-design §2.3):
// um parametro `ref T` e emprestimo e nao pode ser GUARDADO; `own ref T`
// declara o que guarda, e por isso so aceita referencia de celula (R10).
//
// ETAPA 1: as duas checagens emitem AVISO, nao erro — o mesmo rollout de R11.
// Nenhum teste aqui espera falha de compilacao; quando virarem erro (v0.21.0),
// estes casos viram fixtures de erro.

func ownWarnings(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, w := range warningsFor(t, src) {
		if strings.Contains(w.Message, "is a borrow and cannot be kept") ||
			strings.Contains(w.Message, "is declared 'own ref'") {
			out = append(out, w.Message)
		}
	}
	return out
}

// R12, lado do CALLEE: as posicoes de guarda da §2.3 — store em
// global/campo/elemento, `return`, `append`/`chan_send`, captura por closure.
func TestBorrowParamKeptWarns(t *testing.T) {
	cases := []struct {
		nome string
		src  string
		want string
	}{
		{
			nome: "store em global (repro B da spec §1.1)",
			src:  "let g: ref int = null\nfunc keep(r: ref int) -> void\n    g = r\nend\n",
			want: "stored in 'g'",
		},
		{
			nome: "store em campo",
			src: "struct No\n    valor: int\n    prox: ref No\nend\n" +
				"func liga(pai: ref No, filho: ref No) -> void\n    pai.prox = filho\nend\n",
			want: "stored in 'pai.prox'",
		},
		{
			nome: "return",
			src:  "func devolve(r: ref int) -> ref int\n    return r\nend\n",
			want: "is returned",
		},
		{
			nome: "append",
			src: "let x0: int = 0\nlet guardados: (ref int)[] = [ref x0]\n" +
				"func acumula(r: ref int) -> void\n    append(ref guardados, r)\nend\n",
			want: "stored by 'append'",
		},
		{
			nome: "captura por closure",
			src: "func captura(r: ref int) -> func() -> int\n" +
				"    let f: func() -> int = func() -> int\n        return *r\n    end\n    return f\nend\n",
			want: "captured by a closure",
		},
		{
			nome: "construtor cujo resultado e guardado",
			src: "struct Holder\n    r: ref int\nend\nlet h: Holder = Holder(null)\n" +
				"func guarda(r: ref int) -> void\n    h = Holder(r)\nend\n",
			want: "stored in 'h'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.nome, func(t *testing.T) {
			got := ownWarnings(t, tc.src)
			if len(got) == 0 {
				t.Fatalf("esperava aviso R12 contendo %q, nao veio nenhum", tc.want)
			}
			if !strings.Contains(strings.Join(got, "\n"), tc.want) {
				t.Fatalf("esperava %q, veio:\n%s", tc.want, strings.Join(got, "\n"))
			}
		})
	}
}

// O que a R12 NAO pode recusar: usar, escrever atraves e REPASSAR sao os usos
// legitimos de um emprestimo. Um falso positivo aqui mata o idioma que a issue
// #82 acabou de estabelecer.
func TestBorrowParamUsedIsSilent(t *testing.T) {
	cases := []struct {
		nome string
		src  string
	}{
		{
			nome: "escreve atraves do emprestimo",
			src:  "func inc(n: ref int) -> void\n    *n = *n + 1\nend\n",
		},
		{
			nome: "repassa para outra funcao",
			src:  "func inc(n: ref int) -> void\n    *n = *n + 1\nend\nfunc passa(n: ref int) -> void\n    inc(n)\nend\n",
		},
		{
			nome: "copia o VALOR para um global",
			src:  "let g: int = 0\nfunc copia(r: ref int) -> void\n    g = *r\nend\n",
		},
		{
			nome: "liga a um local (morre com a chamada)",
			src:  "func local(r: ref int) -> void\n    let alias: ref int = r\n    *alias = 1\nend\n",
		},
		{
			nome: "retorna o VALOR, nao o emprestimo",
			src:  "func le(r: ref int) -> int\n    return *r\nend\n",
		},
		{
			nome: "parametro declarado own",
			src: "struct No\n    valor: int\n    prox: ref No\nend\n" +
				"func liga(pai: ref No, filho: own ref No) -> void\n    pai.prox = filho\nend\n",
		},
		{
			nome: "parametro da closure sombreia o emprestimo",
			src: "func sombra(r: ref int) -> func(int) -> int\n" +
				"    let f: func(int) -> int = func(r: int) -> int\n        return r\n    end\n    return f\nend\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.nome, func(t *testing.T) {
			if got := ownWarnings(t, tc.src); len(got) > 0 {
				t.Fatalf("uso legitimo nao pode avisar, veio:\n%s", strings.Join(got, "\n"))
			}
		})
	}
}

// R12, lado do CHAMADOR: `own ref` guarda, logo so aceita celula (R10). A
// checagem e uma consulta a assinatura — o corpo do callee nunca e inspecionado
// para decidir a legalidade de uma chamada (spec §2.2).
func TestOwnParamRejectsBorrowAtCallSite(t *testing.T) {
	const decl = "struct No\n    valor: int\n    prox: ref No\nend\n" +
		"func liga(pai: ref No, filho: own ref No) -> void\n    pai.prox = filho\nend\n" +
		"let a: No = No(1, null)\n"

	t.Run("emprestimo por indice e recusado", func(t *testing.T) {
		src := decl + "let lista: No[] = [No(2, null)]\nliga(ref a, ref lista[0])\n"
		got := ownWarnings(t, src)
		joined := strings.Join(got, "\n")
		if !strings.Contains(joined, "declared 'own ref'") || !strings.Contains(joined, "'ref lista[0]'") {
			t.Fatalf("esperava recusa de 'ref lista[0]' em parametro own, veio:\n%s", joined)
		}
		if !strings.Contains(joined, "argument 2 to 'liga'") {
			t.Fatalf("o aviso tem de nomear a POSICAO do argumento, veio:\n%s", joined)
		}
	})

	t.Run("emprestimo por campo e recusado", func(t *testing.T) {
		src := decl + "struct Par\n    n: No\nend\nlet par: Par = Par(No(2, null))\nliga(ref a, ref par.n)\n"
		if got := ownWarnings(t, src); !strings.Contains(strings.Join(got, "\n"), "'ref par.n'") {
			t.Fatalf("esperava recusa de 'ref par.n', veio:\n%s", strings.Join(got, "\n"))
		}
	})

	t.Run("celula e aceita", func(t *testing.T) {
		src := decl + "let b: No = No(2, null)\nliga(ref a, ref b)\n"
		if got := ownWarnings(t, src); len(got) > 0 {
			t.Fatalf("referencia de celula e o que 'own' aceita, veio:\n%s", strings.Join(got, "\n"))
		}
	})

	t.Run("parametro sem own nao restringe o call site", func(t *testing.T) {
		src := "func usa(r: ref int) -> void\n    *r = 1\nend\n" +
			"let arr: int[] = [1, 2]\nusa(ref arr[0])\n"
		if got := ownWarnings(t, src); len(got) > 0 {
			t.Fatalf("emprestimo em parametro emprestimo e o caso NORMAL, veio:\n%s", strings.Join(got, "\n"))
		}
	})

	t.Run("referencia adiantada de dentro de um corpo enxerga o contrato", func(t *testing.T) {
		// O predeclare publica os flags `own` antes de qualquer corpo compilar.
		// Sem isso, um corpo que chama uma funcao declarada MAIS ABAIXO — que e
		// como recursao mutua se escreve — passaria sem checagem. (Chamada
		// adiantada em TOP LEVEL nao entra aqui: o global so existe depois do
		// OP_SET_GLOBAL da declaracao, e o programa morre em runtime antes.)
		src := "struct No\n    valor: int\n    prox: ref No\nend\n" +
			"func topo(a: ref No, lista: No[]) -> void\n    liga(a, ref lista[0])\nend\n" +
			"func liga(pai: ref No, filho: own ref No) -> void\n    pai.prox = filho\nend\n"
		if got := ownWarnings(t, src); !strings.Contains(strings.Join(got, "\n"), "declared 'own ref'") {
			t.Fatalf("referencia adiantada tem de ser checada, veio:\n%s", strings.Join(got, "\n"))
		}
	})
}

// A CONTRAPROVA da spec §1.3: a escrita atraves do emprestimo continua chegando
// no original. Um teste de "a copia esta isolada" passa numa implementacao que
// PERDE a escrita num clone anonimo — que e o conserto errado que a spec proibe.
// R12 e estatica e nao pode ter mexido nisso; este teste trava a propriedade.
func TestBorrowWriteStillReachesOriginal(t *testing.T) {
	src := "func inc(n: ref int) -> void\n    *n = *n + 1\nend\n" +
		"let arr: int[] = [1, 2, 3]\ninc(ref arr[0])\n"
	if got := ownWarnings(t, src); len(got) > 0 {
		t.Fatalf("o idioma central nao pode avisar, veio:\n%s", strings.Join(got, "\n"))
	}
}
