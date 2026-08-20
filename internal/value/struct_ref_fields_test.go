package value

import "testing"

func TestStructFieldIsRef(t *testing.T) {
	definition := NewStruct("Node", []string{"valor", "proximo"}).Obj.(*ObjStruct)
	if definition.FieldIsRef("proximo") || definition.FieldIsRef("valor") {
		t.Fatal("sem RefFields nenhum campo e ref")
	}
	definition.RefFields = map[string]bool{"proximo": true}
	if !definition.FieldIsRef("proximo") || definition.FieldIsRef("valor") || definition.FieldIsRef("inexistente") {
		t.Fatal("FieldIsRef deve refletir RefFields")
	}
	var nilDefinition *ObjStruct
	if nilDefinition.FieldIsRef("x") {
		t.Fatal("FieldIsRef em nil deve ser false")
	}
}
