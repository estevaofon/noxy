package value

import "testing"

// Os construtores de contêiner são a única fronteira pela qual natives e
// plugins criam arrays/maps/instâncias; a regra do runtime — "todo contêiner
// é dono durável de cada filho composto" — é aplicada aqui para valer para
// todos os ~30 call sites sem allowlist (Retain em escalar/string é no-op).

func structForTest(fields ...string) *ObjStruct {
	return NewStruct("Envelope", fields).Obj.(*ObjStruct)
}

func TestNewArrayAdoptingKeepsOwnersAsReceived(t *testing.T) {
	child := NewArray(nil)
	Retain(child) // o chamador já reteve em nome do array (move)
	adopted := NewArrayAdopting([]Value{child, NewInt(7)})
	if got := OwnersCount(child); got != 1 {
		t.Fatalf("NewArrayAdopting nao pode reter de novo: Owners=%d, esperado 1", got)
	}
	if got := OwnersCount(adopted); got != 0 {
		t.Fatalf("o array novo nasce sem dono: Owners=%d", got)
	}
	if adopted.Obj.(*ObjArray).Elements[0].Obj != child.Obj {
		t.Fatal("NewArrayAdopting deve guardar o mesmo objeto filho")
	}
}

func TestNewInstanceWithRetainsCompositeFields(t *testing.T) {
	definition := structForTest("data", "ok", "meta")
	data := NewArray(nil)
	meta := NewMap()
	instance := NewInstanceWith(definition, map[string]Value{
		"data": data,
		"ok":   NewBool(true),
		"meta": meta,
	})
	if got := OwnersCount(data); got != 1 {
		t.Fatalf("campo array deve ter a instancia como dono: Owners=%d", got)
	}
	if got := OwnersCount(meta); got != 1 {
		t.Fatalf("campo map deve ter a instancia como dono: Owners=%d", got)
	}
	object := instance.Obj.(*ObjInstance)
	if object.Struct != definition {
		t.Fatal("NewInstanceWith deve apontar para a definicao recebida")
	}
	if object.Field("data").Obj != data.Obj || !object.Field("ok").Bool() {
		t.Fatal("NewInstanceWith deve guardar os campos recebidos")
	}
	if got := OwnersCount(instance); got != 0 {
		t.Fatalf("a instancia nova nasce sem dono: Owners=%d", got)
	}
}

func TestNewInstanceWithNilFieldsIsWritable(t *testing.T) {
	instance := NewInstanceWith(structForTest("x"), nil)
	instance.Obj.(*ObjInstance).MustSet("x", NewInt(1)) // nao pode entrar em panico (slots alocados)
}

func TestNewArrayRetainsCompositeElementsOnly(t *testing.T) {
	child := NewArray(nil)
	grand := NewMap()
	array := NewArray([]Value{child, NewInt(1), NewString("s"), grand})
	if got := OwnersCount(child); got != 1 {
		t.Fatalf("array deve ser dono duravel do elemento composto: Owners=%d, esperado 1", got)
	}
	if got := OwnersCount(grand); got != 1 {
		t.Fatalf("map filho deve ter o array como dono: Owners=%d", got)
	}
	if got := OwnersCount(array); got != 0 {
		t.Fatalf("o proprio array nasce sem dono: Owners=%d", got)
	}
	elements := array.Obj.(*ObjArray).Elements
	if OwnersCount(elements[1]) != -1 || OwnersCount(elements[2]) != -1 {
		t.Fatal("escalares e strings nao tem contador (Retain e no-op)")
	}
	if OwnersCount(NewArray(nil)) != 0 || len(NewArray(nil).Obj.(*ObjArray).Elements) != 0 {
		t.Fatal("NewArray(nil) continua valendo como array vazio")
	}
}

func TestNewMapWithDataRetainsCompositeValues(t *testing.T) {
	child := NewArray(nil)
	mapping := NewMapWithData(map[string]Value{"rows": child, "count": NewInt(2)})
	if got := OwnersCount(child); got != 1 {
		t.Fatalf("map deve ser dono duravel do valor composto: Owners=%d, esperado 1", got)
	}
	stored, ok := mapping.Obj.(*ObjMap).Get("rows")
	if !ok || stored.Obj != child.Obj {
		t.Fatal("NewMapWithData deve guardar o mesmo objeto filho")
	}
	if got := OwnersCount(mapping); got != 0 {
		t.Fatalf("o proprio map nasce sem dono: Owners=%d", got)
	}
}
