package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// Repro G (issue #83): o alias e adquirido num ANCESTRAL do conteiner do
// emprestimo. Achado pela validacao adversarial da H4 (Passo 0 do handoff
// 2026-08-25-issue-83-handoff.md), que FALSIFICOU a hipotese de que os seis
// repros A-F esgotavam o problema.
//
// A propriedade que A-F compartilham sem que ninguem tivesse notado: em todos,
// o conteiner do emprestimo E a raiz. `ref arr[0]` sobre um `int[]` plano — um
// objeto so, um `Owners` so. G quebra isso: `ref h.xs[0]` tem caminho
// raiz->conteiner de DOIS niveis, e o segundo dono e adquirido no nivel de
// cima.
//
// Consequencia para o desenho (spec §4.2): `Retain` e por-OBJETO. Quando o
// corpo faz `copia = h`, quem incrementa e o `*ObjInstance`; o `*ObjArray` de
// `h.xs` — o conteiner que o emprestimo marcou — fica intocado em owners=1.
// P3 abre o acesso no conteiner e detecta em `Retain`, entao NAO VE G. Pelo
// mesmo motivo, consultar `IsShared` no ramo REF_INDEX tambem nao veria: no
// momento da escrita o conteiner e, para o RC, perfeitamente unico. O
// compartilhamento mora um nivel acima.
//
// ESTES TESTES SAO DE CARACTERIZACAO: as expectativas abaixo sao a saida
// ERRADA de hoje. Sao verdes agora e falham alto quando o comportamento mudar
// — que e exatamente o sinal desejado. Quando P3 (ou o que o substituir)
// fechar G, inverta cada `want` para o valor anotado como correto e mova o
// arquivo para o regime normal.

// TestBorrowAncestorAliasLeaks: o vazamento, nas quatro formas do canal.
func TestBorrowAncestorAliasLeaks(t *testing.T) {
	t.Run("ancestral e um struct (repro G)", func(t *testing.T) {
		// CORRETO seria [[1,2,3],[999,2,3]]: `copia` foi feita a partir de `h`
		// com o emprestimo vivo, e deveria ser independente.
		got := captureVMSource(t, `struct H
    xs: int[]
end
let h: H = H([1, 2, 3])
let copia: H = H([])
func f(r: ref int) -> void
    copia = h
    *r = 999
end
f(ref h.xs[0])
test_report([copia.xs, h.xs])`)
		assertIntRows(t, got, [][]int64{{999, 2, 3}, {999, 2, 3}})
	})

	t.Run("ancestral e o array externo", func(t *testing.T) {
		// CORRETO seria [[1,2],[999,2]].
		got := captureVMSource(t, `let a: int[][] = [[1, 2], [3, 4]]
let copia: int[][] = []
func f(r: ref int) -> void
    copia = a
    *r = 999
end
f(ref a[0][0])
test_report([copia[0], a[0]])`)
		assertIntRows(t, got, [][]int64{{999, 2}, {999, 2}})
	})

	t.Run("ancestral e um map", func(t *testing.T) {
		// CORRETO seria [[1,2,3],[999,2,3]].
		got := captureVMSource(t, `let m: map[string, int[]] = {"k": [1, 2, 3]}
let copia: map[string, int[]] = {}
func f(r: ref int) -> void
    copia = m
    *r = 999
end
f(ref m["k"][0])
test_report([copia["k"], m["k"]])`)
		assertIntRows(t, got, [][]int64{{999, 2, 3}, {999, 2, 3}})
	})

	t.Run("REF_PROPERTY com ancestral struct", func(t *testing.T) {
		// CORRETO seria 1: `copia` e anterior a escrita.
		got := captureVMSource(t, `struct Inner
    x: int
end
struct Outer
    inn: Inner
end
let o: Outer = Outer(Inner(1))
let copia: Outer = Outer(Inner(0))
func f(r: ref int) -> void
    copia = o
    *r = 999
end
f(ref o.inn.x)
test_report(copia.inn.x)`)
		if got.Type != value.VAL_INT || got.Int() != 999 {
			t.Fatalf("got %s, want 999 (o VAZAMENTO de hoje; o correto e 1)", got.String())
		}
	})

	t.Run("G combinado com F: o alias viaja dentro de um valor", func(t *testing.T) {
		// O call site nao menciona a raiz E o alias esta um nivel acima —
		// nenhuma checagem local o enxerga. CORRETO seria [[1,2,3],[999,2,3]].
		got := captureVMSource(t, `struct H
    xs: int[]
end
struct Holder
    h: ref H
end
let h: H = H([1, 2, 3])
let k: Holder = Holder(ref h)
let copia: H = H([])
func f(r: ref int, kk: Holder) -> void
    copia = *kk.h
    *r = 999
end
f(ref h.xs[0], k)
test_report([copia.xs, h.xs])`)
		assertIntRows(t, got, [][]int64{{999, 2, 3}, {999, 2, 3}})
	})
}

// A ARMADILHA DA §1.3 ACONTECENDO HOJE, SOZINHA.
//
// A spec diz que trocar o vazamento por uma escrita perdida seria pior que o
// bug, e manda toda correcao vir com a contraprova "a escrita atraves do
// emprestimo continua chegando no original". Este programa produz os DOIS
// modos de falha ao mesmo tempo, sem nenhuma correcao aplicada, com um
// emprestimo 100%% legal sob R11/R12:
//
//	copia = h     // a instancia ganha 2o dono; xs continua owners=1
//	h.xs[1] = 7   // MUT em h clona a instancia -> o clone retem xs -> xs vira
//	              //   shared -> GET_PROP_MUT clona xs; h.xs e um array NOVO
//	*r = 999      // o emprestimo escreve no array VELHO, que agora so a copia ve
//
// Ou seja: a contraprova da §1.3 JA E FALSA em conteiner aninhado. Ela precisa
// ser reescrita antes de servir de criterio para qualquer correcao.
func TestBorrowAncestorAliasLosesTheWrite(t *testing.T) {
	// CORRETO seria [[999,7,3],[1,2,3]]: a escrita chega no original, e a
	// copia fica isolada. Hoje sai o oposto exato nos dois.
	got := captureVMSource(t, `struct H
    xs: int[]
end
let h: H = H([1, 2, 3])
let copia: H = H([])
func f(r: ref int) -> void
    copia = h
    h.xs[1] = 7
    *r = 999
end
f(ref h.xs[0])
test_report([h.xs, copia.xs])`)
	assertIntRows(t, got, [][]int64{{1, 7, 3}, {999, 2, 3}})
}

// Acesso conflitante SEM NENHUM SEGUNDO DONO — `Retain` e cego por construcao.
//
// P3 detecta em `Retain` por ser "o funil unico de aquisicao de dono duravel".
// Estes dois violam exclusividade sem adquirir dono nenhum: nao ha `Retain` na
// janela do emprestimo, entao o funil nao e suficiente como sede da checagem.
func TestBorrowConflictWithoutAnyRetain(t *testing.T) {
	t.Run("entrada de map apagada durante o emprestimo e RESSUSCITADA pela escrita", func(t *testing.T) {
		// `referenceStorage`/REF_INDEX para map usa mapping.Set, que INSERE se
		// a chave nao existe. CORRETO seria {b: 2} — ou um erro de acesso.
		got := captureVMSource(t, `let m: map[string, int] = {"a": 1, "b": 2}
func f(r: ref int) -> void
    delete(ref m, "a")
    *r = 999
end
f(ref m["a"])
test_report(m["a"])`)
		if got.Type != value.VAL_INT || got.Int() != 999 {
			t.Fatalf("got %s, want 999 (a chave RESSUSCITADA de hoje; o correto e a chave nao existir)", got.String())
		}
	})

	t.Run("raiz reatribuida durante o emprestimo: escrita silenciosamente perdida", func(t *testing.T) {
		// CORRETO: erro de acesso conflitante. No Swift isto e exatamente um
		// conflito de acesso simultaneo, e o programa trapa.
		got := captureVMSource(t, `let arr: int[] = [1, 2, 3]
func f(r: ref int) -> void
    arr = [7, 7, 7]
    *r = 999
end
f(ref arr[0])
test_report([arr])`)
		assertIntRows(t, got, [][]int64{{7, 7, 7}})
	})
}
