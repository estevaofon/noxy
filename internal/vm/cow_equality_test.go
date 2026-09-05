package vm

import (
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func TestStructuralEqualityArrays(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let a: int[]
    append(ref a, 1)
    append(ref a, 2)
    let b: int[]
    append(ref b, 1)
    append(ref b, 2)
    if a == b then
        test_report(1)
    else
        test_report(0)
    end
end
main()
`)
	expectInt(t, got, 1, "[1,2] == [1,2] deve ser true (estrutural)")
}

func TestStructuralEqualityStructs(t *testing.T) {
	got := captureVMSource(t, `
struct P
    x: int
end

func main()
    let a: P = P(1)
    let b: P = P(1)
    let c: P = P(2)
    let r: int = 0
    if a == b then
        r = r + 1
    end
    if a == c then
        r = r + 10
    end
    test_report(r)
end
main()
`)
	expectInt(t, got, 1, "instâncias iguais por campo comparam true; diferentes, false")
}

func TestStructuralEqualityNestedAndNegative(t *testing.T) {
	a := value.NewArray([]value.Value{value.NewArray([]value.Value{value.NewInt(1)})})
	b := value.NewArray([]value.Value{value.NewArray([]value.Value{value.NewInt(1)})})
	c := value.NewArray([]value.Value{value.NewArray([]value.Value{value.NewInt(2)})})
	if !valuesEqual(a, b) {
		t.Fatal("estrutural profundo deve ser igual")
	}
	if valuesEqual(a, c) {
		t.Fatal("conteúdo diferente deve ser diferente")
	}
	if valuesEqual(a, value.NewInt(1)) {
		t.Fatal("tipos mistos comparam false")
	}
}

func TestStructuralEqualityMaps(t *testing.T) {
	mk := func(v int64) value.Value {
		m := value.NewMap()
		m.Obj.(*value.ObjMap).Set("k", value.NewInt(v))
		return m
	}
	if !valuesEqual(mk(1), mk(1)) {
		t.Fatal("maps com mesmas chaves/valores devem ser iguais")
	}
	if valuesEqual(mk(1), mk(2)) {
		t.Fatal("maps com valores diferentes devem ser diferentes")
	}
	extra := value.NewMap()
	extra.Obj.(*value.ObjMap).Set("k", value.NewInt(1))
	extra.Obj.(*value.ObjMap).Set("j", value.NewInt(2))
	if valuesEqual(mk(1), extra) {
		t.Fatal("maps com conjuntos de chaves diferentes devem ser diferentes")
	}
}

func TestStructuralEqualityDifferentStructsNotEqual(t *testing.T) {
	aDef := &value.ObjStruct{Name: "A", Fields: []string{"x"}}
	bDef := &value.ObjStruct{Name: "B", Fields: []string{"x"}}
	a := value.NewInstance(aDef)
	a.Obj.(*value.ObjInstance).MustSet("x", value.NewInt(1))
	b := value.NewInstance(bDef)
	b.Obj.(*value.ObjInstance).MustSet("x", value.NewInt(1))
	if valuesEqual(a, b) {
		t.Fatal("structs de definições diferentes nunca são iguais")
	}
}

func TestRefEqualityBySlotIdentity(t *testing.T) {
	machine := New()
	env := machine.shared.Root
	env.SetLocal("g", value.NewInt(1))
	mkRef := func(name string) value.Value {
		return value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{
			RefType:     value.REF_GLOBAL,
			Name:        name,
			GlobalOwner: env,
		}}
	}
	if !valuesEqual(mkRef("g"), mkRef("g")) {
		t.Fatal("refs para o mesmo slot global devem ser iguais")
	}
	if valuesEqual(mkRef("g"), mkRef("h")) {
		t.Fatal("refs para slots diferentes devem ser diferentes")
	}
}

// TestRefEqualityIsSlotIdentityEndToEnd fecha o buraco que
// TestRefEqualityBySlotIdentity (acima) nao pegava: aquele chama valuesEqual
// direto, mas o compilador emitia OP_DEREF nos DOIS operandos de '=='/'!=',
// entao a comparacao de identidade nunca era alcancada a partir de codigo
// Noxy — dois refs para slots distintos de mesmo valor davam `true`.
func TestRefEqualityIsSlotIdentityEndToEnd(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"mesmo slot", "ra == ra2", true},
		{"slots distintos, mesmo valor", "ra == rb", false},
		{"slots distintos com !=", "ra != rb", true},
		{"identidade ignora o valor apontado", "ra == rb", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captureVMSource(t, `
func main()
    let a: int = 1
    let b: int = 1
    let ra: ref int = ref a
    let rb: ref int = ref b
    let ra2: ref int = ref a
    test_report(`+tc.expr+`)
end
main()
`)
			if got.Type != value.VAL_BOOL || got.Bool() != tc.want {
				t.Fatalf("%s: %s = %v, esperado %v", tc.name, tc.expr, got, tc.want)
			}
		})
	}
}

// TestRefEqualityNeverImplicitlyDereferences guarda o outro lado da regra
// (v0.7.1): em `==`/`!=` um ref NUNCA e dereferenciado implicitamente. O
// caso misto estatico ref vs valor e erro de compilacao (coberto em
// internal/compiler/ref_equality_strict_test.go); a comparacao de valor se
// escreve com deref explicito, e ref vs null pergunta sobre o PROPRIO ref
// — o que mantem `no.proximo != null` funcionando por nulidade do ref, nao
// por leitura do valor.
func TestRefEqualityNeverImplicitlyDereferences(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"deref explicito contra valor igual", "*ra == 1", true},
		{"deref explicito contra valor diferente", "*ra == 2", false},
		{"deref explicito a direita", "1 == *ra", true},
		{"ref valido nao é null", "ra == null", false},
		{"ref nulo é null", "nulo == null", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captureVMSource(t, `
func main()
    let a: int = 1
    let ra: ref int = ref a
    let nulo: ref int? = null
    test_report(`+tc.expr+`)
end
main()
`)
			if got.Type != value.VAL_BOOL || got.Bool() != tc.want {
				t.Fatalf("%s: %s = %v, esperado %v", tc.name, tc.expr, got, tc.want)
			}
		})
	}
}
