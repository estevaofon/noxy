package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Campo de struct por indice (issue #96): OP_GET_FIELD / OP_SET_FIELD /
// OP_GET_FIELD_MUT levam [idx][nome]. O caminho rapido (em run()) usa o indice
// quando `Fields[idx] == nome` na definicao da instancia; qualquer outra coisa
// — base ref, null, any que virou map, definicao com outra ordem (json_loads
// monta a sua em ordem alfabetica), campo `ref T` na escrita — cai nos funis
// abaixo, que sao os corpos dos opcodes por nome extraidos em metodo. Os
// `case` genericos chamam os mesmos metodos: uma fonte para mensagens, linha
// (`Lines[ip-1]`, o ultimo byte de operando) e semantica CoW/RC.

// getPropertyGeneric: OP_GET_PROPERTY. [base] -> [val].
func (vm *VM) getPropertyGeneric(c *chunk.Chunk, ip int, name string) error {
	instanceVal := vm.pop()

	// Auto-dereference if instance is a ref
	if instanceVal.Type == value.VAL_REF {
		resolved, err := vm.resolveReferenceValue(instanceVal)
		if err != nil {
			return vm.runtimeError(c, ip, "%s", err)
		}
		instanceVal = resolved
	}

	if instanceVal.Type != value.VAL_OBJ {
		return vm.runtimeError(c, ip, "only instances/maps have properties")
	}

	if instance, ok := instanceVal.Obj.(*value.ObjInstance); ok {
		val, ok := instance.Get(name)
		if !ok {
			return vm.runtimeError(c, ip, "undefined property '%s'", name)
		}
		vm.push(val)
	} else if mapObj, ok := instanceVal.Obj.(*value.ObjMap); ok {
		// Allow accessing map keys as properties (for modules)
		val, ok := mapObj.Get(name)
		if !ok {
			return vm.runtimeError(c, ip, "undefined property '%s' in module/map", name)
		}
		vm.push(val)
	} else {
		return vm.runtimeError(c, ip, "only instances and maps have properties")
	}
	return nil
}

// setPropertyGeneric: OP_SET_PROPERTY. [base, val] -> [val] (o compilador
// emite OP_POP depois do opcode por nome; a forma por indice consome o push).
func (vm *VM) setPropertyGeneric(c *chunk.Chunk, ip int, name string) error {
	val := vm.pop()
	instanceVal := vm.pop()

	// issue #83: aqui a referência é ALVO DE ESCRITA, então a resolução tem de
	// ser em modo de escrita — unicizar o caminho e gravar os clones de volta.
	if instanceVal.Type == value.VAL_REF {
		resolved, err := vm.unicizeThroughRefValue(instanceVal)
		if err != nil {
			return vm.runtimeError(c, ip, "%s", err)
		}
		instanceVal = resolved
	}

	if instanceVal.Type != value.VAL_OBJ {
		return vm.runtimeError(c, ip, "only instances and maps have properties")
	}
	if mapping, isMap := instanceVal.Obj.(*value.ObjMap); isMap && mapping != nil {
		// Issue #133: membro de modulo pelo namespace (ObjMap sobre o
		// bindingStore do modulo) e map acessado como propriedade. Chave
		// inexistente e erro como na leitura; RC: retain-antes-de-release —
		// ObjMap.Swap nao toca contadores. A troca e UMA secao critica que
		// avanca a geracao do store (invalida caches de leitura do modulo):
		// com Get+Set separados dois escritores concorrentes leriam o mesmo
		// valor velho e ambos o liberariam (double free). O Get abaixo so
		// serve as checagens de existencia e de R1, que ja nao sao atomicas
		// com a escrita (docs/concurrency.md).
		old, exists := mapping.Get(name)
		if !exists {
			return vm.runtimeError(c, ip, "undefined property '%s' in module/map", name)
		}
		if old.Type == value.VAL_REF && val.Type != value.VAL_REF && val.Type != value.VAL_NULL {
			return vm.runtimeError(c, ip, "slot '%s' already holds a reference\n  hint: pass it directly, without 'ref'", name)
		}
		value.Retain(val)
		old, _ = mapping.Swap(name, val)
		value.Release(old)
		vm.push(val)
		return nil
	}
	instance, ok := instanceVal.Obj.(*value.ObjInstance)
	if !ok {
		return vm.runtimeError(c, ip, "only instances and maps have properties")
	}

	// Struct e nominal, de campos fixos (spec §5): escrever num nome fora da
	// declaracao e o mesmo "undefined property" da leitura (issue #61 item 2).
	slot, declared := instance.Struct.FieldIndex(name)
	if !declared {
		return vm.runtimeError(c, ip, "undefined property '%s'", name)
	}
	old := instance.Slots[slot]

	// Guard do slot ref (spec 2026-08-20-ref-slot-invariant §6.3):
	// via base tipada o compilador ja rejeitou; aqui so dispara em
	// fronteira dinamica (`any`) e no fallback do OP_SET_FIELD, que recusa
	// campo `ref T` no caminho rapido. Lookup em mapa nil e gratuito.
	if instance.Struct.FieldIsRef(name) && val.Type != value.VAL_REF && val.Type != value.VAL_NULL {
		return vm.runtimeError(c, ip, "%s", refSlotWriteError(structRefFieldTypeName(instance.Struct, name), val))
	}

	// RC: retain-antes-de-release (campo e dono duravel); Release
	// em slot null e no-op (nao e VAL_OBJ)
	value.Retain(val)
	instance.Slots[slot] = val
	value.Release(old)
	vm.push(val)
	return nil
}

// getPropMutGeneric: OP_GET_PROP_MUT. [base] -> [campo unicizado].
func (vm *VM) getPropMutGeneric(c *chunk.Chunk, ip int, name string) error {
	instanceVal := vm.pop()
	if instanceVal.Type == value.VAL_REF {
		uniq, err := vm.unicizeThroughRefValue(instanceVal)
		if err != nil {
			return vm.runtimeError(c, ip, "%s", err)
		}
		instanceVal = uniq
	}
	if instanceVal.Type != value.VAL_OBJ {
		return vm.runtimeError(c, ip, "only instances/maps have properties")
	}
	if instance, ok := instanceVal.Obj.(*value.ObjInstance); ok {
		slot, ok := instance.Struct.FieldIndex(name)
		if !ok {
			return vm.runtimeError(c, ip, "undefined property '%s'", name)
		}
		fieldVal := instance.Slots[slot]
		if value.IsShared(fieldVal) {
			old := fieldVal
			fieldVal = vm.copyValue(fieldVal)
			// RC: retain-antes-de-release em torno da troca
			value.Retain(fieldVal)
			instance.Slots[slot] = fieldVal
			value.Release(old)
		}
		vm.push(fieldVal)
	} else if mapObj, ok := instanceVal.Obj.(*value.ObjMap); ok {
		// Membros de módulo (e maps acessados como propriedade)
		stored, ok := mapObj.Get(name)
		if !ok {
			return vm.runtimeError(c, ip, "undefined property '%s' in module/map", name)
		}
		v, changed := vm.unicize(stored)
		if changed {
			value.Retain(v)
			mapObj.Set(name, v)
			value.Release(stored)
		}
		vm.push(v)
	} else {
		return vm.runtimeError(c, ip, "only instances and maps have properties")
	}
	return nil
}
