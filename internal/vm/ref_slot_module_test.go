package vm

// Struct com campo `ref` definido em OUTRO modulo: o ObjStruct nasce no
// compilador do modulo (mesmo case *ast.StructStatement), entao RefFields
// chega preenchido e a base `any` encaminha o campo ref como a base tipada
// (spec 2026-08-20-ref-slot-invariant §6.1/§6.2 — "struct de outro modulo" e
// a motivacao declarada da consulta em runtime).

import "testing"

func TestImportedStructRefFieldForwardsThroughAnyBase(t *testing.T) {
	root := t.TempDir()
	write(t, root, "lista.nx", `
struct Node
    valor: int
    proximo: ref Node
end
func eh_nulo(n: ref Node) -> bool
    return n == null
end
func preenche(n: ref Node)
    *n = Node(7, null)
end
`)
	got := captureVMSourceAtRoot(t, root, `
use lista select *
let b: Node = Node(2, null)
let a: any = Node(1, ref b)
preenche(a.proximo)
let atravessou: int = b.valor
a.proximo = null
let nulo_via_any: bool = eh_nulo(a.proximo)
if atravessou == 7 && nulo_via_any then
    test_report(7)
else
    test_report(0)
end
`)
	expectInt(t, got, 7, "campo ref de struct importado encaminha via base any")
}
