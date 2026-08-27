package vm

import (
	"testing"

	"fmt"

	"noxy-vm/internal/value"
)

// Contrato da spec §2.2 regra 6 / §4.3 / §5 Self-Reference (issue #92): uma
// copia e independente atraves de todo campo de VALOR, em qualquer
// profundidade; um campo declarado `ref T` e uma aresta compartilhada — a copia
// carrega a mesma aresta, e a escrita atraves dela chega no alvo do original.
// A diferenca esta so na DECLARACAO do struct: a atribuicao e a chamada sao
// identicas nos dois programas.
//
// copyValue (calls.go) clona um nivel e retem os filhos; um VAL_REF cai no
// `default: return v` — e isso, nao um bug, e o que este teste fixa.

func reportedInts(t *testing.T, got value.Value, want int) []int64 {
	t.Helper()
	arr, ok := got.Obj.(*value.ObjArray)
	if !ok || len(arr.Elements) != want {
		t.Fatalf("test_report deveria receber um array de %d ints, veio %s", want, got.String())
	}
	out := make([]int64, want)
	for i, el := range arr.Elements {
		if el.Type != value.VAL_INT {
			t.Fatalf("elemento %d nao e int: %s", i, el.String())
		}
		out[i] = el.Int()
	}
	return out
}

func TestRefFieldIsASharedEdgeAcrossCopies(t *testing.T) {
	const program = `struct Node
    value: int
    next: ref Node
end
struct Holder
    tag: string
    inner: %s
end
func touch(h: Holder) -> void
    h.inner.value = 777
end
let target: Node = Node(1, null)
let holder: Holder = Holder("a", %s)
let copia: Holder = holder
copia.inner.value = 99
let after_copy: int = target.value
touch(holder)
let after_call: int = target.value
test_report([after_copy, after_call, holder.inner.value, copia.inner.value])`

	t.Run("campo ref: original e copia alcancam o mesmo alvo", func(t *testing.T) {
		src := fmt.Sprintf(program, "ref Node", "ref target")
		got := reportedInts(t, captureVMSource(t, src), 4)
		// A copia e a chamada por valor escrevem no alvo — o Holder e ref-carrying.
		if got[0] != 99 || got[1] != 777 || got[2] != 777 || got[3] != 777 {
			t.Fatalf("esperado [99 777 777 777] (aresta compartilhada), veio %v", got)
		}
	})

	t.Run("campo por posse: original e copia sao independentes", func(t *testing.T) {
		src := fmt.Sprintf(program, "Node", "target")
		got := reportedInts(t, captureVMSource(t, src), 4)
		// Mesmo programa, so a declaracao do campo muda: nada vaza.
		if got[0] != 1 || got[1] != 1 || got[2] != 1 || got[3] != 99 {
			t.Fatalf("esperado [1 1 1 99] (posse), veio %v", got)
		}
	})
}

// Idioma por posse da §5: filhos nulaveis sem `ref`, mutacao pelo ref ao SLOT.
// A copia de uma arvore e independente em profundidade e o callee por valor
// nao alcanca a arvore do chamador — e o mesmo vale para o struct generic.
func TestOwnedRecursiveFieldsKeepValueSemantics(t *testing.T) {
	got := reportedInts(t, captureVMSource(t, `struct TreeNode
    valor: int
    esquerda: TreeNode
    direita: TreeNode
end
func insert(node: ref TreeNode, v: int) -> void
    if *node == null then
        *node = TreeNode(v, null, null)
        return
    end
    if v < node.valor then
        insert(ref node.esquerda, v)
    else
        insert(ref node.direita, v)
    end
end
func bump(node: TreeNode) -> int
    if node == null then
        return 0
    end
    node.valor = node.valor + 1000
    return node.valor + bump(node.esquerda) + bump(node.direita)
end
struct GNode<T>
    value: T,
    next: GNode<T>
end
func push_back<T>(node: ref GNode<T>, v: T) -> void
    if *node == null then
        *node = GNode(v, null)
        return
    end
    push_back(ref node.next, v)
end
let raiz: TreeNode = null
insert(ref raiz, 50)
insert(ref raiz, 30)
insert(ref raiz, 70)
let copia: TreeNode = raiz
copia.esquerda.valor = 999
let bumped: int = bump(raiz)
let head: GNode<int> = null
push_back(ref head, 1)
push_back(ref head, 2)
let lcopia: GNode<int> = head
lcopia.next.value = 99
test_report([raiz.esquerda.valor, copia.esquerda.valor, bumped, raiz.valor, head.next.value, lcopia.next.value])`), 6)
	want := []int64{30, 999, 3150, 50, 2, 99}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("esperado %v, veio %v", want, got)
		}
	}
}
