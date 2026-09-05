package vm

import (
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func TestScriptCallCopiesOnlyTopLevelCompositeArgument(t *testing.T) {
	got := captureVMSource(t, `
func change(values: int[]) -> int
    append(ref values, 9)
    return length(values)
end
let original: int[] = [1]
let local_length: int = change(original)
test_report(length(original) * 10 + local_length)`)
	testExpectedObject(t, 12, got)
}

// Contrato CoW (spec 2026-08-16): parâmetros sem ref são valores independentes
// em qualquer profundidade — mutação aninhada no callee não vaza mais para o
// chamador (antes desta mudança o esperado era 22, com o aninhado compartilhado).
func TestScriptCallKeepsNestedCompositeValuesIndependent(t *testing.T) {
	got := captureVMSource(t, `
func change(values: int[][]) -> int
    append(ref values[0], 9)
    return length(values[0])
end
let original: int[][] = [[1]]
let local_length: int = change(original)
test_report(length(original[0]) * 10 + local_length)`)
	testExpectedObject(t, 12, got)
}

func TestExactClosureCallReturnsValue(t *testing.T) {
	got := captureVMSource(t, `
func make_answer() -> func() -> int
    return func() -> int
        return 42
    end
end
let answer: func() -> int = make_answer()
test_report(answer())`)
	testExpectedObject(t, 42, got)
}

func TestBareFunctionDynamicCallReturnsValue(t *testing.T) {
	got := captureVMSource(t, `
func answer() -> int
    return 42
end
let dynamic: func = answer
test_report(dynamic())`)
	testExpectedObject(t, 42, got)
}

func TestStructConstructorCallBuildsInstance(t *testing.T) {
	got := captureVMSource(t, `
struct Box
    value: int
end
let box: Box = Box(42)
test_report(box.value)`)
	testExpectedObject(t, 42, got)
}

func TestNativeArityFailurePreservesStack(t *testing.T) {
	machine := New()
	called := false
	native := value.NewNativeWithSignature("needs_int", value.NativeSignature{
		Arity:      1,
		Params:     []value.ParamInfo{{TypeName: "int"}},
		ReturnType: "void",
	}, func(args []value.Value) value.Value {
		called = true
		return value.NewNull()
	})
	machine.push(native)
	before := machine.stackTop

	ok, err := machine.callValue(native, 0, nil, 0)
	if ok || err == nil {
		t.Fatalf("ok=%v error=%v, want native arity failure", ok, err)
	}
	if got, want := err.Error(), "[?:line 0] native 'needs_int' expects 1 arguments, got 0"; got != want {
		t.Fatalf("error=%q, want %q", got, want)
	}
	if called {
		t.Fatal("native ran after arity validation failed")
	}
	if machine.stackTop != before || machine.stack[0].Obj != native.Obj {
		t.Fatal("native validation failure mutated stack")
	}
}

func TestStructConstructorValidationFailurePreservesStack(t *testing.T) {
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	constructorType := &value.RuntimeTypeInfo{
		Kind:       value.TYPE_CALLABLE,
		Params:     []*value.RuntimeTypeInfo{integer},
		ParamIsRef: []bool{false},
		Return:     &value.RuntimeTypeInfo{Kind: value.TYPE_STRUCT, Name: "Box", Fields: map[string]*value.RuntimeTypeInfo{"value": integer}},
	}
	definition := &value.ObjStruct{Name: "Box", Fields: []string{"value"}, ConstructorType: constructorType}
	constructor := value.Value{Type: value.VAL_OBJ, Obj: definition}
	argument := value.NewString("wrong")
	machine := New()
	machine.push(constructor)
	machine.push(argument)
	before := machine.stackTop

	ok, err := machine.callValue(constructor, 1, nil, 0)
	if ok || err == nil {
		t.Fatalf("ok=%v error=%v, want constructor type failure", ok, err)
	}
	if got, want := err.Error(), "[?:line 0] function 'Box' argument 1: expected int, got object"; got != want {
		t.Fatalf("error=%q, want %q", got, want)
	}
	if machine.stackTop != before || machine.stack[0].Obj != definition || machine.stack[1].Obj != argument.Obj {
		t.Fatal("constructor validation failure mutated stack")
	}
}

func TestScriptArityFailurePreservesStack(t *testing.T) {
	closure := &value.ObjClosure{Function: &value.ObjFunction{Name: "answer", Arity: 1}}
	callee := value.Value{Type: value.VAL_FUNCTION, Obj: closure}
	machine := New()
	machine.push(callee)
	before := machine.stackTop

	ok, err := machine.callValue(callee, 0, nil, 0)
	if ok || err == nil {
		t.Fatalf("ok=%v error=%v, want script arity failure", ok, err)
	}
	if got, want := err.Error(), "[?:line 0] expected 1 arguments but got 0"; got != want {
		t.Fatalf("error=%q, want %q", got, want)
	}
	if machine.stackTop != before || machine.stack[0].Obj != closure {
		t.Fatal("script arity validation failure mutated stack")
	}
}
