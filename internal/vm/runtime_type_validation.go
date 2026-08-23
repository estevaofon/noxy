package vm

import (
	"fmt"
	"noxy-vm/internal/value"
)

func (vm *VM) appendItemCompatible(target *value.ObjRef, item value.Value) bool {
	if item.Type == value.VAL_REF {
		itemRef, ok := item.Obj.(*value.ObjRef)
		if !ok || itemRef == nil {
			return false
		}
	}
	if target == nil {
		return true
	}
	arrayType := target.TargetType.Load()
	if arrayType == nil {
		return true
	}
	if arrayType.Kind != value.TYPE_ARRAY || arrayType.Element == nil {
		return false
	}
	elementType := arrayType.Element
	if elementType.Kind != value.TYPE_REF {
		if item.Type == value.VAL_REF {
			return false
		}
		return vm.runtimeValueMatchesType(item, elementType)
	}
	if item.Type == value.VAL_NULL {
		return true
	}
	if item.Type != value.VAL_REF || elementType.Element == nil {
		return false
	}
	resolved, err := vm.resolveReferenceValue(item)
	if err != nil {
		return false
	}
	return vm.runtimeValueMatchesType(resolved, elementType.Element)
}

func markReferenceTargetType(ref *value.ObjRef, targetType *value.RuntimeTypeInfo) bool {
	if ref == nil || targetType == nil {
		return false
	}
	for {
		existing := ref.TargetType.Load()
		if existing != nil {
			return runtimeTypeAccepts(targetType, existing, make(map[runtimeTypePair]bool))
		}
		if ref.TargetType.CompareAndSwap(nil, targetType) {
			return true
		}
	}
}

func (vm *VM) validateStructConstructorArguments(definition *value.ObjStruct, args []value.Value) error {
	cached, valid := validatedStructConstructor(definition)
	if !valid {
		return fmt.Errorf("struct constructor has incomplete runtime type metadata")
	}
	schema := cached.Schema
	if len(schema.Params) != len(args) {
		return fmt.Errorf("struct '%s' constructor has invalid runtime type metadata", definition.Name)
	}
	if err := validateParameterModes(definition.Name, cached.Params, args); err != nil {
		return err
	}
	for i, expected := range schema.Params {
		if !vm.runtimeValueMatchesType(args[i], expected) {
			return fmt.Errorf("function '%s' argument %d: expected %s, got %s", definition.Name, i+1, expected.String(), runtimeValueMode(args[i]))
		}
	}
	return nil
}

func validStructConstructorType(definition *value.ObjStruct) (*value.RuntimeTypeInfo, bool) {
	cached, valid := validatedStructConstructor(definition)
	if !valid {
		return nil, false
	}
	return cached.Schema, true
}

// validatedStructConstructor e validStructConstructorType com cache no proprio
// ObjStruct (issue #40 item 1): ConstructorType e imutavel depois da
// compilacao, entao o walk de runtimeTypeComplete (que aloca um map por
// chamada), as checagens estruturais e os ParamInfo sao calculados UMA vez por
// struct. So veredito valido e guardado — o invalido e caminho de erro e
// recalcula; o leitor (CtorCache) confere que o schema cacheado ainda e o
// ConstructorType atual.
func validatedStructConstructor(definition *value.ObjStruct) (*value.ValidatedCtor, bool) {
	if definition == nil {
		return nil, false
	}
	if cached := definition.CtorCache(); cached != nil {
		return cached, true
	}
	schema := definition.ConstructorType
	if !runtimeTypeComplete(schema, make(map[*value.RuntimeTypeInfo]bool)) || schema.Kind != value.TYPE_CALLABLE || schema.CallableBare ||
		len(schema.Params) != len(definition.Fields) || len(schema.ParamIsRef) != len(schema.Params) || schema.Return == nil ||
		schema.Return.Kind != value.TYPE_STRUCT || schema.Return.Name != definition.Name {
		return nil, false
	}
	params := make([]value.ParamInfo, len(schema.Params))
	for i, expected := range schema.Params {
		params[i] = value.ParamInfo{IsRef: schema.ParamIsRef[i], TypeName: expected.String()}
	}
	cached := &value.ValidatedCtor{Schema: schema, Params: params}
	definition.StoreCtorCache(cached)
	return cached, true
}


// runtimeTagAccepted informa, em O(profundidade do tipo), se uma tag de
// runtime existente satisfaz o esquema esperado. Tag aceita vale como prova
// do conteudo: os elementos foram validados quando a tag foi gravada e as
// escritas tipadas validam na entrada.
func runtimeTagAccepted(expected, actual *value.RuntimeTypeInfo) bool {
	return actual != nil && runtimeTypeComplete(actual, make(map[*value.RuntimeTypeInfo]bool)) && runtimeTypeAccepts(expected, actual, make(map[runtimeTypePair]bool))
}

func (vm *VM) runtimeValueMatchesType(actual value.Value, expected *value.RuntimeTypeInfo) bool {
	if expected == nil {
		return false
	}
	if expected.Kind == value.TYPE_ANY {
		return true
	}
	if actual.Type == value.VAL_NULL {
		return expected.Kind == value.TYPE_NULL || expected.Kind == value.TYPE_REF || expected.Kind == value.TYPE_STRUCT
	}
	switch expected.Kind {
	case value.TYPE_NULL:
		return false
	case value.TYPE_BOOL:
		return actual.Type == value.VAL_BOOL
	case value.TYPE_INT:
		return actual.Type == value.VAL_INT
	case value.TYPE_FLOAT:
		return actual.Type == value.VAL_FLOAT
	case value.TYPE_STRING:
		if actual.Type != value.VAL_OBJ {
			return false
		}
		_, ok := actual.Obj.(string)
		return ok
	case value.TYPE_BYTES:
		return actual.Type == value.VAL_BYTES
	case value.TYPE_ARRAY:
		array, ok := actual.Obj.(*value.ObjArray)
		if actual.Type != value.VAL_OBJ || !ok {
			return false
		}
		actualType := array.RuntimeType.Load()
		if actualType != nil {
			return runtimeTagAccepted(expected, actualType)
		}
		if expected.Size > 0 || len(array.Elements) == 0 {
			return false
		}
		for _, element := range array.Elements {
			if !vm.runtimeValueMatchesType(element, expected.Element) {
				return false
			}
		}
		return true
	case value.TYPE_MAP:
		mapping, ok := actual.Obj.(*value.ObjMap)
		if actual.Type != value.VAL_OBJ || !ok {
			return false
		}
		actualType := mapping.RuntimeType.Load()
		if actualType != nil {
			return runtimeTagAccepted(expected, actualType)
		}
		if mapping.Len() == 0 {
			return false
		}
		for key, element := range mapping.Snapshot() {
			if !runtimeMapKeyMatchesType(key, expected.Key) || !vm.runtimeValueMatchesType(element, expected.Value) {
				return false
			}
		}
		return true
	case value.TYPE_REF:
		ref, ok := actual.Obj.(*value.ObjRef)
		if actual.Type != value.VAL_REF || !ok || ref == nil || expected.Element == nil {
			return false
		}
		resolved, err := vm.resolveReferenceValue(actual)
		return err == nil && vm.runtimeValueMatchesType(resolved, expected.Element)
	case value.TYPE_STRUCT:
		instance, ok := actual.Obj.(*value.ObjInstance)
		return actual.Type == value.VAL_OBJ && ok && instance.Struct != nil && instance.Struct.Name == expected.Name
	case value.TYPE_CALLABLE:
		return runtimeCallableMatchesType(actual, expected)
	case value.TYPE_CHANNEL:
		channel, ok := actual.Obj.(*value.ObjChannel)
		if actual.Type != value.VAL_CHANNEL || !ok || channel == nil || expected.Element == nil {
			return false
		}
		channel.Lock.Lock()
		elementType := channel.ElementType
		channel.Lock.Unlock()
		if elementType == nil {
			return expected.Element.Kind == value.TYPE_ANY
		}
		return runtimeTypeAccepts(expected.Element, elementType, make(map[runtimeTypePair]bool))
	default:
		return false
	}
}

func runtimeMapKeyMatchesType(key interface{}, expected *value.RuntimeTypeInfo) bool {
	if expected == nil {
		return false
	}
	if expected.Kind == value.TYPE_ANY {
		return true
	}
	switch expected.Kind {
	case value.TYPE_INT:
		_, ok := key.(int64)
		return ok
	case value.TYPE_STRING:
		_, ok := key.(string)
		return ok
	default:
		return false
	}
}

func runtimeCallableMatchesType(actual value.Value, expected *value.RuntimeTypeInfo) bool {
	if expected == nil || expected.Kind != value.TYPE_CALLABLE {
		return false
	}
	if expected.CallableBare {
		switch actual.Type {
		case value.VAL_OBJ:
			constructor, ok := actual.Obj.(*value.ObjStruct)
			_, valid := validStructConstructorType(constructor)
			return ok && valid
		case value.VAL_FUNCTION:
			switch callable := actual.Obj.(type) {
			case *value.ObjClosure:
				return callable != nil && callable.Function != nil
			case *value.ObjFunction:
				return callable != nil
			}
		case value.VAL_NATIVE:
			native, ok := actual.Obj.(*value.ObjNative)
			return ok && native.IsCallable()
		}
		return false
	}

	switch actual.Type {
	case value.VAL_OBJ:
		constructor, ok := actual.Obj.(*value.ObjStruct)
		constructorType, valid := validStructConstructorType(constructor)
		return ok && valid && runtimeTypeAccepts(expected, constructorType, make(map[runtimeTypePair]bool))
	case value.VAL_FUNCTION:
		var actualType *value.RuntimeTypeInfo
		switch callable := actual.Obj.(type) {
		case *value.ObjClosure:
			if callable != nil && callable.Function != nil {
				actualType = callable.Function.RuntimeType
			}
		case *value.ObjFunction:
			if callable != nil {
				actualType = callable.RuntimeType
			}
		}
		return actualType != nil && runtimeTypeAccepts(expected, actualType, make(map[runtimeTypePair]bool))
	case value.VAL_NATIVE:
		native, ok := actual.Obj.(*value.ObjNative)
		return ok && native.IsCallable() && nativeSignatureMatchesRuntimeType(native.Signature, expected)
	default:
		return false
	}
}

func nativeSignatureMatchesRuntimeType(signature *value.NativeSignature, expected *value.RuntimeTypeInfo) bool {
	if signature == nil || !runtimeTypeComplete(expected, make(map[*value.RuntimeTypeInfo]bool)) || signature.Variadic || signature.Arity != len(expected.Params) || len(signature.Params) != len(expected.Params) || len(expected.ParamIsRef) != len(expected.Params) || expected.Return == nil {
		return false
	}
	if signature.ReturnType == "" || signature.ReturnType != expected.Return.String() {
		return false
	}
	for i, param := range signature.Params {
		if param.TypeName == "" || param.IsRef != expected.ParamIsRef[i] || param.TypeName != expected.Params[i].String() {
			return false
		}
	}
	return true
}

type runtimeTypePair struct {
	expected *value.RuntimeTypeInfo
	actual   *value.RuntimeTypeInfo
}

type runtimeValueTypePair struct {
	object interface{}
	schema *value.RuntimeTypeInfo
}

// markRuntimeValueType records channel element metadata only after proving the
// complete value graph compatible. It never replaces a conflicting marker or
// changes the identity of the marked value.
func (vm *VM) markRuntimeValueType(actual value.Value, schema *value.RuntimeTypeInfo) bool {
	if !runtimeTypeComplete(schema, make(map[*value.RuntimeTypeInfo]bool)) {
		return false
	}
	if !vm.walkRuntimeValueType(actual, schema, false, make(map[runtimeValueTypePair]bool)) {
		return false
	}
	return vm.walkRuntimeValueType(actual, schema, true, make(map[runtimeValueTypePair]bool))
}

func (vm *VM) walkRuntimeValueType(actual value.Value, schema *value.RuntimeTypeInfo, apply bool, seen map[runtimeValueTypePair]bool) bool {
	if schema == nil {
		return false
	}
	if schema.Kind == value.TYPE_ANY {
		return true
	}
	if actual.Type == value.VAL_NULL {
		return schema.Kind == value.TYPE_NULL || schema.Kind == value.TYPE_REF || schema.Kind == value.TYPE_STRUCT
	}
	switch schema.Kind {
	case value.TYPE_ARRAY:
		array, ok := actual.Obj.(*value.ObjArray)
		if actual.Type != value.VAL_OBJ || !ok || array == nil || schema.Element == nil {
			return false
		}
		if runtimeValueTypeSeen(array, schema, seen) {
			return true
		}
		actualType := array.RuntimeType.Load()
		if actualType != nil {
			return runtimeTagAccepted(schema, actualType)
		}
		if apply && !array.RuntimeType.CompareAndSwap(nil, schema) {
			// Perdeu a corrida de marcação: o vencedor validou o conteúdo.
			return runtimeTagAccepted(schema, array.RuntimeType.Load())
		}
		for _, element := range array.Elements {
			if !vm.walkRuntimeValueType(element, schema.Element, apply, seen) {
				return false
			}
		}
		return true
	case value.TYPE_MAP:
		mapping, ok := actual.Obj.(*value.ObjMap)
		if actual.Type != value.VAL_OBJ || !ok || mapping == nil || schema.Key == nil || schema.Value == nil {
			return false
		}
		if runtimeValueTypeSeen(mapping, schema, seen) {
			return true
		}
		actualType := mapping.RuntimeType.Load()
		if actualType != nil {
			return runtimeTagAccepted(schema, actualType)
		}
		if apply && !mapping.RuntimeType.CompareAndSwap(nil, schema) {
			// Perdeu a corrida de marcação: o vencedor validou o conteúdo.
			return runtimeTagAccepted(schema, mapping.RuntimeType.Load())
		}
		for key, element := range mapping.Snapshot() {
			if !runtimeMapKeyMatchesType(key, schema.Key) || !vm.walkRuntimeValueType(element, schema.Value, apply, seen) {
				return false
			}
		}
		return true
	case value.TYPE_REF:
		ref, ok := actual.Obj.(*value.ObjRef)
		if actual.Type != value.VAL_REF || !ok || ref == nil || schema.Element == nil {
			return false
		}
		if runtimeValueTypeSeen(ref, schema, seen) {
			return true
		}
		resolved, err := vm.resolveReferenceValue(actual)
		if err != nil {
			return false
		}
		if resolved.Type == value.VAL_NULL {
			return true
		}
		return vm.walkRuntimeValueType(resolved, schema.Element, apply, seen)
	case value.TYPE_STRUCT:
		// Fronteira dinâmica de envelope-como-map (call_result / errors.nx,
		// design doc §Representation): um native sem assinatura devolve um
		// *value.ObjMap fisicamente — nunca um ObjInstance — e o contrato
		// tipado (`let r: CallResult = ...`) vale pelo campo, não pelo nome
		// nominal (errors.nx: "vale pelo contrato de campos"). Aceita aqui
		// se TODO campo de schema.Fields existe no map com tipo
		// recursivamente compatível; chaves extras no map são ignoradas.
		// Deliberadamente NÃO estampa RuntimeType no map (ObjMap.RuntimeType
		// seguiria intocado) — cada marcação revalida do zero, evitando a
		// pergunta de CAS "mesmo map validado contra struct diferente
		// depois". O caminho ObjInstance abaixo (nominal, por Struct.Name)
		// continua byte-a-byte o de sempre.
		if mapping, ok := actual.Obj.(*value.ObjMap); ok && actual.Type == value.VAL_OBJ && mapping != nil {
			if runtimeValueTypeSeen(mapping, schema, seen) {
				return true
			}
			snapshot := mapping.Snapshot()
			for name, fieldSchema := range schema.Fields {
				field, exists := snapshot[name]
				if !exists || !vm.walkRuntimeValueType(field, fieldSchema, apply, seen) {
					return false
				}
			}
			return true
		}
		instance, ok := actual.Obj.(*value.ObjInstance)
		if actual.Type != value.VAL_OBJ || !ok || instance == nil || instance.Struct == nil || instance.Struct.Name != schema.Name {
			return false
		}
		if runtimeValueTypeSeen(instance, schema, seen) {
			return true
		}
		for name, fieldSchema := range schema.Fields {
			field, exists := instance.Fields[name]
			if !exists || !vm.walkRuntimeValueType(field, fieldSchema, apply, seen) {
				return false
			}
		}
		return true
	case value.TYPE_CHANNEL:
		channel, ok := actual.Obj.(*value.ObjChannel)
		if actual.Type != value.VAL_CHANNEL || !ok || channel == nil || schema.Element == nil {
			return false
		}
		if runtimeValueTypeSeen(channel, schema, seen) {
			return true
		}
		channel.Lock.Lock()
		defer channel.Lock.Unlock()
		if channel.ElementType != nil && !runtimeTypeAccepts(schema.Element, channel.ElementType, make(map[runtimeTypePair]bool)) {
			return false
		}
		if apply && channel.ElementType == nil {
			channel.ElementType = schema.Element
		}
		return true
	case value.TYPE_CALLABLE:
		return runtimeCallableMatchesType(actual, schema)
	default:
		return vm.runtimeValueMatchesType(actual, schema)
	}
}

func runtimeValueTypeSeen(object interface{}, schema *value.RuntimeTypeInfo, seen map[runtimeValueTypePair]bool) bool {
	pair := runtimeValueTypePair{object: object, schema: schema}
	if seen[pair] {
		return true
	}
	seen[pair] = true
	return false
}

// runtimeTypeAccepts compares runtime schemas without following recursive
// struct definitions forever. TYPE_ANY is the sole schema wildcard.
func runtimeTypeAccepts(expected, actual *value.RuntimeTypeInfo, seen map[runtimeTypePair]bool) bool {
	if expected == nil || actual == nil {
		return false
	}
	if expected.Kind == value.TYPE_ANY {
		return true
	}
	if expected.Kind != actual.Kind {
		return false
	}
	pair := runtimeTypePair{expected: expected, actual: actual}
	if seen[pair] {
		return true
	}
	seen[pair] = true
	switch expected.Kind {
	case value.TYPE_ARRAY:
		return (expected.Size == 0 || expected.Size == actual.Size) && runtimeTypeAccepts(expected.Element, actual.Element, seen)
	case value.TYPE_REF, value.TYPE_CHANNEL:
		return runtimeTypeAccepts(expected.Element, actual.Element, seen)
	case value.TYPE_MAP:
		return runtimeTypeAccepts(expected.Key, actual.Key, seen) && runtimeTypeAccepts(expected.Value, actual.Value, seen)
	case value.TYPE_STRUCT:
		return expected.Name == actual.Name
	case value.TYPE_CALLABLE:
		if expected.CallableBare {
			return true
		}
		return runtimeTypesEqual(expected, actual, make(map[runtimeTypePair]bool))
	default:
		return true
	}
}

func runtimeTypesEqual(left, right *value.RuntimeTypeInfo, seen map[runtimeTypePair]bool) bool {
	if left == nil || right == nil || left.Kind != right.Kind {
		return false
	}
	pair := runtimeTypePair{expected: left, actual: right}
	if seen[pair] {
		return true
	}
	seen[pair] = true
	switch left.Kind {
	case value.TYPE_ARRAY:
		return left.Size == right.Size && runtimeTypesEqual(left.Element, right.Element, seen)
	case value.TYPE_MAP:
		return runtimeTypesEqual(left.Key, right.Key, seen) && runtimeTypesEqual(left.Value, right.Value, seen)
	case value.TYPE_REF, value.TYPE_CHANNEL:
		return runtimeTypesEqual(left.Element, right.Element, seen)
	case value.TYPE_STRUCT:
		return left.Name == right.Name
	case value.TYPE_CALLABLE:
		if left.CallableBare || right.CallableBare {
			return left.CallableBare == right.CallableBare
		}
		if len(left.Params) != len(right.Params) || len(left.ParamIsRef) != len(left.Params) || len(right.ParamIsRef) != len(right.Params) {
			return false
		}
		for i := range left.Params {
			if left.ParamIsRef[i] != right.ParamIsRef[i] || !runtimeTypesEqual(left.Params[i], right.Params[i], seen) {
				return false
			}
		}
		return runtimeTypesEqual(left.Return, right.Return, seen)
	default:
		return true
	}
}

func runtimeTypeComplete(schema *value.RuntimeTypeInfo, seen map[*value.RuntimeTypeInfo]bool) bool {
	if schema == nil {
		return false
	}
	if seen[schema] {
		return true
	}
	seen[schema] = true
	switch schema.Kind {
	case value.TYPE_ARRAY, value.TYPE_REF, value.TYPE_CHANNEL:
		return runtimeTypeComplete(schema.Element, seen)
	case value.TYPE_MAP:
		return runtimeTypeComplete(schema.Key, seen) && runtimeTypeComplete(schema.Value, seen)
	case value.TYPE_STRUCT:
		if schema.Name == "" {
			return false
		}
		for _, field := range schema.Fields {
			if !runtimeTypeComplete(field, seen) {
				return false
			}
		}
		return true
	case value.TYPE_CALLABLE:
		if schema.CallableBare {
			return true
		}
		if len(schema.ParamIsRef) != len(schema.Params) || !runtimeTypeComplete(schema.Return, seen) {
			return false
		}
		for _, param := range schema.Params {
			if !runtimeTypeComplete(param, seen) {
				return false
			}
		}
		return true
	case value.TYPE_ANY, value.TYPE_NULL, value.TYPE_BOOL, value.TYPE_INT, value.TYPE_FLOAT, value.TYPE_STRING, value.TYPE_BYTES, value.TYPE_VOID:
		return true
	default:
		return false
	}
}
