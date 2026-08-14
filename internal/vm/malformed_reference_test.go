package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"strings"
	"testing"
)

func runMalformedReferenceProgram(t *testing.T, source string, malformed value.Value) (err error) {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.New().Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}

	machine := New()
	machine.DefineNative("malformed_ref", func(args []value.Value) value.Value {
		return malformed
	})
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("malformed reference caused Go panic: %v", recovered)
		}
	}()
	return machine.Interpret(code)
}

func TestMalformedNativeReferenceReadIsRuntimeError(t *testing.T) {
	err := runMalformedReferenceProgram(t, `
func read(value: ref int) -> int
    return value
end
let dynamic: func = read
dynamic(malformed_ref())`, value.Value{Type: value.VAL_REF, Obj: "not an ObjRef"})
	if err == nil || !strings.Contains(err.Error(), "invalid reference value") {
		t.Fatalf("error=%v", err)
	}
}

func TestMalformedNativeReferenceWriteIsRuntimeError(t *testing.T) {
	err := runMalformedReferenceProgram(t, `
func write(value: ref int) -> void
    *value = 42
end
let dynamic: func = write
dynamic(malformed_ref())`, value.Value{Type: value.VAL_REF, Obj: (*value.ObjRef)(nil)})
	if err == nil || !strings.Contains(err.Error(), "invalid reference value") {
		t.Fatalf("error=%v", err)
	}
}

func TestMalformedReferenceMetadataIsRuntimeErrorForReadAndWrite(t *testing.T) {
	moduleEnvironment := value.NewGlobalEnvironmentFrom(map[string]value.Value{"present": value.NewInt(1)}, nil)
	tests := []struct {
		name      string
		malformed value.Value
	}{
		{name: "invalid kind", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.RefType(99)}}},
		{name: "nil pointer", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR}}},
		{name: "nil upvalue", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_UPVALUE}}},
		{name: "nil upvalue location", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_UPVALUE, Upvalue: &value.ObjUpvalue{}}}},
		{name: "nil global owner", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_GLOBAL, Name: "present"}}},
		{name: "missing global", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_GLOBAL, Name: "missing", GlobalOwner: moduleEnvironment}}},
		{name: "property container", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PROPERTY, Container: value.NewInt(1), Name: "field"}}},
		{name: "array index type", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_INDEX, Container: value.NewArray([]value.Value{value.NewInt(1)}), Index: value.NewBool(false)}}},
		{name: "array bounds", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_INDEX, Container: value.NewArray([]value.Value{value.NewInt(1)}), Index: value.NewInt(2)}}},
		{name: "map key", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_INDEX, Container: value.NewMap(), Index: value.Value{Type: value.VAL_OBJ, Obj: []int{1}}}}},
		{name: "index container", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_INDEX, Container: value.NewInt(1), Index: value.NewInt(0)}}},
	}

	readSource := `
func read(value: ref int) -> int
    return value
end
let dynamic: func = read
dynamic(malformed_ref())`
	writeSource := `
func write(value: ref int) -> void
    *value = 42
end
let dynamic: func = write
dynamic(malformed_ref())`

	for _, tt := range tests {
		for _, operation := range []struct {
			name   string
			source string
		}{
			{name: "read", source: readSource},
			{name: "write", source: writeSource},
		} {
			t.Run(tt.name+"/"+operation.name, func(t *testing.T) {
				err := runMalformedReferenceProgram(t, operation.source, tt.malformed)
				if err == nil {
					t.Fatal("malformed reference completed without runtime error")
				}
			})
		}
	}
}

func TestMalformedStoredPropertyReferenceWriteIsRuntimeError(t *testing.T) {
	code := chunk.New()
	code.FileName = "malformed_property_reference"
	holder := value.NewInstance(&value.ObjStruct{Name: "Holder", Fields: []string{"pointer"}})
	holder.Obj.(*value.ObjInstance).Fields["pointer"] = value.Value{Type: value.VAL_REF, Obj: "not an ObjRef"}
	holderConstant := code.AddConstant(holder)
	valueConstant := code.AddConstant(value.NewInt(42))
	propertyConstant := code.AddConstant(value.NewString("pointer"))
	for _, instruction := range []byte{
		byte(chunk.OP_CONSTANT), byte(holderConstant),
		byte(chunk.OP_CONSTANT), byte(valueConstant),
		byte(chunk.OP_SET_PROPERTY_DEREF), byte(propertyConstant),
		byte(chunk.OP_RETURN),
	} {
		code.Write(instruction, 1)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("malformed property reference caused Go panic: %v", recovered)
		}
	}()
	err := New().Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "invalid reference value") {
		t.Fatalf("error=%v", err)
	}
}

func TestMalformedReferenceStoreViaLocalOpcodeIsRuntimeError(t *testing.T) {
	code := chunk.New()
	code.FileName = "malformed_store_via_ref"
	refConstant := code.AddConstant(value.Value{Type: value.VAL_REF, Obj: "not an ObjRef"})
	valueConstant := code.AddConstant(value.NewInt(42))
	for _, instruction := range []byte{
		byte(chunk.OP_CONSTANT), byte(refConstant),
		byte(chunk.OP_CONSTANT), byte(valueConstant),
		byte(chunk.OP_STORE_VIA_REF), 0,
		byte(chunk.OP_NULL),
		byte(chunk.OP_RETURN),
	} {
		code.Write(instruction, 1)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("malformed OP_STORE_VIA_REF caused Go panic: %v", recovered)
		}
	}()
	err := New().Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "invalid reference value") {
		t.Fatalf("error=%v", err)
	}
}

func TestMalformedReferencePrintIsPanicSafe(t *testing.T) {
	err := runMalformedReferenceProgram(t, `print(malformed_ref())`, value.Value{Type: value.VAL_REF, Obj: "not an ObjRef"})
	if err != nil {
		t.Fatalf("print returned unexpected error: %v", err)
	}
}

func TestMalformedReferenceStringificationIsPanicSafe(t *testing.T) {
	tests := []struct {
		name  string
		input value.Value
	}{
		{name: "wrong object", input: value.Value{Type: value.VAL_REF, Obj: "not an ObjRef"}},
		{name: "typed nil", input: value.Value{Type: value.VAL_REF, Obj: (*value.ObjRef)(nil)}},
		{name: "typed nil index metadata", input: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{
			RefType:   value.REF_INDEX,
			Container: value.NewArray(nil),
			Index:     value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjArray)(nil)},
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("malformed reference String panicked: %v", recovered)
				}
			}()
			if got := tt.input.String(); got != "<invalid reference>" {
				t.Fatalf("String()=%q, want invalid reference marker", got)
			}
		})
	}

	t.Run("nil ObjRef receiver", func(t *testing.T) {
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("nil ObjRef.String panicked: %v", recovered)
			}
		}()
		var nilRef *value.ObjRef
		if got := nilRef.String(); got != "<invalid reference>" {
			t.Fatalf("nil ObjRef.String()=%q, want invalid reference marker", got)
		}
	})
}

func TestReferencesToTypedNilObjectsAreRuntimeErrors(t *testing.T) {
	tests := []struct {
		name   string
		stored value.Value
	}{
		{name: "array", stored: value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjArray)(nil)}},
		{name: "map", stored: value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjMap)(nil)}},
		{name: "struct", stored: value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjStruct)(nil)}},
		{name: "instance", stored: value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjInstance)(nil)}},
		{name: "function", stored: value.Value{Type: value.VAL_FUNCTION, Obj: (*value.ObjFunction)(nil)}},
		{name: "closure", stored: value.Value{Type: value.VAL_FUNCTION, Obj: (*value.ObjClosure)(nil)}},
		{name: "native", stored: value.Value{Type: value.VAL_NATIVE, Obj: (*value.ObjNative)(nil)}},
		{name: "bytes", stored: value.Value{Type: value.VAL_BYTES, Obj: (*string)(nil)}},
		{name: "channel", stored: value.Value{Type: value.VAL_CHANNEL, Obj: (*value.ObjChannel)(nil)}},
		{name: "waitgroup", stored: value.Value{Type: value.VAL_WAITGROUP, Obj: (*value.ObjWaitGroup)(nil)}},
		{name: "task", stored: value.Value{Type: value.VAL_TASK, Obj: (*value.ObjTask)(nil)}},
		{name: "reference", stored: value.Value{Type: value.VAL_REF, Obj: (*value.ObjRef)(nil)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := tt.stored
			input := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}}
			if _, err := New().resolveReferenceValue(input); err == nil {
				t.Fatal("typed-nil referenced object resolved without runtime error")
			}
		})
	}
}

func TestMalformedReferenceRejectsTaskPayload(t *testing.T) {
	tests := []struct {
		name   string
		stored value.Value
	}{
		{name: "nil", stored: value.Value{Type: value.VAL_TASK}},
		{name: "wrong type", stored: value.Value{Type: value.VAL_TASK, Obj: "not a task"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateReferencedValue(tt.stored); err == nil {
				t.Fatal("malformed task payload was accepted")
			}
		})
	}
}

func TestReferencesPreserveValidObjectBackedAndScalarValues(t *testing.T) {
	storedInt := value.NewInt(7)
	validRef := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR, Ptr: &storedInt}}
	tests := []struct {
		name   string
		stored value.Value
	}{
		{name: "string object", stored: value.NewString("value")},
		{name: "array", stored: value.NewArray(nil)},
		{name: "map", stored: value.NewMap()},
		{name: "instance", stored: value.NewInstance(&value.ObjStruct{Name: "Holder"})},
		{name: "function", stored: value.NewFunction("f", 0, 0, nil, nil, nil)},
		{name: "native", stored: value.NewNative("n", func(args []value.Value) value.Value { return value.NewNull() })},
		{name: "bytes", stored: value.NewBytes("ok")},
		{name: "channel", stored: value.NewChannel(1)},
		{name: "waitgroup", stored: value.NewWaitGroup()},
		{name: "reference", stored: validRef},
		{name: "null", stored: value.NewNull()},
		{name: "bool", stored: value.NewBool(true)},
		{name: "int", stored: value.NewInt(42)},
		{name: "float", stored: value.NewFloat(3.5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := tt.stored
			input := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}}
			got, err := New().resolveReferenceValue(input)
			if err != nil {
				t.Fatalf("valid referenced value rejected: %v", err)
			}
			if got.Type != tt.stored.Type {
				t.Fatalf("type=%v, want %v", got.Type, tt.stored.Type)
			}
		})
	}
}

func TestContextualReferenceOpcodesRejectTypedNilContainers(t *testing.T) {
	arrayIndex := value.NewInt(0)
	mapIndex := value.NewString("key")
	tests := []struct {
		name       string
		container  value.Value
		indexValue *value.Value
	}{
		{name: "property", container: value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjInstance)(nil)}},
		{name: "array index", container: value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjArray)(nil)}, indexValue: &arrayIndex},
		{name: "map index", container: value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjMap)(nil)}, indexValue: &mapIndex},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := chunk.New()
			code.FileName = "typed_nil_context_reference"
			containerConstant := code.AddConstant(tt.container)
			code.Write(byte(chunk.OP_CONSTANT), 1)
			code.Write(byte(containerConstant), 1)
			if tt.indexValue == nil {
				nameConstant := code.AddConstant(value.NewString("field"))
				code.Write(byte(chunk.OP_CONTEXT_REF_PROPERTY), 1)
				code.Write(byte(nameConstant), 1)
			} else {
				indexConstant := code.AddConstant(*tt.indexValue)
				code.Write(byte(chunk.OP_CONSTANT), 1)
				code.Write(byte(indexConstant), 1)
				code.Write(byte(chunk.OP_CONTEXT_REF_INDEX), 1)
			}
			code.Write(byte(chunk.OP_RETURN), 1)

			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("typed-nil contextual reference caused Go panic: %v", recovered)
				}
			}()
			if err := New().Interpret(code); err == nil {
				t.Fatal("typed-nil contextual reference completed without runtime error")
			}
		})
	}
}
