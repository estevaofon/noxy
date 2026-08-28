package vm

import (
	"strings"
	"testing"
)

// Spec §2.4 fase 2: o espelho de runtime — null vindo por `any` nao entra
// num slot struct/ref nu, e entra num `T?`.

func TestRuntimeRejectsNullIntoNonNullableStructFromAny(t *testing.T) {
	src := `struct P
    xs: int[]
end
let a: any = null
let p: P = a
`
	err := interpretOrCompileErr(t, New(), src)
	if err == nil || !strings.Contains(err.Error(), "null") {
		t.Fatalf("null into P from any must fail at runtime, got %v", err)
	}
	got := captureVMSource(t, `struct P
    xs: int[]
end
let a: any = null
let p: P? = a
test_report(to_str(p == null))
`)
	if got.String() != "true" {
		t.Fatalf("null into P? from any: %s", got.String())
	}
}

func TestRuntimeSharedListWithNullableRefField(t *testing.T) {
	src := `struct Cell
    v: int
    prox: ref Cell?
end
let c3: Cell = Cell(3, null)
let c2: Cell = Cell(2, ref c3)
let c1: Cell = Cell(1, ref c2)
let total: int = 0
let atual: ref Cell? = ref c1
while atual != null do
    total = total + atual.v
    atual = atual.prox
end
test_report(total)
`
	got := captureVMSource(t, src)
	if got.Int() != 6 {
		t.Fatalf("total: %v", got)
	}
}
