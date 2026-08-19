package vm

import (
	"strings"
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

// Fronteira dinâmica de envelope-como-map (call_result / errors.nx): o
// envelope físico é sempre um *value.ObjMap (design doc §Representation), mas
// CallResult tem um campo composto aninhado (failure: Failure, cujo próprio
// causes: Failure[] é array) — diferente de IntResult, que não tem campo
// composto e por isso nunca dispara marcação em runtime. Isto obriga o caso
// TYPE_STRUCT de walkRuntimeValueType a aceitar um map estruturalmente
// compatível (contrato de campos, sem checagem nominal), sem estampar
// RuntimeType no próprio map — cada marcação revalida do zero.

func failureStructSchema() *value.RuntimeTypeInfo {
	stringT := &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}
	schema := &value.RuntimeTypeInfo{Kind: value.TYPE_STRUCT, Name: "Failure"}
	schema.Fields = map[string]*value.RuntimeTypeInfo{
		"kind":    stringT,
		"message": stringT,
		"stack":   stringT,
		"causes":  {Kind: value.TYPE_ARRAY, Element: schema},
	}
	return schema
}

func callResultStructSchema() *value.RuntimeTypeInfo {
	return &value.RuntimeTypeInfo{
		Kind: value.TYPE_STRUCT,
		Name: "CallResult",
		Fields: map[string]*value.RuntimeTypeInfo{
			"ok":      {Kind: value.TYPE_BOOL},
			"value":   {Kind: value.TYPE_ANY},
			"failure": failureStructSchema(),
		},
	}
}

// (a) um map com todo campo do esquema presente e recursivamente compatível
// satisfaz um contrato de struct com campo composto aninhado.
func TestMarkerAcceptsStructurallyMatchingMapAtStructBoundary(t *testing.T) {
	machine := New()
	envelope := value.NewMapWithData(map[string]value.Value{
		"ok":      value.NewBool(true),
		"value":   value.NewInt(42),
		"failure": value.NewNull(),
	})
	if !machine.markRuntimeValueType(envelope, callResultStructSchema()) {
		t.Fatal("map com todo campo do esquema presente e compatível deveria satisfazer o contrato de struct")
	}
}

// (b) map faltando campo do esquema é rejeitado.
func TestMarkerRejectsMapMissingStructField(t *testing.T) {
	machine := New()
	envelope := value.NewMapWithData(map[string]value.Value{
		"ok":    value.NewBool(true),
		"value": value.NewInt(42),
		// "failure" ausente.
	})
	if machine.markRuntimeValueType(envelope, callResultStructSchema()) {
		t.Fatal("map faltando campo declarado do esquema deveria ser rejeitado")
	}
}

// (b) map com campo de tipo errado é rejeitado.
func TestMarkerRejectsMapWithWrongFieldType(t *testing.T) {
	machine := New()
	envelope := value.NewMapWithData(map[string]value.Value{
		"ok":      value.NewString("not a bool"),
		"value":   value.NewInt(42),
		"failure": value.NewNull(),
	})
	if machine.markRuntimeValueType(envelope, callResultStructSchema()) {
		t.Fatal("map com campo de tipo incompatível com o esquema deveria ser rejeitado")
	}
}

// (a)+(b) fim a fim, com o struct real `CallResult` do módulo `errors` (o
// mesmo cujo campo `failure: Failure` aninha `causes: Failure[]` e por isso
// dispara o marcador em runtime — diferente de IntResult). Um native de teste
// próprio produz o map dinamicamente, sem depender do native call_result: o
// mecanismo sob teste é genérico, não específico à feature call_result.
func TestLetStructAnnotationOverDynamicMapEnvelope(t *testing.T) {
	machine := New()
	machine.DefineNative("__test_dynamic_envelope_ok", func(args []value.Value) value.Value {
		return value.NewMapWithData(map[string]value.Value{
			"ok":      value.NewBool(true),
			"value":   value.NewInt(42),
			"failure": value.NewNull(),
		})
	})
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	source := `
use errors select *
let r: CallResult = __test_dynamic_envelope_ok()
test_report(to_str(r.ok) + "|" + to_str(r.value))
`
	if err := interpretVMSource(t, machine, source); err != nil {
		t.Fatalf("map matching CallResult's field contract should satisfy the struct-typed let: %v", err)
	}
	text, _ := captured.Obj.(string)
	if text != "true|42" {
		t.Fatalf("unexpected report: %q", text)
	}
}

// (b) fim a fim: um map faltando o campo `failure` do contrato de CallResult
// é rejeitado com o erro de marcador já existente — o mesmo caminho que hoje
// rejeita qualquer conflito estrutural.
func TestLetStructAnnotationRejectsStructurallyMismatchedMap(t *testing.T) {
	machine := New()
	machine.DefineNative("__test_dynamic_envelope_bad", func(args []value.Value) value.Value {
		return value.NewMapWithData(map[string]value.Value{
			"ok":    value.NewBool(true),
			"value": value.NewInt(42),
			// "failure" ausente de propósito.
		})
	})
	source := `
use errors select *
let r: CallResult = __test_dynamic_envelope_bad()
`
	err := interpretVMSource(t, machine, source)
	if err == nil || !strings.Contains(err.Error(), "runtime value metadata conflicts with static context") {
		t.Fatalf("want the existing marker-rejection error, got: %v", err)
	}
}

// (c) uma ObjInstance real de struct com NOME errado continua rejeitada — o
// caminho nominal para instâncias reais não muda.
func TestMarkerRejectsRealInstanceOfWrongStructName(t *testing.T) {
	machine := New()
	schema := &value.RuntimeTypeInfo{
		Kind:   value.TYPE_STRUCT,
		Name:   "CallResult",
		Fields: map[string]*value.RuntimeTypeInfo{},
	}
	otherDefinition := &value.ObjStruct{Name: "SomethingElse", Fields: []string{}}
	instance := value.Value{
		Type: value.VAL_OBJ,
		Obj:  &value.ObjInstance{Struct: otherDefinition, Fields: map[string]value.Value{}},
	}
	if machine.markRuntimeValueType(instance, schema) {
		t.Fatal("uma ObjInstance de struct com nome diferente do esquema deveria continuar rejeitada (caminho nominal intocado)")
	}
}
