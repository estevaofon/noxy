package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func TestMarkSharedAndUnicize(t *testing.T) {
	machine := New()
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	outer := value.NewArray([]value.Value{inner})

	if value.IsShared(outer) {
		t.Fatal("array novo não deve nascer Shared")
	}
	v, cloned := machine.unicize(outer)
	if cloned || v.Obj != outer.Obj {
		t.Fatal("unicize de objeto não-Shared deve devolver o mesmo objeto sem clonar")
	}

	value.MarkShared(outer)
	ResetCloneCount()
	v, cloned = machine.unicize(outer)
	if !cloned || v.Obj == outer.Obj {
		t.Fatal("unicize de objeto Shared deve clonar")
	}
	if value.IsShared(v) {
		t.Fatal("clone deve nascer com Shared desligado")
	}
	if !value.IsShared(inner) {
		t.Fatal("clone raso deve marcar os filhos compostos como Shared")
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperado 1 clone, contador = %d", CloneCountValue())
	}
	if v.Obj.(*value.ObjArray).Elements[0].Obj != inner.Obj {
		t.Fatal("clone raso deve compartilhar os filhos (mesmo ponteiro)")
	}
}

func TestMarkSharedIgnoresScalars(t *testing.T) {
	n := value.NewInt(7)
	value.MarkShared(n) // não deve entrar em pânico
	if value.IsShared(n) {
		t.Fatal("escalares nunca são Shared")
	}
}

func TestUnicizeMapMarksValues(t *testing.T) {
	machine := New()
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	m := value.NewMap()
	m.Obj.(*value.ObjMap).Set("k", inner)
	value.MarkShared(m)

	v, cloned := machine.unicize(m)
	if !cloned {
		t.Fatal("map Shared deve clonar")
	}
	if !value.IsShared(inner) {
		t.Fatal("valores do map clonado devem ficar Shared")
	}
	got, ok := v.Obj.(*value.ObjMap).Get("k")
	if !ok || got.Obj != inner.Obj {
		t.Fatal("clone raso de map deve compartilhar os valores (mesmo ponteiro)")
	}
}

func TestUnicizeInstanceMarksFields(t *testing.T) {
	machine := New()
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	structDef := &value.ObjStruct{Name: "Box", Fields: []string{"data"}}
	inst := value.NewInstance(structDef)
	inst.Obj.(*value.ObjInstance).Fields["data"] = inner
	value.MarkShared(inst)

	v, cloned := machine.unicize(inst)
	if !cloned {
		t.Fatal("instância Shared deve clonar")
	}
	if !value.IsShared(inner) {
		t.Fatal("campos compostos do clone devem ficar Shared")
	}
	if v.Obj.(*value.ObjInstance).Fields["data"].Obj != inner.Obj {
		t.Fatal("clone raso de instância deve compartilhar os campos (mesmo ponteiro)")
	}
}
