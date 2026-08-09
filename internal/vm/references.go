package vm

import (
	"fmt"
	"noxy-vm/internal/value"
)

func referenceMapKey(index value.Value) (interface{}, error) {
	switch index.Type {
	case value.VAL_INT:
		return index.AsInt, nil
	case value.VAL_OBJ:
		if key, ok := index.Obj.(string); ok {
			return key, nil
		}
		return index.Obj, nil
	default:
		return nil, fmt.Errorf("Map key type not fully supported in ref yet")
	}
}

func (vm *VM) lookupReferenceValue(ref *value.ObjRef, frameGlobals map[string]value.Value) (value.Value, error) {
	switch ref.RefType {
	case value.REF_GLOBAL:
		if result, ok := frameGlobals[ref.Name]; ok {
			return result, nil
		}
		if result, ok := vm.GetGlobal(ref.Name); ok {
			return result, nil
		}
		return value.Value{}, fmt.Errorf("undefined global variable '%s'", ref.Name)
	case value.REF_UPVALUE:
		return *ref.Upvalue.Location, nil
	case value.REF_PTR:
		return *ref.Ptr, nil
	case value.REF_PROPERTY:
		instance, ok := ref.Container.Obj.(*value.ObjInstance)
		if !ok {
			return value.Value{}, fmt.Errorf("Target is not an instance")
		}
		if result, ok := instance.Fields[ref.Name]; ok {
			return result, nil
		}
		return value.NewNull(), nil
	case value.REF_INDEX:
		if array, ok := ref.Container.Obj.(*value.ObjArray); ok {
			index := int(ref.Index.AsInt)
			if index < 0 || index >= len(array.Elements) {
				return value.Value{}, fmt.Errorf("Index out of bounds")
			}
			return array.Elements[index], nil
		}
		if mapping, ok := ref.Container.Obj.(*value.ObjMap); ok {
			key, err := referenceMapKey(ref.Index)
			if err != nil {
				return value.Value{}, err
			}
			if result, ok := mapping.Data[key]; ok {
				return result, nil
			}
			return value.NewNull(), nil
		}
		return value.Value{}, fmt.Errorf("Target is not indexable")
	default:
		return value.Value{}, fmt.Errorf("invalid reference target")
	}
}

func (vm *VM) resolveReferenceValue(input value.Value) (value.Value, error) {
	if input.Type == value.VAL_NULL {
		return value.Value{}, fmt.Errorf("cannot dereference null reference")
	}
	if input.Type != value.VAL_REF {
		return value.Value{}, fmt.Errorf("expected reference value, got %s", runtimeValueMode(input))
	}

	var frameGlobals map[string]value.Value
	if vm.currentFrame != nil {
		frameGlobals = vm.currentFrame.Globals
	}
	return vm.lookupReferenceValue(input.Obj.(*value.ObjRef), frameGlobals)
}
