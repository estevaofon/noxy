package value

import (
	"math"
	"sync/atomic"
)

// MarkShared liga o bit sticky de compartilhamento em compostos (CoW).
// No-op para escalares e demais tipos.
func MarkShared(v Value) {
	if v.Type != VAL_OBJ {
		return
	}
	switch obj := v.Obj.(type) {
	case *ObjArray:
		obj.Shared.Store(true)
	case *ObjMap:
		obj.Shared.Store(true)
	case *ObjInstance:
		obj.Shared.Store(true)
	}
}

// IsShared informa se o composto está marcado como compartilhado.
func IsShared(v Value) bool {
	if v.Type != VAL_OBJ {
		return false
	}
	switch obj := v.Obj.(type) {
	case *ObjArray:
		return obj.Shared.Load()
	case *ObjMap:
		return obj.Shared.Load()
	case *ObjInstance:
		return obj.Shared.Load()
	}
	return false
}

// ownersSaturation impede overflow do contador; acima disso o valor se
// comporta como permanentemente compartilhado (equivalente ao sticky).
const ownersSaturation = math.MaxInt32 / 2

func ownersOf(v Value) *atomic.Int32 {
	if v.Type != VAL_OBJ {
		return nil
	}
	switch obj := v.Obj.(type) {
	case *ObjArray:
		return &obj.Owners
	case *ObjMap:
		return &obj.Owners
	case *ObjInstance:
		return &obj.Owners
	}
	return nil
}

// Retain registra um dono durável novo. Retorna true se o valor é um
// composto rastreável (chamador decide se registra o slot para release).
func Retain(v Value) bool {
	owners := ownersOf(v)
	if owners == nil {
		return false
	}
	if owners.Load() < ownersSaturation {
		owners.Add(1)
	}
	return true
}

// Release solta um dono durável. Nunca desce abaixo de zero (dec a mais é
// proibido por design; o clamp protege contra funis duplicados).
func Release(v Value) {
	owners := ownersOf(v)
	if owners == nil {
		return
	}
	for {
		current := owners.Load()
		if current <= 0 || current >= ownersSaturation {
			return
		}
		if owners.CompareAndSwap(current, current-1) {
			return
		}
	}
}

// OwnersCount é introspecção para testes; -1 para não-compostos.
func OwnersCount(v Value) int32 {
	owners := ownersOf(v)
	if owners == nil {
		return -1
	}
	return owners.Load()
}
