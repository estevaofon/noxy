package vm

import "noxy-vm/internal/value"

func (vm *VM) appendItemCompatible(target *value.ObjRef, item value.Value) bool {
	if item.Type == value.VAL_REF {
		itemRef, ok := item.Obj.(*value.ObjRef)
		if !ok || itemRef == nil {
			return false
		}
	}
	if target == nil || target.TargetType == nil {
		return true
	}
	arrayType := target.TargetType
	if arrayType.Kind != value.TYPE_ARRAY || arrayType.Element == nil {
		return false
	}
	elementType := arrayType.Element
	if elementType.Kind != value.TYPE_REF {
		if item.Type == value.VAL_REF {
			return false
		}
		return runtimeValueMatchesType(item, elementType)
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
	return runtimeValueMatchesType(resolved, elementType.Element)
}

func runtimeValueMatchesType(actual value.Value, expected *value.RuntimeTypeInfo) bool {
	if expected == nil || expected.Kind == value.TYPE_ANY {
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
		for _, element := range array.Elements {
			if !runtimeValueMatchesType(element, expected.Element) {
				return false
			}
		}
		return true
	case value.TYPE_MAP:
		mapping, ok := actual.Obj.(*value.ObjMap)
		if actual.Type != value.VAL_OBJ || !ok {
			return false
		}
		for key, element := range mapping.Data {
			if !runtimeMapKeyMatchesType(key, expected.Key) || !runtimeValueMatchesType(element, expected.Value) {
				return false
			}
		}
		return true
	case value.TYPE_REF:
		ref, ok := actual.Obj.(*value.ObjRef)
		return actual.Type == value.VAL_REF && ok && ref != nil
	case value.TYPE_STRUCT:
		instance, ok := actual.Obj.(*value.ObjInstance)
		return actual.Type == value.VAL_OBJ && ok && instance.Struct != nil && instance.Struct.Name == expected.Name
	default:
		return false
	}
}

func runtimeMapKeyMatchesType(key interface{}, expected *value.RuntimeTypeInfo) bool {
	if expected == nil || expected.Kind == value.TYPE_ANY {
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
