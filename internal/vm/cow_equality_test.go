package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func TestStructuralEqualityArrays(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let a: int[]
    append(a, 1)
    append(a, 2)
    let b: int[]
    append(b, 1)
    append(b, 2)
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
	a.Obj.(*value.ObjInstance).Fields["x"] = value.NewInt(1)
	b := value.NewInstance(bDef)
	b.Obj.(*value.ObjInstance).Fields["x"] = value.NewInt(1)
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
