package vm

import (
	"strings"
	"testing"
)

// Issue #118 item 1: a fronteira `any` -> slot tipado e checada em runtime em
// TODAS as posicoes (let anotado, argumento, return), com `expected T, got U`.
// Antes o let deixava passar um primitivo errado em silencio (x: int com
// "texto" dentro) e return/argumento nem compilavam.

func TestAnyBoundaryGuardLetPrimitiveMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
func nativo() -> any
    return "texto"
end
let x: int = nativo()`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardArgumentMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
func add(a: int) -> int
    return a + 1
end
func nativo() -> any
    return "texto"
end
add(nativo())`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardReturnMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
func nativo() -> any
    return "texto"
end
func tipado() -> int
    return nativo()
end
tipado()`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardStructMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
struct P
    x: int
end
func nativo() -> any
    return 42
end
let p: P = nativo()`)
	if err == nil || !strings.Contains(err.Error(), "expected P, got int") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardCompositeMismatchNamesBothTypes(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
func nativo() -> any
    let xs: int[] = [1, 2]
    return xs
end
func conta(xs: map[string, any][]) -> int
    return length(xs)
end
conta(nativo())`)
	if err == nil || !strings.Contains(err.Error(), "expected map[string, any][], got int[]") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardNullIntoNonNullableSlot(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
func nativo() -> any
    return null
end
func tipado() -> int
    return nativo()
end
tipado()`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got null\n  hint: declare the slot as 'int?' to allow null") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardLetsMatchingValuesThrough(t *testing.T) {
	got := captureVMSource(t, `
func add(a: int) -> int
    return a + 1
end
func nativo() -> any
    return 41
end
func tipado() -> int
    return nativo()
end
func itens() -> any
    return [{"a": 1}, {"b": 2}]
end
func conta(xs: map[string, any][]) -> int
    return length(xs)
end
func scan() -> map[string, any][]
    return itens()
end
let x: any = 41
test_report(add(x) * 1000 + tipado() * 10 + conta(itens()) + conta(scan()))`)
	testExpectedObject(t, 42*1000+41*10+2+2, got)
}
