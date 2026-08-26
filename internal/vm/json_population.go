package vm

import (
	"encoding/json"
	"math"
	"math/big"
	"noxy-vm/internal/value"
	"sort"
)

type jsonCommit func()
type jsonSetter func(value.Value)

func prepareJSONMutation(vm *VM, current value.Value, schema *value.RuntimeTypeInfo, data interface{}, set jsonSetter) (jsonCommit, bool) {
	if schema != nil && schema.Kind == value.TYPE_ANY {
		if set == nil {
			return nil, false
		}
		replacement, ok := dynamicJSONValue(data)
		if !ok {
			return nil, false
		}
		return func() { set(replacement) }, true
	}

	if schema != nil && schema.Kind == value.TYPE_REF {
		if data == nil {
			if set == nil {
				return nil, false
			}
			return func() { set(value.NewNull()) }, true
		}
		if current.Type == value.VAL_REF {
			ref, ok := current.Obj.(*value.ObjRef)
			if !ok || ref == nil {
				return nil, false
			}
			stored, store, ok := jsonReferenceStorage(vm, ref)
			if !ok {
				return nil, false
			}
			return prepareJSONMutation(vm, stored, schema.Element, data, store)
		}
		// Slot `ref T` nulo com payload nao-nulo: celula heap nova + ref
		// (spec 2026-08-20-ref-slot-invariant §5). Valor cru no slot e estado
		// impossivel depois da #50 — recusa em vez de sobrescrever.
		if current.Type != value.VAL_NULL || set == nil {
			return nil, false
		}
		cell, ok := buildReferentCell(schema.Element, data)
		if !ok {
			return nil, false
		}
		return func() { set(cell) }, true
	}
	if schema != nil && schema.Kind == value.TYPE_STRUCT && data == nil {
		if set == nil {
			return nil, false
		}
		return func() { set(value.NewNull()) }, true
	}

	if current.Type == value.VAL_NULL {
		if schema == nil || set == nil {
			return nil, false
		}
		replacement, ok := buildTypedJSONValue(schema, data)
		if !ok {
			return nil, false
		}
		return func() { set(replacement) }, true
	}

	if schema != nil {
		switch schema.Kind {
		case value.TYPE_ARRAY:
			array, ok := current.Obj.(*value.ObjArray)
			if current.Type != value.VAL_OBJ || !ok {
				return nil, false
			}
			return prepareJSONArrayMutation(vm, array, schema.Element, data)
		case value.TYPE_MAP:
			mapping, ok := current.Obj.(*value.ObjMap)
			if current.Type != value.VAL_OBJ || !ok || !jsonMapKeyCompatible(schema.Key) {
				return nil, false
			}
			return prepareJSONMapMutation(vm, mapping, schema.Value, data)
		case value.TYPE_STRUCT:
			instance, ok := current.Obj.(*value.ObjInstance)
			if current.Type != value.VAL_OBJ || !ok || instance.Struct == nil || instance.Struct.Name != schema.Name {
				return nil, false
			}
			return prepareJSONStructMutation(vm, instance, schema, data)
		default:
			if set == nil {
				return nil, false
			}
			replacement, ok := buildTypedJSONValue(schema, data)
			if !ok {
				return nil, false
			}
			return func() { set(replacement) }, true
		}
	}

	if current.Type == value.VAL_OBJ {
		switch object := current.Obj.(type) {
		case *value.ObjArray:
			return prepareJSONArrayMutation(vm, object, nil, data)
		case *value.ObjMap:
			return prepareJSONMapMutation(vm, object, nil, data)
		case *value.ObjInstance:
			return prepareJSONStructMutation(vm, object, nil, data)
		}
	}
	if set == nil {
		return nil, false
	}
	replacement, ok := dynamicJSONValue(data)
	if !ok {
		return nil, false
	}
	if !jsonReplacementCompatible(current, replacement) {
		return nil, false
	}
	return func() { set(replacement) }, true
}

func prepareJSONArrayMutation(vm *VM, array *value.ObjArray, elementSchema *value.RuntimeTypeInfo, data interface{}) (jsonCommit, bool) {
	dataArray, ok := data.([]interface{})
	if !ok {
		return nil, false
	}
	oldElements := array.Elements
	newElements := make([]value.Value, len(dataArray))
	commits := make([]jsonCommit, 0, len(dataArray))
	added := make([]value.Value, 0)
	for i, item := range dataArray {
		index := i
		if i < len(oldElements) {
			previous := oldElements[i]
			newElements[i] = previous
			commit, ok := prepareJSONMutation(vm, previous, elementSchema, item, func(updated value.Value) {
				// RC: a posicao troca de ocupante — retain-novo antes de
				// release-velho (so roda quando o filho foi SUBSTITUIDO; a
				// mutacao in-place de um filho composto nao passa por aqui).
				value.Retain(updated)
				newElements[index] = updated
				value.Release(previous)
			})
			if !ok {
				return nil, false
			}
			commits = append(commits, commit)
			continue
		}
		var created value.Value
		if elementSchema == nil {
			created, ok = dynamicJSONValue(item)
		} else {
			created, ok = buildTypedJSONValue(elementSchema, item)
		}
		if !ok {
			return nil, false
		}
		newElements[i] = created
		added = append(added, created)
	}
	return func() {
		for _, commit := range commits {
			commit()
		}
		// RC: posicoes novas ganham o array como dono; posicoes descartadas
		// (payload menor que o array) perdem.
		for _, created := range added {
			value.Retain(created)
		}
		for j := len(dataArray); j < len(oldElements); j++ {
			value.Release(oldElements[j])
		}
		array.Elements = newElements
	}, true
}

func prepareJSONMapMutation(vm *VM, mapping *value.ObjMap, valueSchema *value.RuntimeTypeInfo, data interface{}) (jsonCommit, bool) {
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, false
	}
	newData := mapping.Snapshot()
	commits := make([]jsonCommit, 0, len(dataMap))
	added := make([]value.Value, 0)
	for key, item := range dataMap {
		mapKey := key
		if current, exists := newData[mapKey]; exists {
			previous := current
			commit, ok := prepareJSONMutation(vm, current, valueSchema, item, func(updated value.Value) {
				// RC: troca de ocupante — retain-novo antes de release-velho
				value.Retain(updated)
				newData[mapKey] = updated
				value.Release(previous)
			})
			if !ok {
				return nil, false
			}
			commits = append(commits, commit)
			continue
		}
		var created value.Value
		if valueSchema == nil {
			created, ok = dynamicJSONValue(item)
		} else {
			created, ok = buildTypedJSONValue(valueSchema, item)
		}
		if !ok {
			return nil, false
		}
		newData[mapKey] = created
		added = append(added, created)
	}
	return func() {
		for _, commit := range commits {
			commit()
		}
		for _, created := range added {
			value.Retain(created) // RC: chave nova — o map vira dono
		}
		mapping.Replace(newData)
	}, true
}

func prepareJSONStructMutation(vm *VM, instance *value.ObjInstance, schema *value.RuntimeTypeInfo, data interface{}) (jsonCommit, bool) {
	dataMap, ok := data.(map[string]interface{})
	if !ok || instance.Struct == nil {
		return nil, false
	}
	newFields := make(map[string]value.Value, len(instance.Fields))
	for name, field := range instance.Fields {
		newFields[name] = field
	}
	commits := make([]jsonCommit, 0, len(instance.Struct.Fields))
	for _, fieldName := range instance.Struct.Fields {
		dataValue, exists := dataMap[fieldName]
		if !exists {
			continue
		}
		current, exists := instance.Fields[fieldName]
		if !exists {
			return nil, false
		}
		var fieldSchema *value.RuntimeTypeInfo
		if schema != nil {
			fieldSchema = schema.Fields[fieldName]
		} else if instance.Struct.JSONDynamicFields[fieldName] {
			fieldSchema = &value.RuntimeTypeInfo{Kind: value.TYPE_ANY}
		}
		name := fieldName
		previous := current
		commit, ok := prepareJSONMutation(vm, current, fieldSchema, dataValue, func(updated value.Value) {
			// RC: troca de ocupante do campo — retain-novo antes de release-velho
			value.Retain(updated)
			newFields[name] = updated
			value.Release(previous)
		})
		if !ok {
			return nil, false
		}
		commits = append(commits, commit)
	}
	return func() {
		for _, commit := range commits {
			commit()
		}
		instance.Fields = newFields
	}, true
}

func buildTypedJSONValue(schema *value.RuntimeTypeInfo, data interface{}) (value.Value, bool) {
	if schema == nil {
		return value.Value{}, false
	}
	switch schema.Kind {
	case value.TYPE_ANY:
		return dynamicJSONValue(data)
	case value.TYPE_NULL:
		if data == nil {
			return value.NewNull(), true
		}
	case value.TYPE_BOOL:
		if actual, ok := data.(bool); ok {
			return value.NewBool(actual), true
		}
	case value.TYPE_INT:
		if actual, ok := exactJSONInt(data); ok {
			return value.NewInt(actual), true
		}
	case value.TYPE_FLOAT:
		switch actual := data.(type) {
		case float64:
			if !math.IsInf(actual, 0) && !math.IsNaN(actual) {
				return value.NewFloat(actual), true
			}
		case json.Number:
			if parsed, err := actual.Float64(); err == nil && !math.IsInf(parsed, 0) && !math.IsNaN(parsed) {
				return value.NewFloat(parsed), true
			}
		}
	case value.TYPE_STRING:
		if actual, ok := data.(string); ok {
			return value.NewString(actual), true
		}
	case value.TYPE_BYTES:
		if actual, ok := data.(string); ok {
			return value.NewBytes(actual), true
		}
	case value.TYPE_REF:
		if data == nil {
			return value.NewNull(), true
		}
		return buildReferentCell(schema.Element, data)
	case value.TYPE_ARRAY:
		items, ok := data.([]interface{})
		if !ok {
			break
		}
		elements := make([]value.Value, len(items))
		for i, item := range items {
			created, ok := buildTypedJSONValue(schema.Element, item)
			if !ok {
				return value.Value{}, false
			}
			elements[i] = created
		}
		array := value.NewArray(elements) // RC: o array e dono duravel de cada elemento (construtor retem; espelha OP_ARRAY)
		array.Obj.(*value.ObjArray).RuntimeType.Store(schema)
		return array, true
	case value.TYPE_MAP:
		items, ok := data.(map[string]interface{})
		if !ok || !jsonMapKeyCompatible(schema.Key) {
			break
		}
		mapping := value.NewMap()
		mapObject := mapping.Obj.(*value.ObjMap)
		mapObject.RuntimeType.Store(schema)
		mapData := make(map[interface{}]value.Value, len(items))
		for key, item := range items {
			created, ok := buildTypedJSONValue(schema.Value, item)
			if !ok {
				return value.Value{}, false
			}
			mapData[key] = created
		}
		for _, created := range mapData {
			value.Retain(created) // RC: o map e dono duravel de cada valor (espelha OP_MAP)
		}
		mapObject.Replace(mapData)
		return mapping, true
	case value.TYPE_STRUCT:
		if data == nil {
			return value.NewNull(), true
		}
		items, ok := data.(map[string]interface{})
		if !ok {
			break
		}
		fieldNames := make([]string, 0, len(schema.Fields))
		for name := range schema.Fields {
			fieldNames = append(fieldNames, name)
		}
		sort.Strings(fieldNames)
		definition := &value.ObjStruct{
			Name:              schema.Name,
			Fields:            fieldNames,
			JSONDynamicFields: make(map[string]bool),
		}
		instance := value.NewInstance(definition)
		fields := instance.Obj.(*value.ObjInstance).Fields
		for _, name := range fieldNames {
			if schema.Fields[name] != nil && schema.Fields[name].Kind == value.TYPE_ANY {
				definition.JSONDynamicFields[name] = true
			}
			// RefFields: schema de runtime do slot ref (spec
			// 2026-08-20-ref-slot-invariant §6.1), como o compilador faz para
			// os structs declarados.
			if schema.Fields[name] != nil && schema.Fields[name].Kind == value.TYPE_REF {
				if definition.RefFields == nil {
					definition.RefFields = make(map[string]bool)
				}
				definition.RefFields[name] = true
			}
			item, exists := items[name]
			if !exists {
				return value.Value{}, false
			}
			created, ok := buildTypedJSONValue(schema.Fields[name], item)
			if !ok {
				return value.Value{}, false
			}
			fields[name] = created
		}
		for _, created := range fields {
			value.Retain(created) // RC: campo e dono duravel (espelha o construtor)
		}
		return instance, true
	}
	return value.Value{}, false
}

// buildReferentCell constroi o T pelo schema do referente e devolve uma ref
// para uma CELULA heap nova que o possui — o analogo exato de
// `let novo: T = ...; slot = ref novo` depois que o frame fecha (caixa
// REF_UPVALUE fechada, Owners do valor = 1, como closeUpvalue deixa).
func buildReferentCell(referent *value.RuntimeTypeInfo, data interface{}) (value.Value, bool) {
	built, ok := buildTypedJSONValue(referent, data)
	if !ok {
		return value.Value{}, false
	}
	value.Retain(built) // RC: a celula e o dono duravel do referente
	cell := value.NewClosedUpvalue(built)
	return value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_UPVALUE, Upvalue: cell}}, true
}

func exactJSONInt(data interface{}) (int64, bool) {
	var rational *big.Rat
	switch actual := data.(type) {
	case json.Number:
		var ok bool
		rational, ok = new(big.Rat).SetString(actual.String())
		if !ok {
			return 0, false
		}
	case float64:
		if math.IsInf(actual, 0) || math.IsNaN(actual) {
			return 0, false
		}
		rational = new(big.Rat).SetFloat64(actual)
	default:
		return 0, false
	}
	if rational == nil || !rational.IsInt() || !rational.Num().IsInt64() {
		return 0, false
	}
	return rational.Num().Int64(), true
}

func dynamicJSONValue(data interface{}) (value.Value, bool) {
	switch actual := data.(type) {
	case nil:
		return value.NewNull(), true
	case bool:
		return value.NewBool(actual), true
	case string:
		return value.NewString(actual), true
	case float64:
		if parsed, ok := exactJSONInt(actual); ok {
			return value.NewInt(parsed), true
		}
		if math.IsInf(actual, 0) || math.IsNaN(actual) {
			return value.Value{}, false
		}
		return value.NewFloat(actual), true
	case json.Number:
		if parsed, ok := exactJSONInt(actual); ok {
			return value.NewInt(parsed), true
		}
		parsed, err := actual.Float64()
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return value.Value{}, false
		}
		return value.NewFloat(parsed), true
	case []interface{}:
		elements := make([]value.Value, len(actual))
		for i, item := range actual {
			converted, ok := dynamicJSONValue(item)
			if !ok {
				return value.Value{}, false
			}
			elements[i] = converted
		}
		return value.NewArray(elements), true // RC: o array e dono duravel de cada elemento (construtor retem)
	case map[string]interface{}:
		mapping := value.NewMap()
		mapObject := mapping.Obj.(*value.ObjMap)
		dataMap := make(map[interface{}]value.Value, len(actual))
		for key, item := range actual {
			converted, ok := dynamicJSONValue(item)
			if !ok {
				return value.Value{}, false
			}
			dataMap[key] = converted
		}
		for _, converted := range dataMap {
			value.Retain(converted) // RC: o map e dono duravel de cada valor
		}
		mapObject.Replace(dataMap)
		return mapping, true
	default:
		return value.Value{}, false
	}
}

func jsonMapKeyCompatible(keyType *value.RuntimeTypeInfo) bool {
	return keyType != nil && (keyType.Kind == value.TYPE_STRING || keyType.Kind == value.TYPE_ANY)
}

// jsonStoreThrough devolve o setter que escreve ATRAVES de uma ref pelo funil
// unico de escrita via ref (storeReferenceValue): retain-novo/release-velho,
// consciencia de caixa emprestada (refStorageBorrows) e reaponte da lista de
// posse do frame (retargetOwnedSlot). O erro e descartado: referenceStorage
// acabou de validar o alvo, e uma falha aqui so viria de invalidacao
// concorrente, que a escrita crua tampouco detectaria.
func jsonStoreThrough(vm *VM, target value.Value) jsonSetter {
	return func(updated value.Value) { _ = vm.storeReferenceValue(target, updated) }
}

func jsonReferenceStorage(vm *VM, ref *value.ObjRef) (value.Value, jsonSetter, bool) {
	target := value.Value{Type: value.VAL_REF, Obj: ref}
	_, exists, store, err := vm.referenceStorageMode(ref, true)
	if err != nil || !exists || !store.valid() {
		return value.Value{}, nil, false
	}
	// Semântica de valor: quem chama muta o referente NO LUGAR. Compartilhado
	// — `let copia = i` antes do json_loads — a mutação vazava para a cópia.
	// unicizeThroughRefValue resolve o lugar em modo de escrita, uniciza o
	// conteúdo e grava o clone de volta, que é a mesma disciplina da família
	// *_MUT e do populateRef.
	stored, err := vm.unicizeThroughRefValue(target)
	if err != nil {
		return value.Value{}, nil, false
	}
	return stored, jsonStoreThrough(vm, target), true
}
