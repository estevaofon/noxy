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
		return nil, fmt.Errorf("map reference key must be int or string")
	default:
		return nil, fmt.Errorf("map reference key must be int or string")
	}
}

func (vm *VM) lookupGlobalReferenceValue(ref *value.ObjRef) (value.Value, error) {
	if ref.GlobalOwner == nil || *ref.GlobalOwner == nil {
		return value.Value{}, fmt.Errorf("invalid global reference owner")
	}
	if ref.GlobalOwner == &vm.shared.Globals {
		if result, ok := vm.GetGlobal(ref.Name); ok {
			return result, nil
		}
	} else if result, ok := (*ref.GlobalOwner)[ref.Name]; ok {
		return result, nil
	}
	return value.Value{}, fmt.Errorf("undefined global variable '%s'", ref.Name)
}

func (vm *VM) storeGlobalReferenceValue(ref *value.ObjRef, updated value.Value) error {
	if ref.GlobalOwner == nil || *ref.GlobalOwner == nil {
		return fmt.Errorf("invalid global reference owner")
	}
	if ref.GlobalOwner == &vm.shared.Globals {
		vm.SetGlobal(ref.Name, updated)
		return nil
	}
	(*ref.GlobalOwner)[ref.Name] = updated
	return nil
}

func (vm *VM) lookupReferenceValue(ref *value.ObjRef) (value.Value, error) {
	if ref == nil {
		return value.Value{}, fmt.Errorf("invalid reference value")
	}
	switch ref.RefType {
	case value.REF_GLOBAL:
		return vm.lookupGlobalReferenceValue(ref)
	case value.REF_UPVALUE:
		if ref.Upvalue == nil || ref.Upvalue.Location == nil {
			return value.Value{}, fmt.Errorf("invalid upvalue reference")
		}
		return *ref.Upvalue.Location, nil
	case value.REF_PTR:
		if ref.Ptr == nil {
			return value.Value{}, fmt.Errorf("invalid pointer reference")
		}
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
			if ref.Index.Type != value.VAL_INT {
				return value.Value{}, fmt.Errorf("array reference index must be integer")
			}
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

	ref, ok := input.Obj.(*value.ObjRef)
	if !ok || ref == nil {
		return value.Value{}, fmt.Errorf("invalid reference value")
	}
	return vm.lookupReferenceValue(ref)
}
