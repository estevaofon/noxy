package vm

import (
	"strings"
	"testing"
)

// print/to_str/f-string recebem o ref como valor e mostram a referencia
// (spec 2026-08-24-explicit-ref, decisao (a)); `*r` mostra o valor.
func TestToStrOfRefShowsReferenceNotValue(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let x: int = 42
    let r: ref int = ref x
    test_report([to_str(r), to_str(*r), f"{r}", f"{*r}"])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 4 {
		t.Fatalf("esperava 4 celulas, obtido %s", got.String())
	}
	asRef, _ := cells[0].Obj.(string)
	asVal, _ := cells[1].Obj.(string)
	fRef, _ := cells[2].Obj.(string)
	fVal, _ := cells[3].Obj.(string)
	if !strings.HasPrefix(asRef, "<ref") || !strings.HasPrefix(fRef, "<ref") {
		t.Fatalf("to_str(r)=%q f\"{r}\"=%q, want prefix <ref", asRef, fRef)
	}
	if asVal != "42" || fVal != "42" {
		t.Fatalf("to_str(*r)=%q f\"{*r}\"=%q, want 42", asVal, fVal)
	}
}

// `return *r` de composto continua devolvendo um valor independente
// (o OP_COPY do caminho antigo de return-deref e preservado).
func TestReturnDerefCompositeIsIndependentCopy(t *testing.T) {
	got := captureVMSource(t, `
func le(r: ref int[]) -> int[]
    return *r
end
func main()
    let xs: int[] = [1, 2]
    let copia: int[] = le(ref xs)
    append(ref copia, 3)
    test_report([length(xs), length(copia)])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 2 || cells[0].Int() != 2 || cells[1].Int() != 3 {
		t.Fatalf("[len xs, len copia] = %s, want [2, 3]", got.String())
	}
}

// R3 em runtime: `*x` sobre um valor que nao e ref (so alcancavel por tipo
// estatico desconhecido — membro dinamico de modulo/any nested) e erro.
func TestDerefOfNonRefAtRuntimeIsError(t *testing.T) {
	err := interpretVMSource(t, New(), `
struct Caixa
    v: any
end
func main()
    let c: Caixa = Caixa(7)
    let d: any = c
    let n: int = *d.v
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "cannot dereference int") {
		t.Fatalf("err = %v, want 'cannot dereference int'", err)
	}
}

// R1 em runtime: `ref` sobre um slot ref T alcancado por base any nao
// encaminha — e erro, espelhando o estatico 'is already a reference'.
func TestRefOfRefSlotThroughAnyIsError(t *testing.T) {
	err := interpretVMSource(t, New(), `
struct Node
    valor: int
    next: ref Node
end
func toca(n: ref Node) -> void
    return
end
func main()
    let seg: Node = Node(2, null)
    let a: any = Node(1, ref seg)
    toca(ref a.next)
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "slot 'next' already holds a reference") || !strings.Contains(err.Error(), "pass it directly") {
		t.Fatalf("err = %v, want 'slot 'next' already holds a reference ... pass it directly'", err)
	}
}

func TestRefOfRefIndexSlotThroughAnyIsError(t *testing.T) {
	err := interpretVMSource(t, New(), `
func toca(n: ref int) -> void
    return
end
func main()
    let x: int = 1
    let xs: (ref int)[] = [ref x]
    let a: any = xs
    toca(ref a[0])
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "already holds a reference") {
		t.Fatalf("err = %v, want 'already holds a reference'", err)
	}
}
