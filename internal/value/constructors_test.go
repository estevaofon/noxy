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
	if object.Fields["data"].Obj != data.Obj || !object.Fields["ok"].AsBool {
		t.Fatal("NewInstanceWith deve guardar os campos recebidos")
	}
	if got := OwnersCount(instance); got != 0 {
		t.Fatalf("a instancia nova nasce sem dono: Owners=%d", got)
	}
}

func TestNewInstanceWithNilFieldsIsWritable(t *testing.T) {
	instance := NewInstanceWith(structForTest("x"), nil)
	instance.Obj.(*ObjInstance).Fields["x"] = NewInt(1) // nao pode entrar em panico (map nil)
}
