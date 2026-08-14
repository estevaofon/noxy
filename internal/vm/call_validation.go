package vm

import (
	"fmt"
	"noxy-vm/internal/value"
)

func runtimeValueMode(v value.Value) string {
	switch v.Type {
	case value.VAL_BOOL:
		return "bool"
	case value.VAL_NULL:
		return "null"
	case value.VAL_INT:
		return "int"
	case value.VAL_FLOAT:
		return "float"
	case value.VAL_OBJ:
		return "object"
	case value.VAL_FUNCTION:
		return "func"
	case value.VAL_NATIVE:
		return "native"
	case value.VAL_BYTES:
		return "bytes"
	case value.VAL_CHANNEL:
		return "chan"
	case value.VAL_WAITGROUP:
		return "waitgroup"
	case value.VAL_REF:
		return "ref"
	case value.VAL_TASK:
		return "task"
	default:
		return "unknown"
	}
}

func validateParameterModes(name string, params []value.ParamInfo, args []value.Value) error {
	for i, param := range params {
		if i >= len(args) {
			break
		}
		actual := args[i]
		if param.IsRef && actual.Type != value.VAL_REF && actual.Type != value.VAL_NULL {
			return fmt.Errorf("function '%s' argument %d: expected %s, got %s", name, i+1, param.TypeName, runtimeValueMode(actual))
		}
		if !param.IsRef && actual.Type == value.VAL_REF {
			return fmt.Errorf("function '%s' argument %d: expected %s, got ref", name, i+1, param.TypeName)
		}
	}
	return nil
}
