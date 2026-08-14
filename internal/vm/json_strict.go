package vm

import (
	"errors"
	"math"
	"unicode/utf8"

	"noxy-vm/internal/value"
)

var (
	errJSONUnsupported = errors.New("value is not JSON-compatible")
	errJSONNonFinite   = errors.New("non-finite float is not valid JSON")
	errJSONCycle       = errors.New("cyclic value is not valid JSON")
	errJSONInvalidUTF8 = errors.New("invalid UTF-8 is not valid JSON")
)

type strictJSONTraversal struct {
	arrays map[*value.ObjArray]bool
	maps   map[*value.ObjMap]bool
}

func strictJSONValToGo(input value.Value) (interface{}, error) {
	traversal := strictJSONTraversal{
		arrays: make(map[*value.ObjArray]bool),
		maps:   make(map[*value.ObjMap]bool),
	}
	return traversal.convert(input)
}

func (t *strictJSONTraversal) convert(input value.Value) (interface{}, error) {
	switch input.Type {
	case value.VAL_NULL:
		return nil, nil
	case value.VAL_BOOL:
		return input.AsBool, nil
	case value.VAL_INT:
		return input.AsInt, nil
	case value.VAL_FLOAT:
		if math.IsNaN(input.AsFloat) || math.IsInf(input.AsFloat, 0) {
			return nil, errJSONNonFinite
		}
		return input.AsFloat, nil
	case value.VAL_OBJ:
		switch object := input.Obj.(type) {
		case string:
			if !utf8.ValidString(object) {
				return nil, errJSONInvalidUTF8
			}
			return object, nil
		case *value.ObjArray:
			if object == nil {
				return nil, errJSONUnsupported
			}
			return t.convertArray(object)
		case *value.ObjMap:
			if object == nil {
				return nil, errJSONUnsupported
			}
			return t.convertMap(object)
		default:
			return nil, errJSONUnsupported
		}
	default:
		return nil, errJSONUnsupported
	}
}

func (t *strictJSONTraversal) convertArray(array *value.ObjArray) ([]interface{}, error) {
	if t.arrays[array] {
		return nil, errJSONCycle
	}
	t.arrays[array] = true
	defer delete(t.arrays, array)
	converted := make([]interface{}, len(array.Elements))
	for index, element := range array.Elements {
		item, err := t.convert(element)
		if err != nil {
			return nil, err
		}
		converted[index] = item
	}
	return converted, nil
}

func (t *strictJSONTraversal) convertMap(mapping *value.ObjMap) (map[string]interface{}, error) {
	if t.maps[mapping] {
		return nil, errJSONCycle
	}
	t.maps[mapping] = true
	defer delete(t.maps, mapping)
	values := mapping.Snapshot()
	converted := make(map[string]interface{}, len(values))
	for key, item := range values {
		stringKey, ok := key.(string)
		if !ok {
			return nil, errJSONUnsupported
		}
		if !utf8.ValidString(stringKey) {
			return nil, errJSONInvalidUTF8
		}
		convertedValue, err := t.convert(item)
		if err != nil {
			return nil, err
		}
		converted[stringKey] = convertedValue
	}
	return converted, nil
}
