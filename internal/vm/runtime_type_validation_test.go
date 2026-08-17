package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// Estes testes fixam o contrato O(1) da validação por tag: quando um contêiner
// já carrega uma tag RuntimeType aceita pelo esquema esperado, a validação
// confia na tag e NÃO varre os elementos. O elemento "contrabandeado" (gravado
// direto via Go, fora dos caminhos validados da linguagem) é o detector: se a
// validação varresse, ela o rejeitaria.

func taggedIntMapWithSmuggledString() (value.Value, *value.RuntimeTypeInfo) {
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	text := &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}
	schema := &value.RuntimeTypeInfo{Kind: value.TYPE_MAP, Key: text, Value: integer}
	mapping := value.NewMap()
	mapObject := mapping.Obj.(*value.ObjMap)
	setTestMap(mapObject, "ok", value.NewInt(1))
	setTestMap(mapObject, "smuggled", value.NewString("boom"))
	mapObject.RuntimeType.Store(schema)
	return mapping, schema
}

func taggedIntArrayWithSmuggledString() (value.Value, *value.RuntimeTypeInfo) {
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	schema := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: integer}
	array := value.NewArray([]value.Value{value.NewInt(1), value.NewString("boom")})
	array.Obj.(*value.ObjArray).RuntimeType.Store(schema)
	return array, schema
}

func TestMarkerTrustsAcceptedMapTagWithoutElementWalk(t *testing.T) {
	machine := New()
	mapping, schema := taggedIntMapWithSmuggledString()
	if !machine.markRuntimeValueType(mapping, schema) {
		t.Fatal("marcador varreu elementos de map com tag aceita; esperado confiar na tag em O(1)")
	}
}

func TestMarkerTrustsAcceptedArrayTagWithoutElementWalk(t *testing.T) {
	machine := New()
	array, schema := taggedIntArrayWithSmuggledString()
	if !machine.markRuntimeValueType(array, schema) {
		t.Fatal("marcador varreu elementos de array com tag aceita; esperado confiar na tag em O(1)")
	}
}

func TestMatchesTrustsAcceptedMapTag(t *testing.T) {
	machine := New()
	mapping, schema := taggedIntMapWithSmuggledString()
	if !machine.runtimeValueMatchesType(mapping, schema) {
		t.Fatal("matcher varreu elementos de map com tag aceita; esperado confiar na tag em O(1)")
	}
}

func TestMatchesTrustsAcceptedArrayTag(t *testing.T) {
	machine := New()
	array, schema := taggedIntArrayWithSmuggledString()
	if !machine.runtimeValueMatchesType(array, schema) {
		t.Fatal("matcher varreu elementos de array com tag aceita; esperado confiar na tag em O(1)")
	}
}

func TestMarkerTrustsAcceptedTagThroughRef(t *testing.T) {
	machine := New()
	mapping, schema := taggedIntMapWithSmuggledString()
	ref := &value.ObjRef{RefType: value.REF_PTR, Ptr: &mapping}
	refValue := value.Value{Type: value.VAL_REF, Obj: ref}
	refSchema := &value.RuntimeTypeInfo{Kind: value.TYPE_REF, Element: schema}
	if !machine.markRuntimeValueType(refValue, refSchema) {
		t.Fatal("marcador varreu map com tag aceita atrás de ref; esperado O(1) também via ref")
	}
}

func TestMarkerTrustsAcceptedTagInsideStructField(t *testing.T) {
	machine := New()
	mapping, mapSchema := taggedIntMapWithSmuggledString()
	structSchema := &value.RuntimeTypeInfo{
		Kind:   value.TYPE_STRUCT,
		Name:   "State",
		Fields: map[string]*value.RuntimeTypeInfo{"payloads": mapSchema},
	}
	definition := &value.ObjStruct{Name: "State", Fields: []string{"payloads"}}
	instance := value.Value{
		Type: value.VAL_OBJ,
		Obj:  &value.ObjInstance{Struct: definition, Fields: map[string]value.Value{"payloads": mapping}},
	}
	if !machine.markRuntimeValueType(instance, structSchema) {
		t.Fatal("marcador varreu map com tag aceita dentro de campo de struct; esperado O(1)")
	}
}

// Guardas do caminho lento: sem tag, a validação completa continua obrigatória
// (é ela que garante a tag na primeira marcação); tag conflitante continua
// rejeitada (coberta também em TestRuntimeValueMarkerRejectsConflictsWithoutOverwriteOrRefMutation).

func TestMarkerStillWalksUntaggedMap(t *testing.T) {
	machine := New()
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	text := &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}
	schema := &value.RuntimeTypeInfo{Kind: value.TYPE_MAP, Key: text, Value: integer}
	mapping := value.NewMap()
	mapObject := mapping.Obj.(*value.ObjMap)
	setTestMap(mapObject, "smuggled", value.NewString("boom"))
	if machine.markRuntimeValueType(mapping, schema) {
		t.Fatal("map sem tag com elemento inválido foi aceito; primeira marcação deve varrer")
	}
	if mapObject.RuntimeType.Load() != nil {
		t.Fatal("tag foi gravada apesar da validação ter falhado")
	}
}

func TestMarkerStillWalksUntaggedArray(t *testing.T) {
	machine := New()
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	schema := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: integer}
	array := value.NewArray([]value.Value{value.NewString("boom")})
	if machine.markRuntimeValueType(array, schema) {
		t.Fatal("array sem tag com elemento inválido foi aceito; primeira marcação deve varrer")
	}
	if array.Obj.(*value.ObjArray).RuntimeType.Load() != nil {
		t.Fatal("tag foi gravada apesar da validação ter falhado")
	}
}
