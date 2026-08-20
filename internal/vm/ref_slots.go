package vm

import (
	"fmt"

	"noxy-vm/internal/value"
)

// Invariante do slot `ref T` (spec docs/superpowers/specs/
// 2026-08-20-ref-slot-invariant-design.md): um slot declarado `ref T` —
// campo de struct, elemento de `(ref T)[]`, valor de `map[K, ref T]` —
// contem uma referencia ou null. O compilador garante isso para bases
// tipadas; em fronteira dinamica (base `any`) o runtime consulta o schema
// (ObjStruct.RefFields, tag RuntimeType de array/map) e aplica a mesma
// regra. Qualquer outra coisa num slot ref e estado impossivel e vira erro
// explicito em vez de ser embrulhado numa ref para o slot (shim da #51).

// forwardRefSlot devolve o conteudo de um slot `ref T` para encaminhamento
// (spec §2.3 regra 2, §4.2): ref ou null passam como estao.
func forwardRefSlot(stored value.Value, slot string) (value.Value, error) {
	if stored.Type == value.VAL_REF || stored.Type == value.VAL_NULL {
		return stored, nil
	}
	return value.Value{}, fmt.Errorf("reference slot %s holds a non-reference value", slot)
}

// describeRefSlotIndex nomeia o slot de um indice nas mensagens de erro:
// `at index 3` para array, `for key "k"` para map com chave string.
func describeRefSlotIndex(index value.Value) string {
	if index.Type == value.VAL_OBJ {
		if key, ok := index.Obj.(string); ok {
			return fmt.Sprintf("for key %q", key)
		}
	}
	return fmt.Sprintf("at index %s", index.String())
}

// arrayElementIsRefSlot: o array passou por um contexto tipado que o etiquetou
// com `(ref T)[]`. Sem tag nao ha informacao (fronteira dinamica pura).
func arrayElementIsRefSlot(array *value.ObjArray) bool {
	if array == nil {
		return false
	}
	tag := array.RuntimeType.Load()
	return tag != nil && tag.Kind == value.TYPE_ARRAY && tag.Element != nil && tag.Element.Kind == value.TYPE_REF
}

// mapValueIsRefSlot: idem para `map[K, ref T]`.
func mapValueIsRefSlot(mapping *value.ObjMap) bool {
	if mapping == nil {
		return false
	}
	tag := mapping.RuntimeType.Load()
	return tag != nil && tag.Kind == value.TYPE_MAP && tag.Value != nil && tag.Value.Kind == value.TYPE_REF
}

// structRefFieldTypeName devolve o tipo declarado do campo (`ref Node`) a
// partir de ConstructorType.Params — so para mensagens de erro (caminho
// frio); sem ConstructorType valido cai em "reference field 'nome'".
func structRefFieldTypeName(definition *value.ObjStruct, name string) string {
	if schema, ok := validStructConstructorType(definition); ok {
		for i, field := range definition.Fields {
			if field == name && i < len(schema.Params) && schema.Params[i] != nil {
				return schema.Params[i].String()
			}
		}
	}
	return fmt.Sprintf("reference field '%s'", name)
}

// refSlotWriteError e o gemeo dinamico do erro de compilacao
// "cannot assign T to ref T" (spec §2.3), usado quando a escrita chega por
// fronteira dinamica (base `any`) — spec §6.3.
func refSlotWriteError(expected string, val value.Value) string {
	return fmt.Sprintf("cannot assign %s to %s", runtimeTypeName(val), expected)
}
