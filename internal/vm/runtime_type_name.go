package vm

import (
	"fmt"
	"regexp"

	"noxy-vm/internal/value"
)

// moduleQualifierPattern casa cada qualificador de modulo (`main::`,
// `strings::`, caminhos de pacote) dentro de um nome de instancia, inclusive
// nos argumentos aninhados (`main::Caixa<main::Caixa<int>>`). O qualificador
// e identidade interna da monomorfizacao e nao interessa ao usuario.
var moduleQualifierPattern = regexp.MustCompile(`[^<>,\s]+::`)

func displayStructName(name string) string {
	return moduleQualifierPattern.ReplaceAllString(name, "")
}

// runtimeValueDescription e o nome de tipo de um valor nos diagnosticos de
// fronteira dinamica (`expected T, got U`, #118): um composto ja etiquetado
// mostra a etiqueta (`int[4]`, `map[string, int]`, `chan string`), uma
// referencia mostra o alvo (`ref string`, `ref null`); o resto, o nome de
// runtime de sempre (runtimeTypeName).
func (vm *VM) runtimeValueDescription(val value.Value) string {
	switch val.Type {
	case value.VAL_REF:
		if target, err := vm.resolveReferenceValue(val); err == nil {
			return "ref " + vm.runtimeValueDescription(target)
		}
	case value.VAL_OBJ:
		switch obj := val.Obj.(type) {
		case *value.ObjArray:
			if tag := obj.RuntimeType.Load(); tag != nil {
				return tag.String()
			}
		case *value.ObjMap:
			if tag := obj.RuntimeType.Load(); tag != nil {
				return tag.String()
			}
		}
	case value.VAL_CHANNEL:
		if channel, ok := val.Obj.(*value.ObjChannel); ok && channel != nil {
			channel.Lock.Lock()
			elementType := channel.ElementType
			channel.Lock.Unlock()
			if elementType != nil {
				return (&value.RuntimeTypeInfo{Kind: value.TYPE_CHANNEL, Element: elementType}).String()
			}
		}
	}
	return runtimeTypeName(val)
}

// runtimeTypeName e a fonte unica dos nomes de tipo em runtime, compartilhada
// pelo builtin `type` e pelo verbo %T do fmt.
func runtimeTypeName(val value.Value) string {
	switch val.Type {
	case value.VAL_INT:
		return "int"
	case value.VAL_FLOAT:
		return "float"
	case value.VAL_BOOL:
		return "bool"
	case value.VAL_NULL:
		return "null"
	case value.VAL_BYTES:
		return "bytes"
	case value.VAL_FUNCTION, value.VAL_NATIVE:
		return "function"
	case value.VAL_TASK:
		return "task"
	case value.VAL_CHANNEL:
		return "channel"
	case value.VAL_WAITGROUP:
		return "waitgroup"
	case value.VAL_REF:
		return "ref"
	case value.VAL_OBJ:
		switch obj := val.Obj.(type) {
		case string:
			return "string"
		case *value.ObjArray:
			return "array"
		case *value.ObjMap:
			return "map"
		case *value.ObjInstance:
			return displayStructName(obj.Struct.Name)
		case *value.ObjStruct:
			return "struct" // definicao (o construtor como valor), nao instancia
		default:
			return fmt.Sprintf("%T", val.Obj)
		}
	}
	return "unknown"
}
