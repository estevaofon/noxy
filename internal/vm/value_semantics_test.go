package vm

// Testes de contrato da semântica de valor com copy-on-write.
// Spec: docs/superpowers/specs/2026-08-16-cow-value-semantics-design.md §2.

import (
	"testing"

	"noxy-vm/internal/value"
)

func expectInt(t *testing.T, got value.Value, want int64, msg string) {
	t.Helper()
	if got.Type != value.VAL_INT || got.Int() != want {
		t.Fatalf("%s: esperado %d, veio %s (%v)", msg, want, got.String(), got.Type)
	}
}

func TestAssignmentIsValueCopy(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let a: int[]
    append(ref a, 1)
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
    append(ref a, 1)
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
    append(ref a, P(1))
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
    append(ref a, P(1))
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
    append(ref a, P(1))
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
    append(ref a, 1)
    bump(ref a)
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
    poke(ref p)
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

func TestCallArgIsDeepIndependent(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func poke(items: P[]) -> void
    items[0].x = 99
end

func main()
    let a: P[]
    append(ref a, P(1))
    poke(a)
    test_report(a[0].x)
end
main()
`)
	expectInt(t, got, 1, "mutação aninhada via parâmetro não pode vazar")
}

func TestCalleeMutationOfArgDoesNotLeak(t *testing.T) {
	got := captureVMSource(t, `
func poke(items: int[]) -> void
    items[0] = 99
end

func main()
    let a: int[]
    append(ref a, 1)
    poke(a)
    test_report(a[0])
end
main()
`)
	expectInt(t, got, 1, "mutação direta do parâmetro não pode vazar")
}

func TestReadOnlyCallDoesNotClone(t *testing.T) {
	machine := New()
	machine.DefineNative("test_reset_clones", func(args []value.Value) value.Value {
		ResetCloneCount()
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, `
func total(data: int[]) -> int
    let s: int = 0
    let i: int = 0
    while i < length(data) do
        s = s + data[i]
        i = i + 1
    end
    return s
end

func main()
    let data: int[]
    let i: int = 0
    while i < 50 do
        append(ref data, i)
        i = i + 1
    end
    test_reset_clones()
    let s: int = 0
    i = 0
    while i < 20 do
        s = s + total(data)
        i = i + 1
    end
end
main()
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if n := CloneCountValue(); n != 0 {
		t.Fatalf("20 chamadas só-leitura deveriam custar 0 clones, veio %d", n)
	}
}

// vmWithCloneReset devolve uma VM com o native de teste test_reset_clones,
// para zerar o contador de clones depois do setup do programa.
func vmWithCloneReset() *VM {
	machine := New()
	machine.DefineNative("test_reset_clones", func(args []value.Value) value.Value {
		ResetCloneCount()
		return value.NewNull()
	})
	return machine
}

func TestHasKeyThenWriteDoesNotClone(t *testing.T) {
	machine := vmWithCloneReset()
	if err := interpretVMSource(t, machine, `
func main()
    let m: map[string, string] = {}
    test_reset_clones()
    let i: int = 0
    while i < 20 do
        let e: bool = has_key(m, "key:" + to_str(i))
        m["key:" + to_str(i)] = "v"
        i = i + 1
    end
end
main()
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if n := CloneCountValue(); n != 0 {
		t.Fatalf("has_key intercalado com escrita deveria custar 0 clones, veio %d", n)
	}
}

func TestKeysThenWriteDoesNotClone(t *testing.T) {
	machine := vmWithCloneReset()
	if err := interpretVMSource(t, machine, `
func main()
    let m: map[string, string] = {}
    test_reset_clones()
    let i: int = 0
    while i < 20 do
        let ks: string[] = keys(m)
        m["key:" + to_str(i)] = "v"
        i = i + 1
    end
end
main()
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if n := CloneCountValue(); n != 0 {
		t.Fatalf("keys intercalado com escrita deveria custar 0 clones, veio %d", n)
	}
}

// Caso negativo: native sem assinatura fora da allowlist tem que continuar
// marcando os args (default conservador) — a escrita seguinte deve clonar.
func TestUnlistedNativeStillMarksArgs(t *testing.T) {
	machine := vmWithCloneReset()
	machine.DefineNative("test_observe", func(args []value.Value) value.Value {
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, `
func main()
    let m: map[string, string] = {}
    m["a"] = "1"
    test_reset_clones()
    test_observe(m)
    m["b"] = "2"
end
main()
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if n := CloneCountValue(); n != 1 {
		t.Fatalf("native fora da allowlist deve marcar o arg: escrita seguinte deveria clonar 1x, veio %d", n)
	}
}

func TestChanSendDeliversIndependentValue(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let c: any = make_chan(1)
    let a: int[]
    append(ref a, 1)
    chan_send(c, a)
    a[0] = 99
    let b: int[] = chan_recv(c)
    test_report(b[0])
end
main()
`)
	expectInt(t, got, 1, "payload de canal deve ser valor independente")
}

func TestSpawnArgIsIndependent(t *testing.T) {
	got := captureVMSource(t, `
use time

func worker(data: int[], c: any) -> void
    time.sleep(50)
    chan_send(c, data[0])
end

func main()
    let c: any = make_chan(1)
    let a: int[]
    append(ref a, 1)
    spawn(worker, a, c)
    a[0] = 99
    let seen: int = chan_recv(c)
    test_report(seen)
end
main()
`)
	expectInt(t, got, 1, "spawn não pode mais encaminhar identidade do argumento")
}

func TestStructConstructorArgIsIndependent(t *testing.T) {
	got := captureVMSource(t, `
struct Box
    data: int[]
end

func main()
    let a: int[]
    append(ref a, 1)
    let b: Box = Box(a)
    a[0] = 99
    test_report(b.data[0])
end
main()
`)
	expectInt(t, got, 1, "arg de construtor guardado no campo deve ser independente")
}

func TestArrayLiteralElementIsIndependent(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let inner: int[]
    append(ref inner, 1)
    let outer: int[][] = [inner]
    inner[0] = 99
    test_report(outer[0][0])
end
main()
`)
	expectInt(t, got, 1, "elemento de literal de array deve ser independente da origem")
}

func TestDeferCapturesValueAtDeclaration(t *testing.T) {
	got := captureVMSource(t, `
let seen: int = 0

func observe(data: int[]) -> void
    seen = data[0]
end

func run() -> void
    let a: int[]
    append(ref a, 1)
    defer observe(a)
    a[0] = 99
end

func main()
    run()
    test_report(seen)
end
main()
`)
	expectInt(t, got, 1, "defer captura o valor no momento da declaração")
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
    append(ref a, P(0))
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
