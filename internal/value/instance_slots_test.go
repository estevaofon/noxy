package value

import (
	"strings"
	"testing"
)

// Issue #86: ObjInstance guarda os campos em slots indexados pela declaracao,
// nao num map por instancia. Estes testes fixam a API que os opcodes e os
// natives usam no lugar do map.

func TestNewStructBuildsFieldIndex(t *testing.T) {
	def := NewStruct("P", []string{"x", "y"}).Obj.(*ObjStruct)
	if i, ok := def.FieldIndex("y"); !ok || i != 1 {
		t.Fatalf("FieldIndex(y) = (%d, %v), esperado (1, true)", i, ok)
	}
	if _, ok := def.FieldIndex("z"); ok {
		t.Fatal("campo nao declarado nao pode ter indice")
	}
	if !def.HasField("x") || def.HasField("z") {
		t.Fatal("HasField deve seguir a declaracao")
	}
}

func TestFieldIndexFallsBackToLinearScanWithoutIndex(t *testing.T) {
	// Literal montado a mao (testes) nao passa por NewStruct: sem indice,
	// a varredura linear tem de dar a mesma resposta.
	def := &ObjStruct{Name: "P", Fields: []string{"a", "b", "c"}}
	if i, ok := def.FieldIndex("c"); !ok || i != 2 {
		t.Fatalf("varredura linear: FieldIndex(c) = (%d, %v), esperado (2, true)", i, ok)
	}
	def.BuildFieldIndex()
	if i, ok := def.FieldIndex("c"); !ok || i != 2 {
		t.Fatalf("com indice: FieldIndex(c) = (%d, %v), esperado (2, true)", i, ok)
	}
	var nilDef *ObjStruct
	if _, ok := nilDef.FieldIndex("a"); ok {
		t.Fatal("FieldIndex em *ObjStruct nil deve ser (0, false)")
	}
}

func TestNewInstanceSlotsStartNull(t *testing.T) {
	def := NewStruct("P", []string{"x", "y"}).Obj.(*ObjStruct)
	inst := NewInstance(def).Obj.(*ObjInstance)
	if inst.Len() != 2 {
		t.Fatalf("Len = %d, esperado 2 (um slot por campo declarado)", inst.Len())
	}
	for _, name := range []string{"x", "y"} {
		got, ok := inst.Get(name)
		if !ok || got.Type != VAL_NULL {
			t.Fatalf("slot %q nao preenchido deve ler null: (%s, %v)", name, got.String(), ok)
		}
	}
}

func TestInstanceGetSetFollowDeclaration(t *testing.T) {
	def := NewStruct("P", []string{"x", "y"}).Obj.(*ObjStruct)
	inst := NewInstance(def).Obj.(*ObjInstance)
	if !inst.Set("y", NewInt(7)) {
		t.Fatal("Set em campo declarado deve devolver true")
	}
	if got, ok := inst.Get("y"); !ok || got.Int() != 7 {
		t.Fatalf("Get(y) = (%s, %v), esperado 7", got.String(), ok)
	}
	if inst.Field("y").Int() != 7 {
		t.Fatal("Field deve ler o mesmo slot")
	}
	if inst.Set("z", NewInt(1)) {
		t.Fatal("Set em nome fora da declaracao deve devolver false (struct e de campos fixos, spec §5)")
	}
	if _, ok := inst.Get("z"); ok {
		t.Fatal("Get em nome fora da declaracao deve devolver ok=false")
	}
	if inst.Field("z").Type != VAL_NULL {
		t.Fatal("Field em nome fora da declaracao deve ler null")
	}
}

func TestMustSetPanicsOnUndeclaredField(t *testing.T) {
	def := NewStruct("P", []string{"x"}).Obj.(*ObjStruct)
	inst := NewInstance(def).Obj.(*ObjInstance)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustSet em campo nao declarado deve entrar em panico (bug do native, nao erro do programa)")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "P") || !strings.Contains(msg, "zz") {
			t.Fatalf("panic deve nomear struct e campo: %v", r)
		}
	}()
	inst.MustSet("zz", NewInt(1))
}

func TestInstanceRangeIsDeclarationOrder(t *testing.T) {
	def := NewStruct("P", []string{"c", "a", "b"}).Obj.(*ObjStruct)
	inst := NewInstanceWith(def, map[string]Value{"a": NewInt(1), "b": NewInt(2), "c": NewInt(3)}).Obj.(*ObjInstance)
	var order []string
	inst.Range(func(name string, v Value) { order = append(order, name) })
	if strings.Join(order, ",") != "c,a,b" {
		t.Fatalf("Range deve seguir a ordem de declaracao, veio %v", order)
	}
	snap := inst.Snapshot()
	if len(snap) != 3 || snap["c"].Int() != 3 {
		t.Fatalf("Snapshot inconsistente: %v", snap)
	}
}

func TestNewInstanceWithRetainsAndRejectsUndeclared(t *testing.T) {
	def := NewStruct("P", []string{"items"}).Obj.(*ObjStruct)
	child := NewArray(nil)
	inst := NewInstanceWith(def, map[string]Value{"items": child})
	if got := OwnersCount(child); got != 1 {
		t.Fatalf("NewInstanceWith deve reter o composto: Owners=%d", got)
	}
	if inst.Obj.(*ObjInstance).Field("items").Obj != child.Obj {
		t.Fatal("campo deve apontar para o mesmo array")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("NewInstanceWith com nome fora da declaracao deve entrar em panico")
		}
	}()
	NewInstanceWith(def, map[string]Value{"other": NewInt(1)})
}
