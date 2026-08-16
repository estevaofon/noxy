package value

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
