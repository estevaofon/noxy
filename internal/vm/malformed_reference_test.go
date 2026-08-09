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
	moduleGlobals := map[string]value.Value{"present": value.NewInt(1)}
	tests := []struct {
		name      string
		malformed value.Value
	}{
		{name: "invalid kind", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.RefType(99)}}},
		{name: "nil pointer", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR}}},
		{name: "nil upvalue", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_UPVALUE}}},
		{name: "nil upvalue location", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_UPVALUE, Upvalue: &value.ObjUpvalue{}}}},
		{name: "nil global owner", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_GLOBAL, Name: "present"}}},
		{name: "missing global", malformed: value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_GLOBAL, Name: "missing", GlobalOwner: &moduleGlobals}}},
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
