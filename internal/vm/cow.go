package vm

import (
	"fmt"
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

// unicizeOwnedSlot e a semantica de OP_GET_LOCAL_MUT para um slot POSSUIDOR:
// se o composto no slot esta compartilhado, grava um clone no slot (ownSlot
// mantem o slot registrado em frame.Owned, como OP_SET_LOCAL) e solta o
// velho; devolve o ocupante (unico) do slot. Metodo para ser o funil comum do
// case generico e do fallback de OP_SET_LOCAL_INDEX_ARRAY_NORC (issue #66).
func (vm *VM) unicizeOwnedSlot(frame *CallFrame, idx int) value.Value {
	v := vm.stack[idx]
	if value.IsShared(v) {
		old := v
		v = vm.copyValue(v)
		vm.stack[idx] = v
		// RC: usa ownSlot (mantem o slot registrado em frame.Owned)
		// em vez de Retain cru — mesmo padrao do OP_SET_LOCAL.
		frame.ownSlot(vm, idx)
		value.Release(old)
	}
	return v
}

// unicizeBorrowedSlot e o gemeo de EMPRESTIMO (OP_GET_LOCAL_MUT_BORROW): slot
// de tipo `ref T` nao possui o que guarda, entao o clone fica no slot sem
// retain do novo nem release do velho (soltar o que nunca se reteve e dec a
// menos, e faria o objeto compartilhado parecer unico). A mutacao adiante
// vai para o clone, exatamente como no comportamento pre-RC. Tambem e o
// fallback de OP_SET_REF_LOCAL_INDEX_ARRAY_NORC quando o slot guarda um valor
// plano (tolerancia herdada do auto-deref antigo).
func (vm *VM) unicizeBorrowedSlot(idx int) value.Value {
	v := vm.stack[idx]
	if value.IsShared(v) {
		v = vm.copyValue(v)
		vm.stack[idx] = v
	}
	return v
}

// unicizeThroughRefValue resolve um valor VAL_REF (slot: variável, campo,
// índice…), garante posse exclusiva do composto armazenado e grava o clone
// de volta no slot pelo setter quando clona. Devolve o valor único.
func (vm *VM) unicizeThroughRefValue(refArg value.Value) (value.Value, error) {
	ref, err := extractReferenceValue(refArg)
	if err != nil {
		return value.Value{}, err
	}
	// Modo de ESCRITA: preparar para mutar. Se o ref for um empréstimo com
	// lugar de pai (issue #83), o caminho raiz->contêiner inteiro é unicizado
	// e gravado de volta antes de o nível de cá ser unicizado.
	stored, exists, store, err := vm.referenceStorageMode(ref, true)
	if err != nil {
		return value.Value{}, err
	}
	// O lugar sumiu durante a vida da referência (entrada de map apagada). A
	// mesma recusa de storeReferenceValue, e aqui ela precisa sair NESTE nível:
	// sem isso o buraco só aparecia um nível adiante, como "Target is not an
	// instance" — um erro de tipo, que descreve o sintoma e não a causa.
	if !exists {
		return value.Value{}, fmt.Errorf("reference target no longer exists")
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
