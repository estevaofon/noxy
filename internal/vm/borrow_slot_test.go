package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// Slot no ObjRef (issue #93b): OP_REF_PROPERTY resolve o indice do campo uma
// vez, na criacao; descend/referenceStorageMode usam Slots[Slot] quando a
// definicao da instancia tem esse nome nesse slot. O Slot e uma DICA: um
// ObjRef montado a mao (natives, testes) deixa zero, e a instancia no lugar
// pode ter outra definicao (json_loads monta a sua em ordem alfabetica) —
// nos dois casos a guarda cai no FieldIndex por nome, como antes.

func TestRefPropertyWrongSlotHintFallsBackToName(t *testing.T) {
	def := value.NewStruct("P", []string{"a", "b"}).Obj.(*value.ObjStruct)
	inst := value.NewInstance(def)
	instObj := inst.Obj.(*value.ObjInstance)
	instObj.Slots[0] = value.NewInt(10)
	instObj.Slots[1] = value.NewInt(20)

	// Slot 1 aponta para "b", mas o ref e de "a": a guarda tem de ignorar a dica.
	ref := &value.ObjRef{RefType: value.REF_PROPERTY, Container: inst, Name: "a", Slot: 1}
	machine := New()
	got, err := machine.lookupReferenceValue(ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.Int() != 10 {
		t.Fatalf("leitura por ref com Slot errado leu %s, esperava 10 (campo a)", got.String())
	}
	if err := machine.storeReferenceValue(value.Value{Type: value.VAL_REF, Obj: ref}, value.NewInt(99)); err != nil {
		t.Fatal(err)
	}
	if instObj.Slots[0].Int() != 99 || instObj.Slots[1].Int() != 20 {
		t.Fatalf("escrita por ref com Slot errado: slots=%s/%s, esperava 99/20", instObj.Slots[0].String(), instObj.Slots[1].String())
	}

	// Slot fora da faixa tambem e so uma dica invalida.
	ref.Slot = 7
	if got, err := machine.lookupReferenceValue(ref); err != nil || got.Int() != 99 {
		t.Fatalf("Slot fora da faixa: got %v, %v", got, err)
	}
	// Campo inexistente continua sendo erro, com a mesma mensagem.
	ref.Name = "zzz"
	if _, err := machine.lookupReferenceValue(ref); err == nil || err.Error() != "undefined property 'zzz'" {
		t.Fatalf("esperava undefined property 'zzz', obtido %v", err)
	}
}

// A instancia que o json_loads cria tem os campos em ordem alfabetica
// ([proximo, valor], nao [valor, proximo]). O `ref xs[0].valor` resolve o
// Slot na propria instancia (1), e a escrita atraves dele chega no campo
// certo; a copia por valor `n` continua independente.
func TestBorrowOfReorderedJSONInstanceFieldWritesTheRightSlot(t *testing.T) {
	got := captureVMSource(t, `
struct Node
    valor: int
    proximo: ref Node
end
func bump(r: ref int) -> void
    *r = *r + 100
end
let xs: Node[] = []
let ok: bool = json_loads("[{\"valor\": 7, \"proximo\": null}]", ref xs)
let n: Node = xs[0]
bump(ref xs[0].valor)
let r: ref int = ref xs[0].valor
*r = *r + 1
test_report([to_str(ok), to_str(xs[0].valor), to_str(n.valor), to_str(xs[0].proximo == null)])
`)
	reportedStrings(t, got, []string{"true", "108", "7", "true"})
}

// BST por posse com chaves ordenadas: o caminho do emprestimo com Slot da o
// mesmo resultado que antes (os sete repros da #83 sao o criterio de
// semantica; este e o de resultado no caso profundo).
func TestBorrowSlotDeepOwnedTreeMatches(t *testing.T) {
	got := captureVMSource(t, `
struct TreeNode
    valor: int
    esquerda: TreeNode?
    direita: TreeNode?
end
func insert(node: ref (TreeNode?), v: int) -> void
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
func soma(node: ref (TreeNode?)) -> int
    if *node == null then
        return 0
    end
    let v: int = node.valor
    let l: int = soma(ref node.esquerda)
    let r: int = 0
    if *node != null then
        r = soma(ref node.direita)
    end
    return v + l + r
end
let raiz: TreeNode? = null
let seed: int = 12345
let i: int = 0
while i < 500 do
    seed = (seed * 1103515245 + 12345) % 2147483648
    insert(ref raiz, seed % 1000)
    i = i + 1
end
let copia: TreeNode? = raiz
insert(ref raiz, 5000)
test_report([to_str(soma(ref raiz) - soma(ref copia))])
`)
	reportedStrings(t, got, []string{"5000"})
}
