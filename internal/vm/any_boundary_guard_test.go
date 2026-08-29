package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
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

// Issue #120 item 2: atribuicao (`x = v`, `s.f = v`, `xs[i] = v`) tem a mesma
// guarda do let anotado para um valor de tipo estatico `any`.

const anyBoundaryTexto = "func nativo() -> any\n    return \"texto\"\nend\n"

func TestAnyBoundaryGuardAssignmentLocalMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyBoundaryTexto+"func f() -> int\n    let x: int = 1\n    x = nativo()\n    return x\nend\nf()\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardAssignmentGlobalMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyBoundaryTexto+"let x: int = 1\nx = nativo()\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardAssignmentUpvalueMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyBoundaryTexto+"func f() -> int\n    let x: int = 1\n    let set = func() -> void\n        x = nativo()\n    end\n    set()\n    return x\nend\nf()\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardAssignmentFieldMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyBoundaryTexto+"struct P\n    x: int\nend\nlet p: P = P(1)\np.x = nativo()\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardAssignmentIndexMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyBoundaryTexto+"let xs: int[] = [1]\nxs[0] = nativo()\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardAssignmentMapValueMismatch(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyBoundaryTexto+"let m: map[string, int] = {\"a\": 1}\nm[\"a\"] = nativo()\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardAssignmentLetsMatchingValuesThrough(t *testing.T) {
	got := captureVMSource(t, `
func nativo() -> any
    return 7
end
struct P
    x: int
end
let g: int = 0
g = nativo()
func f() -> int
    let x: int = 1
    x = nativo()
    let p: P = P(1)
    p.x = nativo()
    let xs: int[] = [1]
    xs[0] = nativo()
    let m: map[string, int] = {"a": 1}
    m["a"] = nativo()
    return x + p.x + xs[0] + m["a"] + g
end
test_report(f())`)
	testExpectedObject(t, 35, got)
}

func TestAnyBoundaryGuardSkipsUnknownStaticType(t *testing.T) {
	// Tipo estatico DESCONHECIDO (nativo sem assinatura, membro de namespace)
	// NAO e checado por esta guarda: 134 natives da stdlib nao tem contrato
	// de retorno declarado e a guarda custava +35-55 % por chamada de wrapper
	// (revisao do #119, decisao B). O contrato de retorno dos natives e uma
	// issue propria; ate la o comportamento anterior fica.
	machine := New()
	machine.DefineNative("__untyped_null", func(args []value.Value) value.Value {
		return value.NewNull()
	})
	captured := value.NewInt(-1)
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	err := interpretVMSource(t, machine, `
struct P
    x: int
end
func wrapper() -> P
    return __untyped_null()
end
let p: P = wrapper()
test_report(p)`)
	if err != nil {
		t.Fatalf("unknown static type must keep the pre-#118 behavior, got %v", err)
	}
	if captured.Type != value.VAL_NULL {
		t.Fatalf("want null through the untyped wrapper, got %v", captured)
	}
}
