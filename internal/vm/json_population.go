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
		// Null and legacy-filled ref slots retain their declared referent type.
		return prepareJSONMutation(vm, current, schema.Element, data, set)
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
	newElements := make([]value.Value, len(dataArray))
	commits := make([]jsonCommit, 0, len(dataArray))
	for i, item := range dataArray {
		index := i
		if i < len(array.Elements) {
			newElements[i] = array.Elements[i]
			commit, ok := prepareJSONMutation(vm, array.Elements[i], elementSchema, item, func(updated value.Value) {
				newElements[index] = updated
			})
			if !ok {
				return nil, false
			}
			commits = append(commits, commit)
			continue
		}
		if elementSchema == nil {
			created, ok := dynamicJSONValue(item)
			if !ok {
				return nil, false
			}
			newElements[i] = created
			continue
		}
		created, ok := buildTypedJSONValue(elementSchema, item)
		if !ok {
			return nil, false
		}
		newElements[i] = created
	}
	return func() {
		for _, commit := range commits {
			commit()
		}
		array.Elements = newElements
	}, true
}

func prepareJSONMapMutation(vm *VM, mapping *value.ObjMap, valueSchema *value.RuntimeTypeInfo, data interface{}) (jsonCommit, bool) {
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, false
	}
	newData := make(map[interface{}]value.Value, len(mapping.Data)+len(dataMap))
	for key, item := range mapping.Data {
		newData[key] = item
	}
	commits := make([]jsonCommit, 0, len(dataMap))
	for key, item := range dataMap {
		mapKey := key
		if current, exists := mapping.Data[mapKey]; exists {
			commit, ok := prepareJSONMutation(vm, current, valueSchema, item, func(updated value.Value) {
				newData[mapKey] = updated
			})
			if !ok {
				return nil, false
			}
			commits = append(commits, commit)
			continue
		}
		if valueSchema == nil {
			created, ok := dynamicJSONValue(item)
			if !ok {
				return nil, false
			}
			newData[mapKey] = created
			continue
		}
		created, ok := buildTypedJSONValue(valueSchema, item)
		if !ok {
			return nil, false
		}
		newData[mapKey] = created
	}
	return func() {
		for _, commit := range commits {
			commit()
		}
		mapping.Data = newData
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
		commit, ok := prepareJSONMutation(vm, current, fieldSchema, dataValue, func(updated value.Value) {
			newFields[name] = updated
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
		return buildTypedJSONValue(schema.Element, data)
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
		return value.NewArray(elements), true
	case value.TYPE_MAP:
		items, ok := data.(map[string]interface{})
		if !ok || !jsonMapKeyCompatible(schema.Key) {
			break
		}
		mapping := value.NewMap()
		mapData := mapping.Obj.(*value.ObjMap).Data
		for key, item := range items {
			created, ok := buildTypedJSONValue(schema.Value, item)
			if !ok {
				return value.Value{}, false
			}
			mapData[key] = created
		}
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
		return instance, true
	}
	return value.Value{}, false
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
		return value.NewArray(elements), true
	case map[string]interface{}:
		mapping := value.NewMap()
		dataMap := mapping.Obj.(*value.ObjMap).Data
		for key, item := range actual {
			converted, ok := dynamicJSONValue(item)
			if !ok {
				return value.Value{}, false
			}
			dataMap[key] = converted
		}
		return mapping, true
	default:
		return value.Value{}, false
	}
}

func jsonMapKeyCompatible(keyType *value.RuntimeTypeInfo) bool {
	return keyType != nil && (keyType.Kind == value.TYPE_STRING || keyType.Kind == value.TYPE_ANY)
}

func jsonReferenceStorage(vm *VM, ref *value.ObjRef) (value.Value, jsonSetter, bool) {
	stored, exists, store, err := vm.referenceStorage(ref)
	if err != nil || !exists || store == nil {
		return value.Value{}, nil, false
	}
	return stored, jsonSetter(store), true
}
