package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// setIndexGeneric e o corpo de OP_SET_INDEX: desempilha valor, indice e
// container, escreve em array (guarda de slot ref, retain-antes-de-release)
// ou map (retain/release so se a chave existia) e EMPILHA o valor (resultado
// da atribuicao; o compilador emite OP_POP em seguida). Virou metodo na
// indexacao tipada (issue #66, item 1) para ser o funil unico dos fallbacks
// dos opcodes *_NORC — que materializam a pilha generica, chamam isto e
// desempilham o resultado — sem duplicar a logica de erro; o custo e uma
// chamada a mais por OP_SET_INDEX generico (maps, any, elemento composto),
// medido em bench_map_churn.
func (vm *VM) setIndexGeneric(c *chunk.Chunk, ip int) error {
	val := vm.pop()
	indexVal := vm.pop()
	collectionVal := vm.pop() // The array/map itself is on stack (pointer)

	if collectionVal.Type == value.VAL_OBJ {
		if arr, ok := collectionVal.Obj.(*value.ObjArray); ok {
			if indexVal.Type != value.VAL_INT {
				return vm.runtimeError(c, ip, "array index must be integer")
			}
			idx := int(indexVal.Int())
			if idx < 0 || idx >= len(arr.Elements) {
				return vm.runtimeError(c, ip, "array index out of bounds")
			}
			// Guard do slot ref (spec §6.3): elemento `ref T` (tag
			// RuntimeType) so aceita ref/null; via base tipada o
			// compilador ja rejeitou. O teste de val.Type vem antes
			// para o Load() atomico so rodar em escritas nao-ref.
			if val.Type != value.VAL_REF && val.Type != value.VAL_NULL && arrayElementIsRefSlot(arr) {
				return vm.runtimeError(c, ip, "%s", refSlotWriteError(arr.RuntimeType.Load().Element.String(), val))
			}
			// RC: retain-antes-de-release (elemento e dono duravel)
			old := arr.Elements[idx]
			value.Retain(val)
			arr.Elements[idx] = val
			value.Release(old)
			vm.push(val) // Assignment expression result
			return nil
		} else if mapObj, ok := collectionVal.Obj.(*value.ObjMap); ok {
			var key interface{}
			if indexVal.Type == value.VAL_INT {
				key = indexVal.Int()
			} else if indexVal.Type == value.VAL_OBJ {
				if str, ok := indexVal.Obj.(string); ok {
					key = str
				} else {
					return vm.runtimeError(c, ip, "map key must be int or string")
				}
			} else {
				return vm.runtimeError(c, ip, "map key must be int or string")
			}
			// Guard do slot ref (spec §6.3): valor `ref T` (tag
			// RuntimeType) so aceita ref/null.
			if val.Type != value.VAL_REF && val.Type != value.VAL_NULL && mapValueIsRefSlot(mapObj) {
				return vm.runtimeError(c, ip, "%s", refSlotWriteError(mapObj.RuntimeType.Load().Value.String(), val))
			}
			// RC: so libera o velho se a chave ja existia (dec a
			// menos e proibido); retain-antes-de-release quando existe.
			if old, exists := mapObj.Get(key); exists {
				value.Retain(val)
				mapObj.Set(key, val)
				value.Release(old)
			} else {
				value.Retain(val)
				mapObj.Set(key, val)
			}
			vm.push(val)
			return nil
		}
	}
	return vm.runtimeError(c, ip, "cannot set index on non-array/map")
}
