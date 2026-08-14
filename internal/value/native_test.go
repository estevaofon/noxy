package value

import (
	"errors"
	"testing"
)

type testNativeContext struct{}

func (*testNativeContext) IsNativeContext() {}

func TestObjNativeInvokeSupportsLegacyAndContextualHandlers(t *testing.T) {
	ctx := &testNativeContext{}
	legacy := NewNative("legacy", func(args []Value) Value { return args[0] })
	got, err := legacy.Obj.(*ObjNative).Invoke(ctx, []Value{NewInt(7)})
	if err != nil || got.AsInt != 7 {
		t.Fatalf("legacy invoke=(%v, %v), want (7, nil)", got, err)
	}

	contextual := NewContextualNative("contextual", func(actual NativeContext, args []Value) (Value, error) {
		if actual != ctx {
			return NewNull(), errors.New("wrong context")
		}
		return args[0], nil
	})
	got, err = contextual.Obj.(*ObjNative).Invoke(ctx, []Value{NewInt(9)})
	if err != nil || got.AsInt != 9 {
		t.Fatalf("contextual invoke=(%v, %v), want (9, nil)", got, err)
	}
}

func TestObjNativeInvokeRejectsInvalidHandlerConfigurations(t *testing.T) {
	ctx := &testNativeContext{}
	tests := []*ObjNative{
		{Name: "missing"},
		{Name: "both", Fn: func([]Value) Value { return NewNull() }, Contextual: func(NativeContext, []Value) (Value, error) { return NewNull(), nil }},
	}
	for _, native := range tests {
		if native.IsCallable() {
			t.Fatalf("%s unexpectedly callable", native.Name)
		}
		if _, err := native.Invoke(ctx, nil); err == nil {
			t.Fatalf("%s invoke succeeded", native.Name)
		}
	}
}
