package vm

import (
	"testing"
)

// JSON_SUPPORT.md, "Reference slots (ref T)": um slot `ref T` dentro do alvo
// (elemento `(ref T)[]`, campo `ref T`) recebe a escrita ATRAVÉS da referência
// quando já aponta para algo; `null` no payload zera o slot; e um slot `null`
// com payload não-nulo ganha uma célula nova que é dona do `T` construído a
// partir do schema do referente. Exemplo literal da documentação mais os dois
// outros ramos da tabela.
func TestJSONLoadsIntoRefSlotsFollowsTheDocumentedTable(t *testing.T) {
	got := captureVMSource(t, `
struct Inner
    n: int
end
struct Holder
    child: ref Inner
end
// slot null + payload não-nulo: célula nova, dona do valor construído
let target: (ref int)[] = [null]
let ok1: bool = json_loads("[42]", target)
let viz: ref int = target[0]
let kind: string = type(ref viz)
let lido: int = *viz
// slot que já aponta: escrita através da referência
let x: int = 1
let via: (ref int)[] = [ref x]
let ok2: bool = json_loads("[5]", via)
// payload null: slot vira null
let ok3: bool = json_loads("[null]", via)
let zerado: bool = via[0] == null
// campo ref de struct: null + objeto constrói o Inner numa célula nova
let h: Holder = Holder(null)
let ok4: bool = json_loads("{\"child\": {\"n\": 7}}", ref h)
let filho: ref Inner = h.child
test_report([to_str(ok1), kind, to_str(lido), to_str(ok2), to_str(x), to_str(ok3), to_str(zerado), to_str(ok4), to_str(filho.n)])
`)
	want := []string{"true", "ref", "42", "true", "5", "true", "true", "true", "7"}
	cells := semArray(t, got)
	if len(cells) != len(want) {
		t.Fatalf("células=%d, want %d: %s", len(cells), len(want), got.String())
	}
	for i, cell := range cells {
		if s, ok := cell.Obj.(string); !ok || s != want[i] {
			t.Fatalf("célula %d: got %s, want %q (tudo: %s)", i, cell.String(), want[i], got.String())
		}
	}
}
