package vm

// Runtime da igualdade estrita de ref (v0.7.1): OP_EQUAL deixa de resolver
// o caso misto VAL_REF vs nao-ref. Um ref comparado com null pergunta se o
// PROPRIO ref e nulo (antes, o deref implicito tornava `rv == null` true
// para um ref VALIDO apontando para um slot que contem null — a pergunta
// "ref nulo?" e "valor apontado nulo?" eram indistinguiveis). Na fronteira
// dinamica (`any`), um ref comparado com um valor e simplesmente diferente
// — nunca dereferenciado.

import (
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func requireBoolResults(t *testing.T, src string, want []bool) {
	t.Helper()
	got := captureVMSource(t, src)
	arr, ok := got.Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("esperava array de bools, veio %v", got)
	}
	if len(arr.Elements) != len(want) {
		t.Fatalf("esperava %d resultados, vieram %d", len(want), len(arr.Elements))
	}
	for i, expected := range want {
		if arr.Elements[i].Bool() != expected {
			t.Errorf("caso %d: esperava %v, veio %v", i, expected, arr.Elements[i].Bool())
		}
	}
}

// O caso-armadilha do null, agora com as duas perguntas expressaveis.
func TestRefNullComparisonAsksAboutTheRefItself(t *testing.T) {
	requireBoolResults(t, `
struct Ponto
    x: int
end
let vazio: Ponto? = null
let rv: ref (Ponto?) = ref vazio
let nulo: ref Ponto? = null
test_report([
    rv == null,
    rv != null,
    *rv == null,
    nulo == null,
    nulo != null
])`, []bool{false, true, true, true, false})
}

// Travessia classica continua identica: o terminador e um ref nulo.
func TestLinkedListTraversalUnchanged(t *testing.T) {
	got := captureVMSource(t, `
struct Node
    value: int,
    next: ref Node?
end
let n2: Node = Node(2, null)
let n1: Node = Node(1, ref n2)
let soma: int = 0
let cur: ref Node? = ref n1
while cur != null do
    soma = soma + cur.value
    cur = cur.next
end
test_report(soma)`)
	if got.Int() != 3 {
		t.Fatalf("travessia deveria somar 3, veio %d", got.Int())
	}
}

// Na fronteira dinamica, ref vs valor e diferente (sem deref); ref vs ref
// segue comparando identidade de slot. Nota sobre o canal: um ref cru nao
// entra numa VARIAVEL `any` (`let a: any = ref r` guarda copia deref'ada, e
// parametro `any` rejeita ref em runtime) — o caminho real e o acesso
// dinamico a membro, que devolve o campo `ref` sem deref.
func TestAnyBoundaryDoesNotDereference(t *testing.T) {
	requireBoolResults(t, `
struct Obs
    alvo: ref int
end
let t: int = 20
let o: Obs = Obs(ref t)
let d: any = o
let d2: any = o
test_report([
    d.alvo == 20,
    d.alvo != 20,
    d.alvo == null,
    d.alvo == d2.alvo
])`, []bool{false, true, false, true})
}

// O caminho explicito compara valores normalmente.
func TestExplicitDerefComparesValues(t *testing.T) {
	requireBoolResults(t, `
let x: int = 5
let y: int = 5
let ra: ref int = ref x
let rb: ref int = ref y
test_report([
    *ra == 5,
    *ra == *rb,
    ra == rb
])`, []bool{true, true, false})
}
