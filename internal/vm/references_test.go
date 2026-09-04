package vm

import (
	"noxy-vm/internal/value"
	"testing"
)

func TestResolveReferenceValueRejectsMalformedReferences(t *testing.T) {
	malformedArrayIndex := value.Value{
		Type: value.VAL_REF,
		Obj: &value.ObjRef{
			RefType:   value.REF_INDEX,
			Container: value.NewArray([]value.Value{value.NewInt(1)}),
			Index:     value.NewBool(false),
		},
	}
	malformedMapKey := value.Value{
		Type: value.VAL_REF,
		Obj: &value.ObjRef{
			RefType:   value.REF_INDEX,
			Container: value.NewMap(),
			Index:     value.Value{Type: value.VAL_OBJ, Obj: []int{1}},
		},
	}
	tests := []struct {
		name  string
		input value.Value
	}{
		{name: "wrong object", input: value.Value{Type: value.VAL_REF, Obj: "not a reference"}},
		{name: "nil reference", input: value.Value{Type: value.VAL_REF, Obj: (*value.ObjRef)(nil)}},
		{name: "nil upvalue", input: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_UPVALUE}}},
		{name: "nil upvalue location", input: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_UPVALUE, Upvalue: &value.ObjUpvalue{}}}},
		{name: "nil pointer", input: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR}}},
		{name: "array index type", input: malformedArrayIndex},
		{name: "unhashable map key", input: malformedMapKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New().resolveReferenceValue(tt.input); err == nil {
				t.Fatal("malformed reference resolved without error")
			}
		})
	}
}

func TestLookupReferenceValueRejectsNilReference(t *testing.T) {
	if _, err := New().lookupReferenceValue(nil); err == nil {
		t.Fatal("nil reference resolved without error")
	}
}

// Duas metades: append/delete continuam no no-op legado (devolvem null) e
// pop, desde a #126 item 4, propaga o erro — o nome cobre as duas.
func TestMutatingNativesOnMalformedReferences(t *testing.T) {
	machine := New()
	malformed := []struct {
		name  string
		value value.Value
	}{
		{name: "wrong object", value: value.Value{Type: value.VAL_REF, Obj: "not a reference"}},
		{
			name: "array index type",
			value: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{
				RefType:   value.REF_INDEX,
				Container: value.NewArray([]value.Value{value.NewInt(1)}),
				Index:     value.NewBool(false),
			}},
		},
		{
			name: "unhashable map key",
			value: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{
				RefType:   value.REF_INDEX,
				Container: value.NewMap(),
				Index:     value.Value{Type: value.VAL_OBJ, Obj: []int{1}},
			}},
		},
	}
	// append e delete continuam engolindo o erro de unicizeThroughRefValue
	// (no-op legado); pop, desde a #126 item 4, propaga o erro em vez de
	// devolver null (regra da #121) — verificado abaixo, fora deste loop.
	natives := []struct {
		name string
		args func(value.Value) []value.Value
	}{
		{name: "append", args: func(target value.Value) []value.Value { return []value.Value{target, value.NewInt(1)} }},
		{name: "delete", args: func(target value.Value) []value.Value { return []value.Value{target, value.NewString("key")} }},
	}

	for _, target := range malformed {
		for _, native := range natives {
			t.Run(target.name+"/"+native.name, func(t *testing.T) {
				nativeValue, ok := machine.GetGlobal(native.name)
				if !ok {
					t.Fatalf("missing native %s", native.name)
				}
				result, err := nativeValue.Obj.(*value.ObjNative).Invoke(machine, native.args(target.value))
				if err != nil {
					t.Fatal(err)
				}
				if result.Type != value.VAL_NULL {
					t.Fatalf("result=%v, want null", result)
				}
			})
		}
	}

	// Issue #126 item 4: pop sobre uma referencia malformada e erro de
	// runtime, nao mais o no-op legado que devolvia null.
	popNative, ok := machine.GetGlobal("pop")
	if !ok {
		t.Fatal("missing native pop")
	}
	for _, target := range malformed {
		t.Run(target.name+"/pop", func(t *testing.T) {
			_, err := popNative.Obj.(*value.ObjNative).Invoke(machine, []value.Value{target.value})
			if err == nil {
				t.Fatal("pop on malformed reference resolved without error")
			}
		})
	}
}

func TestGlobalReferenceStorageUsesExplicitOwner(t *testing.T) {
	environment := value.NewGlobalEnvironmentFrom(map[string]value.Value{"answer": value.NewInt(41)}, nil)
	ref := &value.ObjRef{RefType: value.REF_GLOBAL, Name: "answer", GlobalOwner: environment}
	machine := New()

	got, err := machine.lookupReferenceValue(ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != value.VAL_INT || got.Int() != 41 {
		t.Fatalf("lookup=%v, want 41", got)
	}
	if err := machine.storeReferenceValue(value.Value{Type: value.VAL_REF, Obj: ref}, value.NewInt(42)); err != nil {
		t.Fatal(err)
	}
	if got, _ := environment.GetLocal("answer"); got.Type != value.VAL_INT || got.Int() != 42 {
		t.Fatalf("stored value=%v, want 42", got)
	}

	for _, tt := range []struct {
		name string
		ref  *value.ObjRef
		want string
	}{
		{
			name: "nil owner",
			ref:  &value.ObjRef{RefType: value.REF_GLOBAL, Name: "answer"},
			want: "invalid global reference owner",
		},
		{
			name: "missing global",
			ref:  &value.ObjRef{RefType: value.REF_GLOBAL, Name: "missing", GlobalOwner: environment},
			want: "undefined global variable 'missing'",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := machine.lookupReferenceValue(tt.ref); err == nil || err.Error() != tt.want {
				t.Fatalf("lookup error=%v, want %q", err, tt.want)
			}
			if err := machine.storeReferenceValue(value.Value{Type: value.VAL_REF, Obj: tt.ref}, value.NewInt(42)); err == nil || err.Error() != tt.want {
				t.Fatalf("store error=%v, want %q", err, tt.want)
			}
		})
	}
}
