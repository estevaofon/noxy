package vm

import (
	"errors"
	"slices"
	"testing"

	"noxy-vm/internal/value"
)

func TestDeferredCallsRunLIFOOnExplicitReturn(t *testing.T) {
	machine := New()
	var order []int64
	machine.DefineNative("record", func(args []value.Value) value.Value {
		order = append(order, args[0].AsInt)
		return value.NewNull()
	})
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		captured = args[0]
		return value.NewNull()
	})
	err := interpretVMSource(t, machine, `
func work() -> int
    defer record(1)
    defer record(2)
    return 7
end
test_report(work())`)
	if err != nil || !slices.Equal(order, []int64{2, 1}) || !valuesEqual(captured, value.NewInt(7)) {
		t.Fatalf("error=%v order=%v result=%v", err, order, captured)
	}
}

func TestDeferredCallsRunOnImplicitAndScriptReturn(t *testing.T) {
	cases := []struct {
		source string
		want   []int64
	}{
		{`func work() -> void
    defer record(1)
    record(0)
end
work()`, []int64{0, 1}},
		{`defer record(1)
defer record(2)
record(0)`, []int64{0, 2, 1}},
	}

	for _, test := range cases {
		machine := New()
		var order []int64
		machine.DefineNative("record", func(args []value.Value) value.Value {
			order = append(order, args[0].AsInt)
			return value.NewNull()
		})
		if err := interpretVMSource(t, machine, test.source); err != nil {
			t.Fatalf("source=%q error=%v", test.source, err)
		}
		if !slices.Equal(order, test.want) {
			t.Fatalf("source=%q order=%v want=%v", test.source, order, test.want)
		}
	}
}

func TestDeferredClosureReadsOwnerLocalBeforeUpvalueCloses(t *testing.T) {
	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		captured = args[0]
		return value.NewNull()
	})

	err := interpretVMSource(t, machine, `
func work() -> void
    let local: int = 42
    func cleanup() -> void
        test_report(local)
    end
    defer cleanup()
end
work()`)
	if err != nil || !valuesEqual(captured, value.NewInt(42)) {
		t.Fatalf("error=%v captured=%v, want 42", err, captured)
	}
	if machine.frameCount != 0 || machine.currentFrame != nil || machine.stackTop != 0 || machine.frames[0] != nil || machine.openUpvalues != nil {
		t.Fatalf("dirty terminal VM: frames=%d current=%p stack=%d open=%p", machine.frameCount, machine.currentFrame, machine.stackTop, machine.openUpvalues)
	}
}

func TestDeferredCallsRunAllAfterFailuresAndPreserveOrder(t *testing.T) {
	machine := New()
	firstFailure := errors.New("cleanup two failed")
	secondFailure := errors.New("cleanup three failed")
	var order []int64
	machine.DefineContextualNative("cleanup", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		id := args[0].AsInt
		order = append(order, id)
		switch id {
		case 2:
			return value.NewNull(), firstFailure
		case 3:
			return value.NewNull(), secondFailure
		default:
			return value.NewNull(), nil
		}
	})

	err := interpretVMSource(t, machine, `
defer cleanup(1)
defer cleanup(2)
defer cleanup(3)`)
	if !slices.Equal(order, []int64{3, 2, 1}) {
		t.Fatalf("order=%v, want [3 2 1]", order)
	}
	var unwind *UnwindError
	if !errors.As(err, &unwind) {
		t.Fatalf("error=%T %v, want *UnwindError", err, err)
	}
	if unwind.Primary != nil || len(unwind.Deferred) != 2 {
		t.Fatalf("unwind=%#v, want two deferred failures and no primary", unwind)
	}
	if !errors.Is(&unwind.Deferred[0], secondFailure) || !errors.Is(&unwind.Deferred[1], firstFailure) {
		t.Fatalf("deferred causes=%v, want cleanup three then cleanup two", unwind.Deferred)
	}
	if unwind.Deferred[0].Registration.Line != 4 || unwind.Deferred[1].Registration.Line != 3 {
		t.Fatalf("registration lines=%d,%d want 4,3", unwind.Deferred[0].Registration.Line, unwind.Deferred[1].Registration.Line)
	}
	if machine.frameCount != 0 || machine.currentFrame != nil || machine.stackTop != 0 || machine.frames[0] != nil {
		t.Fatalf("dirty terminal VM: frames=%d current=%p stack=%d", machine.frameCount, machine.currentFrame, machine.stackTop)
	}
}

func TestRuntimeErrorRunsAllDeferredCalls(t *testing.T) {
	machine := New()
	var order []int64
	machine.DefineNative("record", func(args []value.Value) value.Value {
		order = append(order, args[0].AsInt)
		return value.NewNull()
	})

	err := interpretVMSource(t, machine, `
func fail() -> void
    defer record(1)
    defer record(2)
    let zero: int = 0
    print(1 / zero)
end
fail()`)
	if err == nil || !slices.Equal(order, []int64{2, 1}) {
		t.Fatalf("error=%v order=%v", err, order)
	}
}

func TestCleanupFailuresAggregateAndDoNotSkipOlderEntries(t *testing.T) {
	machine := New()
	first := errors.New("first cleanup")
	second := errors.New("second cleanup")
	var completed bool
	machine.DefineContextualNative("fail_first", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), first
	})
	machine.DefineContextualNative("fail_second", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), second
	})
	machine.DefineNative("complete", func([]value.Value) value.Value {
		completed = true
		return value.NewNull()
	})

	err := interpretVMSource(t, machine, "defer complete()\ndefer fail_first()\ndefer fail_second()\n")
	var unwind *UnwindError
	if !errors.As(err, &unwind) || !errors.Is(err, first) || !errors.Is(err, second) || !completed || len(unwind.Deferred) != 2 {
		t.Fatalf("error=%v unwind=%#v completed=%v", err, unwind, completed)
	}
	if !errors.Is(&unwind.Deferred[0], second) || !errors.Is(&unwind.Deferred[1], first) {
		t.Fatalf("deferred=%v, want second then first", unwind.Deferred)
	}
}

func TestNormalReturnCleanupFailureUnwindsCallers(t *testing.T) {
	machine := New()
	first := errors.New("first cleanup")
	var completed bool
	machine.DefineContextualNative("fail_first", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), first
	})
	machine.DefineNative("complete", func([]value.Value) value.Value {
		completed = true
		return value.NewNull()
	})

	err := interpretVMSource(t, machine, `
func inner() -> void
    defer fail_first()
end
func outer() -> void
    defer complete()
    inner()
end
outer()`)
	if !errors.Is(err, first) || !completed {
		t.Fatalf("error=%v completed=%v", err, completed)
	}
}

func TestNestedCleanupFailurePreservesAggregate(t *testing.T) {
	machine := New()
	first := errors.New("first cleanup")
	second := errors.New("second cleanup")
	machine.DefineContextualNative("fail_first", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), first
	})
	machine.DefineContextualNative("fail_second", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), second
	})

	err := interpretVMSource(t, machine, `
func nested_cleanup() -> void
    defer fail_second()
    fail_first()
end
defer nested_cleanup()`)
	var outer *UnwindError
	if !errors.As(err, &outer) || outer.Primary != nil || len(outer.Deferred) != 1 {
		t.Fatalf("error=%v outer=%#v", err, outer)
	}
	inner, ok := outer.Deferred[0].Cause.(*UnwindError)
	if !ok || !errors.Is(inner.Primary, first) || len(inner.Deferred) != 1 || !errors.Is(&inner.Deferred[0], second) {
		t.Fatalf("outer cause=%T %#v, want nested aggregate", outer.Deferred[0].Cause, outer.Deferred[0].Cause)
	}
}

func TestVMReusableAfterRuntimeError(t *testing.T) {
	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		captured = args[0]
		return value.NewNull()
	})

	err := interpretVMSource(t, machine, `
func fail() -> void
    let local: int = 1
    func cleanup() -> void
        test_report(local)
    end
    defer cleanup()
    let zero: int = 0
    print(1 / zero)
end
fail()`)
	if err == nil {
		t.Fatal("runtime failure returned nil")
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
