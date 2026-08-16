package value

import "fmt"

type NativeContext interface {
	IsNativeContext()
}

type NativeFunc func(args []Value) Value
type ContextualNativeFunc func(context NativeContext, args []Value) (Value, error)

type ObjNative struct {
	Name       string
	Fn         NativeFunc
	Contextual ContextualNativeFunc
	Signature  *NativeSignature
	// ReadonlyArgs: native auditado que não retém nem muta args — o CoW não
	// precisa marcar os compostos passados. Estampado no registro (hot path
	// de chamada lê o campo em vez de consultar mapa por nome).
	ReadonlyArgs bool
}

func (native *ObjNative) IsCallable() bool {
	return native != nil && (native.Fn == nil) != (native.Contextual == nil)
}

func (native *ObjNative) Invoke(context NativeContext, args []Value) (Value, error) {
	if native == nil {
		return NewNull(), fmt.Errorf("invalid native function")
	}
	if !native.IsCallable() {
		return NewNull(), fmt.Errorf("native '%s' must define exactly one handler", native.Name)
	}
	if native.Contextual != nil {
		if context == nil {
			return NewNull(), fmt.Errorf("native '%s' requires a runtime context", native.Name)
		}
		return native.Contextual(context, args)
	}
	return native.Fn(args), nil
}

func NewNative(name string, fn NativeFunc) Value {
	return Value{Type: VAL_NATIVE, Obj: &ObjNative{Name: name, Fn: fn}}
}

func NewNativeWithSignature(name string, signature NativeSignature, fn NativeFunc) Value {
	return Value{Type: VAL_NATIVE, Obj: &ObjNative{Name: name, Fn: fn, Signature: &signature}}
}

func NewContextualNative(name string, fn ContextualNativeFunc) Value {
	return Value{Type: VAL_NATIVE, Obj: &ObjNative{Name: name, Contextual: fn}}
}

func NewContextualNativeWithSignature(name string, signature NativeSignature, fn ContextualNativeFunc) Value {
	return Value{Type: VAL_NATIVE, Obj: &ObjNative{Name: name, Contextual: fn, Signature: &signature}}
}
