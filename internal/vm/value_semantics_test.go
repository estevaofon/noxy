package vm

// Testes de contrato da semântica de valor com copy-on-write.
// Spec: docs/superpowers/specs/2026-08-16-cow-value-semantics-design.md §2.

import (
	"testing"

	"noxy-vm/internal/value"
)

func expectInt(t *testing.T, got value.Value, want int64, msg string) {
	t.Helper()
	if got.Type != value.VAL_INT || got.AsInt != want {
		t.Fatalf("%s: esperado %d, veio %s (%v)", msg, want, got.String(), got.Type)
	}
}

func TestAssignmentIsValueCopy(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let a: int[]
    append(a, 1)
    let b: int[] = a
    b[0] = 99
    test_report(a[0])
end
main()
`)
	expectInt(t, got, 1, "atribuição deve ser cópia (a[0] intacto)")
}

func TestReassignmentIsValueCopy(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let a: int[]
    append(a, 1)
    let b: int[]
    b = a
    b[0] = 99
    test_report(a[0])
end
main()
`)
	expectInt(t, got, 1, "reatribuição deve ser cópia (a[0] intacto)")
}

func TestNestedAssignmentIsDeepIndependent(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func main()
    let a: P[]
    append(a, P(1))
    let b: P[] = a
    b[0].x = 99
    test_report(a[0].x)
end
main()
`)
	expectInt(t, got, 1, "mutação aninhada via cópia não pode vazar")
}

func TestReadFromContainerIsValueCopy(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func main()
    let a: P[]
    append(a, P(1))
    let p: P = a[0]
    p.x = 99
    test_report(a[0].x)
end
main()
`)
	expectInt(t, got, 1, "ler de contêiner e mutar o alias não pode vazar")
}

func TestPathMutationStillWorks(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func main()
    let a: P[]
    append(a, P(1))
    a[0].x = 42
    test_report(a[0].x)
end
main()
`)
	expectInt(t, got, 42, "mutação pelo caminho deve funcionar")
}

func TestGlobalPathMutationStillWorks(t *testing.T) {
	got := captureVMSource(t, `
let g: int[][] = [[1]]

func main()
    g[0][0] = 42
    test_report(g[0][0])
end
main()
`)
	expectInt(t, got, 42, "mutação de caminho em global deve funcionar")
}

func TestRefStillShares(t *testing.T) {
	got := captureVMSource(t, `
func bump(data: ref int[]) -> void
    data[0] = 77
end

func main()
    let a: int[]
    append(a, 1)
    bump(a)
    test_report(a[0])
end
main()
`)
	expectInt(t, got, 77, "ref continua compartilhando")
}

func TestRefStructFieldMutationStillShares(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func poke(p: ref P) -> void
    p.x = 88
end

func main()
    let p: P = P(1)
    poke(p)
    test_report(p.x)
end
main()
`)
	expectInt(t, got, 88, "mutação de campo através de ref continua compartilhando")
}

func TestMapValuePathMutation(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let m: map[string, int[]] = {"a": [1, 2]}
    m["a"][0] = 42
    test_report(m["a"][0])
end
main()
`)
	expectInt(t, got, 42, "mutação de caminho através de map deve funcionar")
}

func TestSingleOwnerPathMutationDoesNotClone(t *testing.T) {
	machine := New()
	machine.DefineNative("test_reset_clones", func(args []value.Value) value.Value {
		ResetCloneCount()
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, `
struct P
    x: int
end

func main()
    let a: P[]
    append(a, P(0))
    test_reset_clones()
    let i: int = 0
    while i < 100 do
        a[0].x = a[0].x + 1
        i = i + 1
    end
end
main()
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if n := CloneCountValue(); n > 2 {
		t.Fatalf("mutação single-owner em loop deveria custar no máximo ~2 clones (primeira unicização), veio %d", n)
	}
}
