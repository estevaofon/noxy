package vm

// Contrato de json_loads para slots `ref T` (spec 2026-08-20-ref-slot-
// invariant §5.1): slot ja apontando escreve ATRAVES; payload null limpa;
// slot nulo/novo com payload nao-nulo ganha celula heap + ref; alvo direto
// nulo devolve false.

import "testing"

func TestJSONLoadsNewRefElementIsAReference(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Pair
    a: int
    b: int
end
let target: (ref Pair)[] = []
let ok: bool = json_loads("[{\"a\":3,\"b\":4}]", target)
let viz: ref Pair = target[0]
if ok && type(ref viz) == "ref" && viz.a * 10 + viz.b == 34 then
    test_report(34)
else
    test_report(999)
end`)
	testExpectedObject(t, 34, got)
}

func TestJSONLoadsNullRefFieldViaOwnerGetsCell(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Holder
    child: ref int
end
let h: Holder = Holder(null)
let ok: bool = json_loads("{\"child\": 5}", h)
let viz: ref int = h.child
if ok && type(ref viz) == "ref" && *viz == 5 then
    test_report(5)
else
    test_report(999)
end`)
	testExpectedObject(t, 5, got)
}

func TestJSONLoadsRefSlotAlreadyPointingWritesThrough(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let backing: int = 7
let target: (ref int)[] = [ref backing]
let ok: bool = json_loads("[42]", target)
let viz: ref int = target[0]
if ok && backing == 42 && *viz == 42 then
    test_report(42)
else
    test_report(999)
end`)
	testExpectedObject(t, 42, got)
}

func TestJSONLoadsDirectNullRefIntSlotReturnsFalse(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Holder
    child: ref int
end
let h: Holder = Holder(null)
let ok: bool = json_loads("5", h.child)
if !ok && h.child == null then
    test_report(1)
else
    test_report(0)
end`)
	testExpectedObject(t, 1, got)
}

// A celula e possuidora (Owners=1): mutar uma copia por valor do referente
// nao altera o que o slot aponta (o `let` da copia leva Owners a 2 e clona).
func TestJSONLoadsCellOwnsItsReferent(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Pair
    a: int
    b: int
end
let target: (ref Pair)[] = [null]
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", target)
let copia: Pair = target[0]
copia.a = 99
let viz: ref Pair = target[0]
if ok && viz.a == 1 then
    test_report(1)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}
