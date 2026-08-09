package vm

import (
	"math"
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
		replacement := goValToNoxy(data)
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
	replacement := goValToNoxy(data)
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
			newElements[i] = goValToNoxy(item)
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
			newData[mapKey] = goValToNoxy(item)
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
		return goValToNoxy(data), true
	case value.TYPE_NULL:
		if data == nil {
			return value.NewNull(), true
		}
	case value.TYPE_BOOL:
		if actual, ok := data.(bool); ok {
			return value.NewBool(actual), true
		}
	case value.TYPE_INT:
		if actual, ok := data.(float64); ok && actual >= math.MinInt64 && actual <= math.MaxInt64 && actual == math.Trunc(actual) {
			return value.NewInt(int64(actual)), true
		}
	case value.TYPE_FLOAT:
		if actual, ok := data.(float64); ok {
			return value.NewFloat(actual), true
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
				continue
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

func jsonMapKeyCompatible(keyType *value.RuntimeTypeInfo) bool {
	return keyType != nil && (keyType.Kind == value.TYPE_STRING || keyType.Kind == value.TYPE_ANY)
}

func jsonReferenceStorage(vm *VM, ref *value.ObjRef) (value.Value, jsonSetter, bool) {
	if ref == nil {
		return value.Value{}, nil, false
	}
	switch ref.RefType {
	case value.REF_GLOBAL:
		if vm.currentFrame != nil && vm.currentFrame.Globals != nil {
			if stored, ok := vm.currentFrame.Globals[ref.Name]; ok {
				return stored, func(updated value.Value) { vm.currentFrame.Globals[ref.Name] = updated }, true
			}
		}
		stored, ok := vm.GetGlobal(ref.Name)
		if !ok {
			return value.Value{}, nil, false
		}
		return stored, func(updated value.Value) { vm.SetGlobal(ref.Name, updated) }, true
	case value.REF_UPVALUE:
		if ref.Upvalue == nil || ref.Upvalue.Location == nil {
			return value.Value{}, nil, false
		}
		return *ref.Upvalue.Location, func(updated value.Value) { *ref.Upvalue.Location = updated }, true
	case value.REF_PTR:
		if ref.Ptr == nil {
			return value.Value{}, nil, false
		}
		return *ref.Ptr, func(updated value.Value) { *ref.Ptr = updated }, true
	case value.REF_PROPERTY:
		instance, ok := ref.Container.Obj.(*value.ObjInstance)
		if ref.Container.Type != value.VAL_OBJ || !ok {
			return value.Value{}, nil, false
		}
		stored, ok := instance.Fields[ref.Name]
		if !ok {
			return value.Value{}, nil, false
		}
		return stored, func(updated value.Value) { instance.Fields[ref.Name] = updated }, true
	case value.REF_INDEX:
		if array, ok := ref.Container.Obj.(*value.ObjArray); ref.Container.Type == value.VAL_OBJ && ok {
			if ref.Index.Type != value.VAL_INT {
				return value.Value{}, nil, false
			}
			index := int(ref.Index.AsInt)
			if index < 0 || index >= len(array.Elements) {
				return value.Value{}, nil, false
			}
			return array.Elements[index], func(updated value.Value) { array.Elements[index] = updated }, true
		}
		if mapping, ok := ref.Container.Obj.(*value.ObjMap); ref.Container.Type == value.VAL_OBJ && ok {
			key, err := referenceMapKey(ref.Index)
			if err != nil {
				return value.Value{}, nil, false
			}
			stored, ok := mapping.Data[key]
			if !ok {
				return value.Value{}, nil, false
			}
			return stored, func(updated value.Value) { mapping.Data[key] = updated }, true
		}
	}
	return value.Value{}, nil, false
}
