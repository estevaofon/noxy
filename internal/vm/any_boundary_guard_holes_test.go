package vm

import (
	"strings"
	"testing"
)

// Revisao do PR #119: tres sites de slot tipado ficaram sem a guarda da
// fronteira — `*r = v`, o caminho fundido de `xs[i] = v` com xs local e
// `append(ref xs, v)`. Todos com v de tipo estatico `any`.

func TestAnyBoundaryGuardDerefAssignmentFromAny(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyBoundaryTexto+"let x: int = 1\nlet r: ref int = ref x\n*r = nativo()\nprint(x)\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardFusedLocalIndexAssignmentFromAny(t *testing.T) {
	// xs local + indice e valor sem efeito colateral = tryFuseLocalIndexAssign.
	err := interpretOrCompileErr(t, New(), "func f() -> int\n    let xs: int[] = [1, 2]\n    let v: any = \"texto\"\n    xs[0] = v\n    return xs[0]\nend\nf()\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardFusedRefLocalIndexAssignmentFromAny(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "func f(xs: ref int[]) -> int\n    let v: any = \"texto\"\n    xs[0] = v\n    return xs[0]\nend\nlet a: int[] = [1, 2]\nf(ref a)\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardAppendFromAny(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyBoundaryTexto+"let xs: int[] = [1]\nappend(ref xs, nativo())\nprint(xs)\n")
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyBoundaryGuardAppendLetsMatchingValueThrough(t *testing.T) {
	got := captureVMSource(t, "func nativo() -> any\n    return 7\nend\nlet xs: int[] = [1]\nappend(ref xs, nativo())\ntest_report(xs[1])\n")
	testExpectedObject(t, 7, got)
}
