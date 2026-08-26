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

// Achados da validação adversarial contra o próprio conserto (2026-08-25). Um
// agente independente atacou a implementação e derrubou quatro coisas; estas
// são as regressões e os buracos que restavam, cada um travado aqui.

// O caminho de um empréstimo pode ATRAVESSAR um campo declarado `ref T` — é
// como toda estrutura ligada do Noxy é percorrida. A guarda de R1 do runtime
// ("slot 'next' already holds a reference") existe para o passo FINAL, onde
// não se toma referência de referência, e passou a disparar no passo
// INTERMEDIÁRIO quando a base virou uma cadeia de OP_REF_PROPERTY. Agora o
// compilador detecta o nível ref antes de emitir e compila aquele nível como
// VALOR: o ref guardado ali já é a referência de célula que a cadeia quer.
func TestBorrowPathCrossesRefTypedField(t *testing.T) {
	t.Run("campo ref intermediário", func(t *testing.T) {
		got := captureVMSource(t, `struct Node
    data: int,
    next: ref Node
end
let n2: Node = Node(2, null)
let n1: Node = Node(1, ref n2)
func setit(p: ref int) -> void
    *p = 99
end
setit(ref n1.next.data)
test_report([[n2.data]])`)
		assertIntRows(t, got, [][]int64{{99}})
	})

	t.Run("campo ref intermediário seguido de índice", func(t *testing.T) {
		got := captureVMSource(t, `struct Node
    xs: int[],
    next: ref Node
end
let n2: Node = Node([10, 20], null)
let n1: Node = Node([1, 2], ref n2)
func setit(p: ref int) -> void
    *p = 99
end
setit(ref n1.next.xs[0])
test_report([n2.xs])`)
		assertIntRows(t, got, [][]int64{{99, 20}})
	})

	t.Run("elemento de (ref T)[] como base", func(t *testing.T) {
		got := captureVMSource(t, `struct Node
    data: int,
    next: ref Node
end
let n2: Node = Node(2, null)
let refs: (ref Node)[] = [ref n2]
func setit(p: ref int) -> void
    *p = 99
end
setit(ref refs[0].data)
test_report([[n2.data]])`)
		assertIntRows(t, got, [][]int64{{99}})
	})
}

// A identidade de um empréstimo é o CAMINHO, não o objeto congelado no instante
// da criação. Com dois roots ainda preguiçosamente compartilhados (`let b = a`),
// comparar o contêiner dizia que `ref a[0].x` e `ref b[0].x` eram a MESMA
// referência — e o próprio programa prova que não são: escrever num deixa o
// outro intacto.
func TestBorrowIdentityIsThePath(t *testing.T) {
	t.Run("lugares diferentes em roots compartilhados não são iguais", func(t *testing.T) {
		got := captureVMSource(t, `struct S
    x: int
end
let a: S[] = [S(1), S(2)]
let b: S[] = a
let r1: ref int = ref a[0].x
let r2: ref int = ref b[0].x
*r1 = 99
test_report([[a[0].x], [b[0].x]])`)
		assertIntRows(t, got, [][]int64{{99}, {1}})
	})

	t.Run("mesma raiz, passos intermediários diferentes", func(t *testing.T) {
		// `ref a[0].x` e `ref a[1].x` partem da MESMA raiz e têm o MESMO passo
		// final. Só os passos intermediários os separam — o caso que o
		// achatamento do caminho quase deixou passar.
		got := captureVMSource(t, `struct S
    x: int
end
let a: S[] = [S(1), S(2)]
let r1: ref int = ref a[0].x
let r2: ref int = ref a[1].x
*r1 = 99
test_report([[r1 == r2], [a[0].x], [a[1].x]])`)
		rows := semArray(t, got)
		iguais := semArray(t, rows[0])[0]
		if iguais.Type != value.VAL_BOOL || iguais.Bool() {
			t.Fatalf("ref a[0].x == ref a[1].x deu %s, want false", iguais.String())
		}
		if v := semArray(t, rows[1])[0]; v.Int() != 99 {
			t.Fatalf("a[0].x = %s, want 99", v.String())
		}
		if v := semArray(t, rows[2])[0]; v.Int() != 2 {
			t.Fatalf("a[1].x = %s, want 2", v.String())
		}
	})

	t.Run("== separa os dois, e junta duas referências ao mesmo lugar", func(t *testing.T) {
		got := captureVMSource(t, `struct S
    x: int
end
let a: S[] = [S(1), S(2)]
let b: S[] = a
let r1: ref int = ref a[0].x
let r2: ref int = ref b[0].x
let r3: ref int = ref a[0].x
test_report([[r1 == r2], [r1 == r3]])`)
		rows := semArray(t, got)
		first := semArray(t, rows[0])[0]
		second := semArray(t, rows[1])[0]
		if first.Type != value.VAL_BOOL || first.Bool() {
			t.Fatalf("ref a[0].x == ref b[0].x deu %s, want false", first.String())
		}
		if second.Type != value.VAL_BOOL || !second.Bool() {
			t.Fatalf("ref a[0].x == ref a[0].x deu %s, want true", second.String())
		}
	})
}

// O bug do #83 chegando por um site de escrita cujo tipo estático é `any`. Via
// base tipada o compilador emite a família *_MUT, que já unicizava; o caminho
// dinâmico (`func setx(p: any) -> void  p.x = 99 end`) resolvia a referência em
// modo de LEITURA e mutava o objeto compartilhado no lugar.
func TestBorrowWriteThroughAnyDoesNotLeak(t *testing.T) {
	cases := []struct {
		nome  string
		corpo string
	}{
		{"parâmetro any", `func escreve(p: any) -> void
    p.x = 99
end`},
		{"local any", `func escreve(r: ref S) -> void
    let a: any = r
    a.x = 99
end`},
		{"elemento de any[]", `func escreve(r: ref S) -> void
    let xs: any[] = [r]
    xs[0].x = 99
end`},
	}
	for _, tc := range cases {
		t.Run(tc.nome, func(t *testing.T) {
			got := captureVMSource(t, `struct S
    x: int
end
`+tc.corpo+`
let arr: S[] = [S(1), S(2)]
let copia: S[] = arr
escreve(ref arr[0])
test_report([[arr[0].x], [copia[0].x]])`)
			assertIsolatedAndWritten(t, got, []int64{99}, []int64{1})
		})
	}
}

// `json_loads` popula o alvo NO LUGAR. Se o objeto apontado estiver
// compartilhado, a mutação vazava — inclusive com `ref` a um local simples,
// sem empréstimo nenhum envolvido. Buraco pré-existente, da mesma família.
func TestJSONLoadsThroughRefDoesNotLeak(t *testing.T) {
	const decl = `struct U
    nome: string,
    idade: int
end
`
	t.Run("ref a um local", func(t *testing.T) {
		got := captureVMSource(t, decl+`let u: U = U("a", 1)
let copia: U = u
json_loads("{\"nome\":\"z\",\"idade\":9}", ref u)
test_report([[u.idade], [copia.idade]])`)
		assertIsolatedAndWritten(t, got, []int64{9}, []int64{1})
	})

	t.Run("campo declarado ref T, populado através dele", func(t *testing.T) {
		// Caminho separado dos outros dois: prepareJSONMutation desce pelo
		// campo `ref T` e muta o REFERENTE no lugar (jsonReferenceStorage).
		got := captureVMSource(t, `struct Inner
    v: int
end
struct Outer
    r: ref Inner
end
let i: Inner = Inner(1)
let copia: Inner = i
let o: Outer = Outer(ref i)
json_loads("{\"r\":{\"v\":9}}", ref o)
test_report([[i.v], [copia.v]])`)
		assertIsolatedAndWritten(t, got, []int64{9}, []int64{1})
	})

	t.Run("empréstimo para dentro de um array", func(t *testing.T) {
		got := captureVMSource(t, decl+`let arr: U[] = [U("a", 1)]
let copia: U[] = arr
json_loads("{\"nome\":\"z\",\"idade\":9}", ref arr[0])
test_report([[arr[0].idade], [copia[0].idade]])`)
		assertIsolatedAndWritten(t, got, []int64{9}, []int64{1})
	})
}

// O lugar apagado no MEIO do caminho tem de acusar a causa, não o sintoma um
// nível adiante ("Target is not an instance", que é erro de tipo).
func TestBorrowIntermediatePlaceDeleted(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `struct S
    x: int
end
let m: map[string, S] = {"k": S(1)}
func f(r: ref int) -> void
    delete(ref m, "k")
    *r = 99
end
f(ref m["k"].x)`)
	if err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Fatalf("err=%v, want 'reference target no longer exists'", err)
	}
}
