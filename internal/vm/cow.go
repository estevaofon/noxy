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
