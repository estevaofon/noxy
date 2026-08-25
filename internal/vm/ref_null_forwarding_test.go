package vm

// Encaminhamento de `ref T` nulo vindo de campo/indice (spec §2.3 regra 2 e
// §4.2): um argumento cujo tipo estatico ja e `ref T` nao sofre conversao
// contextual — o valor armazenado e encaminhado como esta, inclusive `null`.
// Antes, OP_CONTEXT_REF_PROPERTY/OP_CONTEXT_REF_INDEX fabricavam uma ref
// para o SLOT quando ele continha null, e `n == null` dentro da funcao dava
// false (ref valida para um slot que contem null). Variavel local ja
// encaminhava; campo, indice de array e chave de map nao.

import "testing"

const refNullForwardingPrelude = `
struct Node
    valor: int
    proximo: ref Node
end
func eh_nulo(n: ref Node) -> bool
    return n == null
end
`

// Campo `ref Node` contendo null passado a parametro `ref Node`.
func TestNullRefFieldForwardsNullToRefParameter(t *testing.T) {
	requireBoolResults(t, refNullForwardingPrelude+`
let a: Node = Node(1, null)
test_report([eh_nulo(a.proximo)])`, []bool{true})
}

// O mesmo campo, acessado atraves de uma base que e ela propria `ref Node`
// (o caso do append recursivo: `_append(node.proximo, v)` com `node: ref`).
// Antes: `n == null` dava false (ref para o slot); na recursao seguinte, com
// essa ref-para-slot como base e o slot contendo null, o deref da base dava
// null e o opcode morria com "contextual property reference base must be an
// instance".
func TestNullRefFieldThroughRefBaseForwardsNull(t *testing.T) {
	requireBoolResults(t, refNullForwardingPrelude+`
func via_ref(node: ref Node) -> bool
    return eh_nulo(node.proximo)
end
let a: Node = Node(1, null)
test_report([via_ref(ref a)])`, []bool{true})
}

// Elemento null de `(ref Node)[]`.
func TestNullRefArrayElementForwardsNullToRefParameter(t *testing.T) {
	requireBoolResults(t, refNullForwardingPrelude+`
let arr: (ref Node)[] = [null]
test_report([eh_nulo(arr[0])])`, []bool{true})
}

// Chave ausente em `map[string, ref Node]`: a leitura plana `m["x"]` da
// null, e o encaminhamento tem de dar o mesmo null.
func TestMissingMapKeyOfRefValueForwardsNullToRefParameter(t *testing.T) {
	requireBoolResults(t, refNullForwardingPrelude+`
let m: map[string, ref Node] = {}
let lido: ref Node = m["x"]
test_report([lido == null, eh_nulo(m["x"])])`, []bool{true, true})
}

// Guarda de regressao: campo/indice com ref NAO-nula continua encaminhando
// a ref existente (identidade preservada: escrever atraves do parametro
// altera o no original).
func TestNonNullRefFieldStillForwardsExistingReference(t *testing.T) {
	requireBoolResults(t, refNullForwardingPrelude+`
func marca(n: ref Node)
    n.valor = 99
end
let b: Node = Node(2, null)
let a: Node = Node(1, ref b)
let arr: (ref Node)[] = [ref b]
marca(a.proximo)
let via_campo: bool = b.valor == 99
b.valor = 2
marca(arr[0])
let via_indice: bool = b.valor == 99
test_report([eh_nulo(a.proximo), eh_nulo(arr[0]), via_campo, via_indice])`,
		[]bool{false, false, true, true})
}
