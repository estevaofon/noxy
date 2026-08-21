package vm

import (
	"errors"
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

func testStructConstructor(name string, fields []string, params []*value.RuntimeTypeInfo) value.Value {
	fieldTypes := make(map[string]*value.RuntimeTypeInfo, len(fields))
	for index, field := range fields {
		fieldTypes[field] = params[index]
	}
	constructor := value.NewStruct(name, fields)
	constructor.Obj.(*value.ObjStruct).ConstructorType = &value.RuntimeTypeInfo{
		Kind:       value.TYPE_CALLABLE,
		Params:     append([]*value.RuntimeTypeInfo(nil), params...),
		ParamIsRef: make([]bool, len(params)),
		Return: &value.RuntimeTypeInfo{
			Kind:   value.TYPE_STRUCT,
			Name:   name,
			Fields: fieldTypes,
		},
	}
	return constructor
}

func defineCleanupFailureNative(machine *VM, sentinel error) {
	machine.DefineContextualNative("cleanup_fail", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), sentinel
	})
}

func TestPrepareDeferredCallLegacyNativeKeepsValueIdentity(t *testing.T) {
	machine := New()
	array := value.NewArray([]value.Value{value.NewInt(1)})
	legacy := value.NewNative("legacy", func([]value.Value) value.Value { return value.NewNull() })
	prepared, err := machine.prepareDeferredCall(legacy, []value.Value{array}, SourceLocation{File: "test.nx", Line: 1})
	if err != nil || prepared.Arguments[0].Obj != array.Obj {
		t.Fatalf("prepared=%#v error=%v, want unchanged identity", prepared, err)
	}
}

func TestInvokePreparedCallDoesNotCopyArgumentsAgainAndRestoresStack(t *testing.T) {
	machine := New()
	array := value.NewArray([]value.Value{value.NewInt(1)})
	var received value.Value
	signed := value.NewNativeWithSignature("signed", value.NativeSignature{
		Arity:  1,
		Params: []value.ParamInfo{{TypeName: "int[]"}},
	}, func(args []value.Value) value.Value {
		received = args[0]
		return value.NewInt(9)
	})
	prepared, err := machine.prepareDeferredCall(signed, []value.Value{array}, SourceLocation{})
	if err != nil {
		t.Fatalf("prepare deferred call: %v", err)
	}
	machine.push(value.NewInt(42))
	base := machine.stackTop

	if err := machine.invokePreparedCall(prepared); err != nil {
		t.Fatalf("invoke prepared call: %v", err)
	}
	if received.Obj != prepared.Arguments[0].Obj {
		t.Fatal("prepared dispatch copied argument a second time")
	}
	if machine.stackTop != base || machine.stack[0].AsInt != 42 {
		t.Fatalf("stackTop=%d stack[0]=%v, want restored base %d", machine.stackTop, machine.stack[0], base)
	}
	for i := base; i < base+2; i++ {
		if machine.stack[i] != (value.Value{}) {
			t.Fatalf("temporary slot %d was not cleared: %#v", i, machine.stack[i])
		}
	}
}

func TestInvokePreparedConstructorClearsAllTemporarySlots(t *testing.T) {
	machine := New()
	anyType := &value.RuntimeTypeInfo{Kind: value.TYPE_ANY}
	constructor := testStructConstructor("Pair", []string{"left", "right"}, []*value.RuntimeTypeInfo{anyType, anyType})
	prepared, err := machine.prepareDeferredCall(constructor, []value.Value{
		value.NewArray([]value.Value{value.NewInt(1)}),
		value.NewArray([]value.Value{value.NewInt(2)}),
	}, SourceLocation{})
	if err != nil {
		t.Fatalf("prepare deferred constructor: %v", err)
	}
	machine.push(value.NewInt(42))
	base := machine.stackTop

	if err := machine.invokePreparedCall(prepared); err != nil {
		t.Fatalf("invoke prepared constructor: %v", err)
	}
	if machine.stackTop != base || machine.stack[0].AsInt != 42 {
		t.Fatalf("stackTop=%d stack[0]=%v, want restored base %d", machine.stackTop, machine.stack[0], base)
	}
	for i := base; i < base+3; i++ {
		if machine.stack[i] != (value.Value{}) {
			t.Fatalf("temporary slot %d was not cleared: %#v", i, machine.stack[i])
		}
	}
}

func TestInvokePreparedClosureClearsAllTemporarySlots(t *testing.T) {
	machine := New()
	body := chunk.New()
	body.Write(byte(chunk.OP_NULL), 1)
	body.Write(byte(chunk.OP_RETURN), 1)
	function := value.NewFunction("cleanup", 1, 0, []value.ParamInfo{{TypeName: "int"}}, body, machine.shared.Root)
	closure := &value.ObjClosure{Function: function.Obj.(*value.ObjFunction), Environment: machine.shared.Root}
	prepared, err := machine.prepareDeferredCall(value.Value{Type: value.VAL_FUNCTION, Obj: closure}, []value.Value{value.NewInt(7)}, SourceLocation{})
	if err != nil {
		t.Fatalf("prepare deferred closure: %v", err)
	}
	machine.push(value.NewInt(42))
	base := machine.stackTop

	if err := machine.invokePreparedCall(prepared); err != nil {
		t.Fatalf("invoke prepared closure: %v", err)
	}
	if machine.stackTop != base || machine.stack[0].AsInt != 42 {
		t.Fatalf("stackTop=%d stack[0]=%v, want restored base %d", machine.stackTop, machine.stack[0], base)
	}
	for i := base; i < base+2; i++ {
		if machine.stack[i] != (value.Value{}) {
			t.Fatalf("temporary slot %d was not cleared: %#v", i, machine.stack[i])
		}
	}
}

func TestInvokePreparedCallRestoresStackAfterNativeFailure(t *testing.T) {
	machine := New()
	wantErr := errors.New("cleanup failed")
	native := value.NewContextualNative("failing", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), wantErr
	})
	prepared, err := machine.prepareDeferredCall(native, []value.Value{value.NewArray([]value.Value{value.NewInt(1)})}, SourceLocation{})
	if err != nil {
		t.Fatalf("prepare deferred call: %v", err)
	}
	machine.push(value.NewInt(42))
	base := machine.stackTop

	err = machine.invokePreparedCall(prepared)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want wrapped cleanup failure", err)
	}
	if machine.stackTop != base || machine.stack[0].AsInt != 42 {
		t.Fatalf("stackTop=%d stack[0]=%v, want restored base %d", machine.stackTop, machine.stack[0], base)
	}
	for i := base; i < base+2; i++ {
		if machine.stack[i] != (value.Value{}) {
			t.Fatalf("temporary slot %d was not cleared: %#v", i, machine.stack[i])
		}
	}
}

func TestImmediateSignedNativeFailurePreservesOperandIdentity(t *testing.T) {
	machine := New()
	array := value.NewArray([]value.Value{value.NewInt(1)})
	native := value.NewContextualNativeWithSignature("failing", value.NativeSignature{
		Arity:  1,
		Params: []value.ParamInfo{{TypeName: "int[]"}},
	}, func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), errors.New("failed")
	})
	machine.push(native)
	machine.push(array)

	ok, err := machine.callValue(native, 1, nil, 0)
	if ok || err == nil {
		t.Fatalf("ok=%v error=%v, want native failure", ok, err)
	}
	if machine.stackTop != 2 || machine.stack[1].Obj != array.Obj {
		t.Fatalf("failed immediate call changed operand identity: %#v", machine.stack[1])
	}
}

func TestPrepareDeferredCallUsesCallableCaptureSemantics(t *testing.T) {
	machine := New()
	array := value.NewArray([]value.Value{value.NewInt(1)})
	closureValue := value.NewFunction("cleanup", 1, 0, []value.ParamInfo{{TypeName: "int[]"}}, chunk.New(), machine.shared.Root)
	closure := &value.ObjClosure{Function: closureValue.Obj.(*value.ObjFunction), Environment: machine.shared.Root}

	// Contrato CoW: closure defer captura por valor sem copiar — mesmo
	// ponteiro, cópia adiada para a primeira mutação (unicize). Depois da
	// chave (spec §3), o que registra a captura é o contador de donos, não o
	// bit sticky: aqui `array` nasce sem dono nenhum (montado fora do
	// bytecode), então a captura o leva de 0 para 1 — em código real o dono do
	// chamador já estaria contado e o total passaria de 1, disparando o CoW na
	// primeira mutação.
	ownersBefore := value.OwnersCount(array)
	prepared, err := machine.prepareDeferredCall(value.Value{Type: value.VAL_FUNCTION, Obj: closure}, []value.Value{array}, SourceLocation{})
	if err != nil || prepared.Arguments[0].Obj != array.Obj {
		t.Fatalf("closure defer deve capturar o arg composto sem copiar: err=%v", err)
	}
	if got := value.OwnersCount(prepared.Arguments[0]); got != ownersBefore+1 {
		t.Fatalf("captura do defer deve contar um dono durável: esperado %d, veio %d", ownersBefore+1, got)
	}

	reference := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_GLOBAL, Name: "items", GlobalOwner: machine.shared.Root}}
	closure.Function.Params[0].IsRef = true
	prepared, err = machine.prepareDeferredCall(value.Value{Type: value.VAL_FUNCTION, Obj: closure}, []value.Value{reference}, SourceLocation{})
	if err != nil || prepared.Arguments[0].Obj != reference.Obj {
		t.Fatalf("reference identity changed")
	}

	signature := value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int[]"}}}
	signed := value.NewNativeWithSignature("signed", signature, func([]value.Value) value.Value { return value.NewNull() })
	prepared, err = machine.prepareDeferredCall(signed, []value.Value{array}, SourceLocation{})
	if err != nil || prepared.Arguments[0].Obj == array.Obj {
		t.Fatalf("signed native did not shallow-copy array")
	}

	intType := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	constructor := testStructConstructor("Box", []string{"items"}, []*value.RuntimeTypeInfo{{Kind: value.TYPE_ARRAY, Element: intType}})
	prepared, err = machine.prepareDeferredCall(constructor, []value.Value{array}, SourceLocation{})
	if err != nil || prepared.Arguments[0].Obj != array.Obj {
		t.Fatalf("constructor capture changed identity")
	}

	if _, err = machine.prepareDeferredCall(constructor, nil, SourceLocation{}); err == nil {
		t.Fatal("constructor arity accepted")
	}
}

func TestPrepareDeferredCallRejectsStructWithoutRuntimeMetadataLikeImmediateCall(t *testing.T) {
	machine := New()
	constructor := value.NewStruct("Box", []string{"value"})
	argument := value.NewInt(1)

	_, deferredErr := machine.prepareDeferredCall(constructor, []value.Value{argument}, SourceLocation{})
	if deferredErr == nil {
		t.Fatal("deferred constructor accepted incomplete runtime type metadata")
	}

	machine.push(constructor)
	machine.push(argument)
	ok, immediateErr := machine.callValue(constructor, 1, nil, 0)
	if ok || immediateErr == nil {
		t.Fatalf("ok=%v error=%v, want immediate constructor validation failure", ok, immediateErr)
	}
	if !strings.Contains(immediateErr.Error(), deferredErr.Error()) {
		t.Fatalf("deferred error=%q immediate error=%q, want matching validation", deferredErr, immediateErr)
	}
}

func TestDeferredStructConstructorTypeErrorOccursAtRegistration(t *testing.T) {
	machine := New()
	olderRan := false
	machine.DefineNative("older_cleanup", func([]value.Value) value.Value {
		olderRan = true
		return value.NewNull()
	})

	err := interpretVMSource(t, machine, `struct Box
    value: int
end
let dynamic: func = Box
defer older_cleanup()
defer dynamic("wrong")
`)
	if err == nil {
		t.Fatal("deferred constructor accepted wrong field type")
	}
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("error=%T %[1]v, want RuntimeError", err)
	}
	if runtimeErr.Location.Line != 6 || runtimeErr.Cause == nil || !strings.Contains(runtimeErr.Cause.Error(), "expected int, got object") {
		t.Fatalf("runtime error=%#v, want registration-line constructor type error", runtimeErr)
	}
	if !olderRan {
		t.Fatal("registration failure skipped an older defer")
	}
}

func TestFinishFrameAggregatesPreparedCallHeadroomFailureAndContinues(t *testing.T) {
	machine := New()
	olderRan := false
	older := value.NewNative("older", func([]value.Value) value.Value {
		olderRan = true
		return value.NewNull()
	})
	newer := value.NewNative("newer", func([]value.Value) value.Value {
		t.Fatal("deferred call ran without enough fixed-stack headroom")
		return value.NewNull()
	})
	frame := &machine.frames[0]
	*frame = CallFrame{
		StackBase: 0,
		LocalBase: 0,
		Deferred: []PreparedCall{
			{Callee: older, Registration: SourceLocation{File: "headroom.nx", Line: 4}},
			{Callee: newer, Arguments: []value.Value{value.NewInt(1)}, Registration: SourceLocation{File: "headroom.nx", Line: 5}},
		},
	}
	machine.frameCount = 1
	machine.currentFrame = frame
	// Pilha no teto: ensureCallCapacity nao consegue a folga e a chamada
	// diferida falha com stack overflow (o cenario "sem headroom" do teste).
	machine.stack = make([]value.Value, StackMax)
	machine.stackTop = StackMax - 1
	machine.stack[StackMax-2] = value.NewInt(99)

	outcome := machine.finishFrame(frameOutcome{Result: value.NewNull()})
	var unwind *UnwindError
	if !errors.As(outcome.Err, &unwind) || len(unwind.Deferred) != 1 {
		t.Fatalf("error=%T %[1]v unwind=%#v, want one aggregated cleanup failure", outcome.Err, unwind)
	}
	deferred := unwind.Deferred[0]
	if deferred.Registration != (SourceLocation{File: "headroom.nx", Line: 5}) || deferred.Cause == nil || !strings.Contains(deferred.Cause.Error(), "stack overflow") {
		t.Fatalf("deferred=%#v, want registered headroom failure", deferred)
	}
	if !olderRan {
		t.Fatal("headroom failure skipped the older deferred call")
	}
	if machine.frameCount != 0 || machine.currentFrame != nil || machine.stackTop != 0 || machine.frames[0].Closure != nil || machine.frames[0].Environment != nil || len(machine.frames[0].Owned) != 0 || len(machine.frames[0].Deferred) != 0 || machine.openUpvalues != nil {
		t.Fatalf("dirty terminal VM: frames=%d current=%p stack=%d frame0=%p env=%p owned=%d deferred=%d open=%p", machine.frameCount, machine.currentFrame, machine.stackTop, machine.frames[0].Closure, machine.frames[0].Environment, len(machine.frames[0].Owned), len(machine.frames[0].Deferred), machine.openUpvalues)
	}
	if machine.stack[StackMax-2] != (value.Value{}) {
		t.Fatalf("owned stack slot was not cleared: %#v", machine.stack[StackMax-2])
	}
}

func TestScriptLocalBaseDoesNotCollideWithCalleeSlot(t *testing.T) {
	got := captureVMSource(t, `
if true then
    let local: int = 42
    test_report(local)
end`)
	if !valuesEqual(got, value.NewInt(42)) {
		t.Fatalf("local=%v, want 42", got)
	}
}

func TestVMReusableAfterDeferredRegistrationFailure(t *testing.T) {
	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		captured = args[0]
		return value.NewNull()
	})

	err := interpretVMSource(t, machine, `
func one_arg(value: int) -> void
end
let dynamic: func = one_arg
defer dynamic()`)
	if err == nil {
		t.Fatal("deferred registration accepted the wrong arity")
	}
	if machine.frameCount != 0 || machine.currentFrame != nil || machine.stackTop != 0 || machine.openUpvalues != nil {
		t.Fatalf("dirty VM after error: frames=%d current=%p stack=%d open=%p", machine.frameCount, machine.currentFrame, machine.stackTop, machine.openUpvalues)
	}
	if err := interpretVMSource(t, machine, "test_report(42)\n"); err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(captured, value.NewInt(42)) {
		t.Fatalf("reuse result=%v", captured)
	}
}
