package vm

import (
	"errors"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

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
	constructor := value.NewStruct("Pair", []string{"left", "right"})
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

	prepared, err := machine.prepareDeferredCall(value.Value{Type: value.VAL_FUNCTION, Obj: closure}, []value.Value{array}, SourceLocation{})
	if err != nil || prepared.Arguments[0].Obj == array.Obj {
		t.Fatalf("closure did not shallow-copy array")
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

	constructor := value.NewStruct("Box", []string{"items"})
	prepared, err = machine.prepareDeferredCall(constructor, []value.Value{array}, SourceLocation{})
	if err != nil || prepared.Arguments[0].Obj != array.Obj {
		t.Fatalf("constructor capture changed identity")
	}

	if _, err = machine.prepareDeferredCall(constructor, nil, SourceLocation{}); err == nil {
		t.Fatal("constructor arity accepted")
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
