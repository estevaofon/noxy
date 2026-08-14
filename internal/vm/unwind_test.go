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
