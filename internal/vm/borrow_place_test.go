package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// O empréstimo como LUGAR (issue #83).
//
// `ref a[i]` / `ref p.x` não denota um objeto, denota um LUGAR dentro de um
// composto que o copy-on-write pode bifurcar. Congelar o objeto na criação
// deixava a referência correta no instante em que nascia e errada a partir do
// primeiro evento que mexesse no caminho. Agora o caminho raiz→contêiner é
// re-resolvido na escrita, unicizando e gravando o clone de volta em cada nível
// (borrowContainer, references.go; compileBorrowBase, compiler/borrow_place.go).
//
// TODO TESTE AQUI TEM AS DUAS METADES, e é de propósito. A §1.3 da spec avisa
// que o conserto óbvio — unicizar o contêiner na escrita — é PIOR que o bug: a
// escrita vai para um clone anônimo e some, e um teste que só verifica "a cópia
// está isolada" PASSA nessa implementação errada. Por isso cada caso afirma
// também que a escrita através do empréstimo CONTINUA CHEGANDO no original.

// assertIsolatedAndWritten trava as duas metades de uma vez: [cópia, original].
func assertIsolatedAndWritten(t *testing.T, got value.Value, copia, original []int64) {
	t.Helper()
	assertIntRows(t, got, [][]int64{copia, original})
}

// Os SETE repros da §1.1 da spec. A e B são escape; C, D, E, F e G são acesso
// conflitante. G foi encontrado pela validação adversarial da H4 e é o que
// obrigou a solução a ser sobre CAMINHO, não sobre objeto: nele o contêiner do
// empréstimo não é a raiz, e o segundo dono entra num ancestral.
func TestBorrowPlaceClosesAllRepros(t *testing.T) {
	t.Run("A: o empréstimo é ligado a um nome, cópia depois do ref", func(t *testing.T) {
		got := captureVMSource(t, `let arr: int[] = [1, 2, 3]
let r: ref int = ref arr[0]
let copia: int[] = arr
*r = 999
test_report([copia, arr])`)
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})

	t.Run("B: o empréstimo escapa por dentro do chamado", func(t *testing.T) {
		got := captureVMSource(t, `let g: ref int = null
func keep(r: ref int) -> void
    g = r
end
let arr: int[] = [1, 2, 3]
keep(ref arr[0])
let copia: int[] = arr
*g = 999
test_report([copia, arr])`)
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})

	t.Run("C: outro argumento da mesma chamada copia a raiz", func(t *testing.T) {
		got := captureVMSource(t, `let arr: int[] = [1, 2, 3]
let visto: int[] = []
func f(r: ref int, xs: int[]) -> void
    *r = 999
    visto = xs
end
f(ref arr[0], arr)
test_report([visto, arr])`)
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})

	t.Run("D: o callee alcança a raiz por um global", func(t *testing.T) {
		got := captureVMSource(t, `let arr: int[] = [1, 2, 3]
let copia: int[] = []
func f(r: ref int) -> void
    copia = arr
    *r = 999
end
f(ref arr[0])
test_report([copia, arr])`)
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})

	t.Run("E: outro argumento passa a raiz como referência de célula", func(t *testing.T) {
		got := captureVMSource(t, `let arr: int[] = [1, 2, 3]
let copia: int[] = []
func f(r: ref int, xs: ref int[]) -> void
    copia = *xs
    *r = 999
end
f(ref arr[0], ref arr)
test_report([copia, arr])`)
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})

	t.Run("F: o alias viaja dentro de um valor; o call site não menciona a raiz", func(t *testing.T) {
		got := captureVMSource(t, `struct Holder
    r: ref int[]
end
let arr: int[] = [1, 2, 3]
let h: Holder = Holder(ref arr)
let copia: int[] = []
func f(r: ref int, hh: Holder) -> void
    copia = *hh.r
    *r = 999
end
f(ref arr[0], h)
test_report([copia, arr])`)
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})

	t.Run("G: o alias é adquirido num ANCESTRAL do contêiner", func(t *testing.T) {
		// O contêiner do empréstimo é `h.xs`; o segundo dono entra em `h`.
		// `Retain` é por objeto, então o `Owners` de `h.xs` fica intocado —
		// é por isso que nenhuma checagem centrada no contêiner via este caso.
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
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})
}

// G em todas as formas do canal: o que varia é o TIPO do ancestral.
func TestBorrowPlaceClosesAncestorAliasByAncestorKind(t *testing.T) {
	t.Run("ancestral é o array externo", func(t *testing.T) {
		got := captureVMSource(t, `let a: int[][] = [[1, 2], [3, 4]]
let copia: int[][] = []
func f(r: ref int) -> void
    copia = a
    *r = 999
end
f(ref a[0][0])
test_report([copia[0], a[0]])`)
		assertIsolatedAndWritten(t, got, []int64{1, 2}, []int64{999, 2})
	})

	t.Run("ancestral é um map", func(t *testing.T) {
		got := captureVMSource(t, `let m: map[string, int[]] = {"k": [1, 2, 3]}
let copia: map[string, int[]] = {}
func f(r: ref int) -> void
    copia = m
    *r = 999
end
f(ref m["k"][0])
test_report([copia["k"], m["k"]])`)
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})

	t.Run("REF_PROPERTY com ancestral struct", func(t *testing.T) {
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
test_report([[copia.inn.x], [o.inn.x]])`)
		assertIsolatedAndWritten(t, got, []int64{1}, []int64{999})
	})

	t.Run("G combinado com F: o alias viaja dentro de um valor E está um nível acima", func(t *testing.T) {
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
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})
}

// A ARMADILHA DA §1.3, no caso em que ela era falsa nas DUAS direções.
//
// Antes: `copia = h` dava um 2º dono à instância; `h.xs[1] = 7` fazia o MUT
// clonar a instância, o clone retinha `xs`, `xs` virava compartilhado e
// `GET_PROP_MUT` o clonava — deixando o empréstimo apontando para o array
// VELHO, que a essa altura só a cópia enxergava. Saíam os dois modos de falha
// juntos: escrita perdida no original E vazamento na cópia.
//
// É o teste que separa esta correção do conserto errado. Uma implementação que
// unicize sem gravar de volta perde a escrita aqui.
func TestBorrowPlaceSurvivesCowForkOfTheAncestor(t *testing.T) {
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
test_report([copia.xs, h.xs])`)
	assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 7, 3})
}

// Mutação de FORMA da raiz durante o empréstimo. Não é vazamento de semântica
// de valor; é a pergunta "para onde aponta um lugar cujo contêiner mudou".
// Como o empréstimo denota um LUGAR e não um objeto, a resposta é o lugar atual.
func TestBorrowPlaceUnderRootMutation(t *testing.T) {
	t.Run("raiz reatribuída: a escrita chega no lugar, não se perde", func(t *testing.T) {
		// Antes a escrita sumia no array órfão e `arr` ficava [7, 7, 7].
		got := captureVMSource(t, `let arr: int[] = [1, 2, 3]
func f(r: ref int) -> void
    arr = [7, 7, 7]
    *r = 999
end
f(ref arr[0])
test_report([arr])`)
		assertIntRows(t, got, [][]int64{{999, 7, 7}})
	})

	t.Run("entrada de map apagada: erro, em vez de ressuscitar a chave", func(t *testing.T) {
		// Antes a escrita RECRIAVA a chave que o programa mandou apagar,
		// porque o ramo REF_INDEX de map usa mapping.Set, que insere.
		err := interpretOrCompileErr(t, New(), `let m: map[string, int] = {"a": 1, "b": 2}
func f(r: ref int) -> void
    delete(ref m, "a")
    *r = 999
end
f(ref m["a"])`)
		if err == nil || !strings.Contains(err.Error(), "no longer exists") {
			t.Fatalf("err=%v, want 'reference target no longer exists'", err)
		}
	})

	t.Run("pop durante o empréstimo continua errando alto", func(t *testing.T) {
		err := interpretOrCompileErr(t, New(), `let arr: int[] = [1, 2, 3]
func f(r: ref int) -> void
    pop(ref arr)
    pop(ref arr)
    pop(ref arr)
    *r = 999
end
f(ref arr[0])`)
		if err == nil {
			t.Fatalf("escrever num índice que não existe mais tem de errar")
		}
	})
}

// CONTROLES NEGATIVOS: o que a correção não pode ter quebrado. São os idiomas
// que a issue #82 estabeleceu, e o custo de um falso positivo aqui é alto.
func TestBorrowPlaceKeepsWorkingIdioms(t *testing.T) {
	t.Run("escrita através do empréstimo, sem ninguém compartilhando", func(t *testing.T) {
		got := captureVMSource(t, `func inc(n: ref int) -> void
    *n = *n + 1
end
let arr: int[] = [1, 2, 3]
inc(ref arr[0])
test_report([arr])`)
		assertIntRows(t, got, [][]int64{{2, 2, 3}})
	})

	t.Run("cópia feita ANTES do ref já era isolada e continua", func(t *testing.T) {
		got := captureVMSource(t, `let arr: int[] = [1, 2, 3]
let copia: int[] = arr
func f(r: ref int) -> void
    *r = 999
end
f(ref arr[0])
test_report([copia, arr])`)
		assertIsolatedAndWritten(t, got, []int64{1, 2, 3}, []int64{999, 2, 3})
	})

	t.Run("dois empréstimos na mesma chamada, contêineres compartilhados", func(t *testing.T) {
		got := captureVMSource(t, `let a: int[] = [1, 2, 3]
let b: int[] = a
func f(x: ref int, y: ref int) -> void
    *x = 111
    *y = 222
end
f(ref a[0], ref b[0])
test_report([a, b])`)
		assertIntRows(t, got, [][]int64{{111, 2, 3}, {222, 2, 3}})
	})

	t.Run("empréstimo sobre base que já é ref", func(t *testing.T) {
		got := captureVMSource(t, `func inc(n: ref int) -> void
    *n = *n + 1
end
func viaRef(xs: ref int[]) -> void
    inc(ref xs[0])
end
let arr: int[] = [1, 2, 3]
viaRef(ref arr)
test_report([arr])`)
		assertIntRows(t, got, [][]int64{{2, 2, 3}})
	})

	t.Run("empréstimo enraizado num local capturado por closure", func(t *testing.T) {
		got := captureVMSource(t, `func inc(n: ref int) -> void
    *n = *n + 1
end
func corpo() -> int[]
    let arr: int[] = [1, 2, 3]
    let copia: int[] = arr
    inc(ref arr[0])
    return arr
end
test_report([corpo()])`)
		assertIntRows(t, got, [][]int64{{2, 2, 3}})
	})

	t.Run("caminho fundo: struct em array em struct", func(t *testing.T) {
		got := captureVMSource(t, `struct Cell
    v: int
end
struct Grid
    linhas: Cell[]
end
let g: Grid = Grid([Cell(1), Cell(2)])
let copia: Grid = Grid([])
func f(r: ref int) -> void
    copia = g
    *r = 999
end
f(ref g.linhas[0].v)
test_report([[g.linhas[0].v], [copia.linhas[0].v]])`)
		assertIntRows(t, got, [][]int64{{999}, {1}})
	})
}
