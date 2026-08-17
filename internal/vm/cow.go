package vm

import (
	"sync/atomic"

	"noxy-vm/internal/value"
)

// cloneCount conta clones CoW; visível para testes e diagnóstico.
var cloneCount atomic.Int64

func ResetCloneCount()       { cloneCount.Store(0) }
func CloneCountValue() int64 { return cloneCount.Load() }

// unicize garante posse exclusiva de um composto: se o valor está marcado
// Shared, devolve um clone raso (com os filhos marcados Shared) e true;
// caso contrário devolve o próprio valor e false.
func (vm *VM) unicize(v value.Value) (value.Value, bool) {
	if !value.IsShared(v) {
		return v, false
	}
	return vm.copyValue(v), true
}

// unicizeThroughRefValue resolve um valor VAL_REF (slot: variável, campo,
// índice…), garante posse exclusiva do composto armazenado e grava o clone
// de volta no slot pelo setter quando clona. Devolve o valor único.
func (vm *VM) unicizeThroughRefValue(refArg value.Value) (value.Value, error) {
	ref, err := extractReferenceValue(refArg)
	if err != nil {
		return value.Value{}, err
	}
	stored, _, store, err := vm.referenceStorage(ref)
	if err != nil {
		return value.Value{}, err
	}
	v, changed := vm.unicize(stored)
	if changed {
		// RC: o clone substitui o ocupante velho no destino apontado pelo
		// ref; retain-antes-de-release em torno da troca. Lugar que apenas
		// empresta (caixa de upvalue emprestada) troca sem contar posse.
		if !refStorageBorrows(ref) {
			value.Retain(v)
			// mesma correcao do storeReferenceValue: a entrada de posse do frame
			// passa a nomear o clone, senao o fim do frame soltaria o valor
			// velho uma segunda vez (dec a mais).
			vm.retargetOwnedSlot(ref, v)
			value.Release(stored)
		}
		store(v)
	}
	return v, nil
}
