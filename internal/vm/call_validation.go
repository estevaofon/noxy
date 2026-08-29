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

// validateRefTargets confere o TIPO DO ALVO de cada argumento `ref` contra o
// parametro `ref T` (revisao do #119, condicao 2): validateParameterModes so
// olha o modo, e um `any` guardando `ref string` entrava num `ref int` e era
// lido como int. Roda so no caminho de modo nao provado (OP_CALL com
// argumento any/desconhecido) — codigo tipado usa OP_CALL_STATIC e nao passa
// aqui. Alvo null segue encaminhado (ref-null-forwarding: falha na leitura).
func (vm *VM) validateRefTargets(name string, schema *value.RuntimeTypeInfo, args []value.Value) error {
	if schema == nil || len(schema.ParamIsRef) != len(schema.Params) {
		return nil
	}
	for i, param := range schema.Params {
		if i >= len(args) || !schema.ParamIsRef[i] || args[i].Type != value.VAL_REF {
			continue
		}
		if param == nil || param.Kind != value.TYPE_REF || param.Element == nil {
			continue
		}
		target, err := vm.resolveReferenceValue(args[i])
		if err != nil || target.Type == value.VAL_NULL {
			continue
		}
		if !vm.runtimeValueMatchesType(target, param.Element) {
			return fmt.Errorf("function '%s' argument %d: expected %s, got %s", name, i+1, param.String(), vm.runtimeValueDescription(args[i]))
		}
	}
	return nil
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
		// R2 (spec 2026-08-24-explicit-ref): um parametro `any` recebe o ref
		// COMO VALOR, igual a print/to_str — nunca ha leitura implicita ali.
		// So um parametro de tipo concreto recusa VAL_REF, porque nesse caso
		// o ref precisaria ser lido para virar o valor esperado.
		if !param.IsRef && actual.Type == value.VAL_REF && param.TypeName != "any" {
			return fmt.Errorf("function '%s' argument %d: expected %s, got ref", name, i+1, param.TypeName)
		}
	}
	return nil
}
