package vm

import (
	"fmt"
	"reflect"

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

type referenceSetter func(value.Value)

func validateReferencedValue(stored value.Value) error {
	switch stored.Type {
	case value.VAL_OBJ, value.VAL_FUNCTION, value.VAL_NATIVE, value.VAL_BYTES,
		value.VAL_CHANNEL, value.VAL_WAITGROUP, value.VAL_REF, value.VAL_TASK:
		// These tags require a concrete payload in Obj.
	default:
		return nil
	}
	if stored.Obj == nil {
		return fmt.Errorf("invalid referenced object")
	}
	object := reflect.ValueOf(stored.Obj)
	switch object.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		if object.IsNil() {
			return fmt.Errorf("invalid referenced object")
		}
	}
	if stored.Type == value.VAL_TASK {
		if _, ok := stored.Obj.(*value.ObjTask); !ok {
			return fmt.Errorf("invalid referenced object")
		}
	}
	return nil
}

func extractReferenceValue(input value.Value) (*value.ObjRef, error) {
	if input.Type != value.VAL_REF {
		return nil, fmt.Errorf("expected reference value, got %s", runtimeValueMode(input))
	}
	ref, ok := input.Obj.(*value.ObjRef)
	if !ok || ref == nil {
		return nil, fmt.Errorf("invalid reference value")
	}
	return ref, nil
}

func (vm *VM) referenceStorage(ref *value.ObjRef) (stored value.Value, exists bool, store referenceSetter, err error) {
	defer func() {
		if err == nil && exists {
			if validationErr := validateReferencedValue(stored); validationErr != nil {
				err = validationErr
				store = nil
			}
		}
	}()
	if ref == nil {
		return value.Value{}, false, nil, fmt.Errorf("invalid reference value")
	}
	switch ref.RefType {
	case value.REF_GLOBAL:
		if ref.GlobalOwner == nil {
			return value.Value{}, false, nil, fmt.Errorf("invalid global reference owner")
		}
		stored, ok := ref.GlobalOwner.GetLocal(ref.Name)
		if !ok {
			return value.Value{}, false, nil, fmt.Errorf("undefined global variable '%s'", ref.Name)
		}
		return stored, true, func(updated value.Value) { ref.GlobalOwner.SetLocal(ref.Name, updated) }, nil
	case value.REF_UPVALUE:
		if ref.Upvalue == nil || ref.Upvalue.Location == nil {
			return value.Value{}, false, nil, fmt.Errorf("invalid upvalue reference")
		}
		return *ref.Upvalue.Location, true, func(updated value.Value) { *ref.Upvalue.Location = updated }, nil
	case value.REF_PTR:
		if ref.Ptr == nil {
			return value.Value{}, false, nil, fmt.Errorf("invalid pointer reference")
		}
		return *ref.Ptr, true, func(updated value.Value) { *ref.Ptr = updated }, nil
	case value.REF_PROPERTY:
		instance, ok := ref.Container.Obj.(*value.ObjInstance)
		if ref.Container.Type != value.VAL_OBJ || !ok || instance == nil {
			return value.Value{}, false, nil, fmt.Errorf("Target is not an instance")
		}
		stored, ok := instance.Fields[ref.Name]
		if !ok {
			return value.Value{}, false, nil, fmt.Errorf("undefined property '%s'", ref.Name)
		}
		return stored, true, func(updated value.Value) { instance.Fields[ref.Name] = updated }, nil
	case value.REF_INDEX:
		if array, ok := ref.Container.Obj.(*value.ObjArray); ref.Container.Type == value.VAL_OBJ && ok && array != nil {
			if ref.Index.Type != value.VAL_INT {
				return value.Value{}, false, nil, fmt.Errorf("array reference index must be integer")
			}
			index := int(ref.Index.AsInt)
			if index < 0 || index >= len(array.Elements) {
				return value.Value{}, false, nil, fmt.Errorf("Index out of bounds")
			}
			return array.Elements[index], true, func(updated value.Value) { array.Elements[index] = updated }, nil
		}
		if mapping, ok := ref.Container.Obj.(*value.ObjMap); ref.Container.Type == value.VAL_OBJ && ok && mapping != nil {
			key, err := referenceMapKey(ref.Index)
			if err != nil {
				return value.Value{}, false, nil, err
			}
			stored, exists := mapping.Get(key)
			if !exists {
				stored = value.NewNull()
			}
			return stored, exists, func(updated value.Value) { mapping.Set(key, updated) }, nil
		}
		return value.Value{}, false, nil, fmt.Errorf("Target is not indexable")
	default:
		return value.Value{}, false, nil, fmt.Errorf("invalid reference target")
	}
}

func (vm *VM) lookupGlobalReferenceValue(ref *value.ObjRef) (value.Value, error) {
	stored, _, _, err := vm.referenceStorage(ref)
	return stored, err
}

func (vm *VM) storeGlobalReferenceValue(ref *value.ObjRef, updated value.Value) error {
	_, _, store, err := vm.referenceStorage(ref)
	if err != nil {
		return err
	}
	store(updated)
	return nil
}

func (vm *VM) lookupReferenceValue(ref *value.ObjRef) (value.Value, error) {
	stored, _, _, err := vm.referenceStorage(ref)
	return stored, err
}

func (vm *VM) storeReferenceValue(input value.Value, updated value.Value) error {
	if input.Type == value.VAL_NULL {
		return fmt.Errorf("cannot update null reference")
	}
	ref, err := extractReferenceValue(input)
	if err != nil {
		return err
	}
	_, _, store, err := vm.referenceStorage(ref)
	if err != nil {
		return err
	}
	store(updated)
	return nil
}

func (vm *VM) resolveReferenceValue(input value.Value) (value.Value, error) {
	if input.Type == value.VAL_NULL {
		return value.Value{}, fmt.Errorf("cannot dereference null reference")
	}
	ref, err := extractReferenceValue(input)
	if err != nil {
		return value.Value{}, err
	}
	return vm.lookupReferenceValue(ref)
}
