package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// m[k] = v com Swap sob um lock (issue #66, item 4): o RC do valor velho
// continua igual — guardar um array no map da a ele um segundo dono
// (slot + entrada), substitui-lo libera exatamente esse dono, e sobrescrever
// uma chave NOVA nao libera nada. Contagem observada por OwnersCount, como os
// testes de RC fazem.
func TestMapSetReleasesOldValueOnce(t *testing.T) {
	machine := New()
	var owners []int32
	machine.DefineNative("probe", func(args []value.Value) value.Value {
		owners = append(owners, value.OwnersCount(args[0]))
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe")
	src := `
func main()
    let a: int[] = [1, 2, 3]
    let m: map[string, int[]] = {}
    probe(a)
    m["k"] = a
    probe(a)
    m["k"] = [9]
    probe(a)
    m["outra"] = a
    probe(a)
end
main()
`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	want := []int32{1, 2, 1, 2}
	if len(owners) != len(want) {
		t.Fatalf("owners = %v, want %v", owners, want)
	}
	for i := range want {
		if owners[i] != want[i] {
			t.Fatalf("owners = %v, want %v (set sobre chave existente tem de liberar o velho uma vez, sobre chave nova nada)", owners, want)
		}
	}
}

// length(m), has_key e leitura/escrita com chave string e int: o mesmo
// resultado com a chave reaproveitada (sem re-boxing) e com Len sem Snapshot.
func TestMapLengthHasKeyAndKeys(t *testing.T) {
	src := `
let m: map[string, int] = {"a": 1}
m["b"] = 2
m["a"] = 3
let before: int = length(m)
delete(m, "a")
let after: int = length(m)
let n: map[int, string] = {}
n[7] = "seven"
n[7] = "SEVEN"
let hk: bool = has_key(m, "b")
let hk2: bool = has_key(m, "a")
test_report([before, after, length(n), m["b"]])
test_report([before, after, length(n), m["b"], to_str(hk) == "true", to_str(hk2) == "false", n[7] == "SEVEN"])
`
	got := semArray(t, captureVMSource(t, src))
	wantInts := []int64{2, 1, 1, 2}
	for i, w := range wantInts {
		if got[i].Type != value.VAL_INT || got[i].Int() != w {
			t.Fatalf("celula %d: got %s, want %d", i, got[i].String(), w)
		}
	}
	for i := 4; i < 7; i++ {
		if got[i].Type != value.VAL_BOOL || !got[i].Bool() {
			t.Fatalf("celula %d: got %s, want true", i, got[i].String())
		}
	}
}
