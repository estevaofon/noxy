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
