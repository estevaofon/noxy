package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
	"testing"
)

func TestLegacyConstantReaders(t *testing.T) {
	machine := New()
	machine.chunk = chunk.New()
	machine.chunk.Constants = []value.Value{value.NewInt(7), value.NewInt(42)}
	machine.chunk.Code = []byte{0, 1, 0, 1}
	machine.ip = 0
	if got := machine.readConstant(); got.AsInt != 7 {
		t.Fatalf("readConstant=%v", got)
	}
	machine.ip = 2
	if got := machine.readShort(); got != 1 {
		t.Fatalf("readShort=%d, want 1", got)
	}
}

func TestFalseyAndEqualityMatrix(t *testing.T) {
	if !isFalsey(value.NewNull()) || !isFalsey(value.NewBool(false)) || isFalsey(value.NewBool(true)) {
		t.Fatal("falsey semantics changed")
	}
	pairs := []struct {
		left, right value.Value
		want        bool
	}{
		{value.NewNull(), value.NewNull(), true},
		{value.NewInt(42), value.NewFloat(42), true},
		{value.NewFloat(42), value.NewInt(42), true},
		{value.NewBytes("x"), value.NewBytes("x"), true},
		{value.NewString("x"), value.NewString("x"), true},
		{value.NewInt(1), value.NewBool(true), false},
	}
	for _, pair := range pairs {
		if got := valuesEqual(pair.left, pair.right); got != pair.want {
			t.Fatalf("valuesEqual(%v, %v)=%v, want %v", pair.left, pair.right, got, pair.want)
		}
	}
}

func TestStackLIFOAndSlotClearing(t *testing.T) {
	machine := New()
	first := value.NewInt(7)
	second := value.NewString("top")

	machine.push(first)
	machine.push(second)
	if machine.stackTop != 2 {
		t.Fatalf("stackTop=%d, want 2", machine.stackTop)
	}
	if got := machine.peek(0); !valuesEqual(got, second) {
		t.Fatalf("peek(0)=%v, want %v", got, second)
	}
	if got := machine.peek(1); !valuesEqual(got, first) {
		t.Fatalf("peek(1)=%v, want %v", got, first)
	}
	if got := machine.pop(); !valuesEqual(got, second) {
		t.Fatalf("first pop=%v, want %v", got, second)
	}
	if machine.stackTop != 1 {
		t.Fatalf("stackTop=%d after first pop, want 1", machine.stackTop)
	}
	if got := machine.stack[1]; got != (value.Value{}) {
		t.Fatalf("popped stack slot retained value: %#v", got)
	}
	if got := machine.pop(); !valuesEqual(got, first) {
		t.Fatalf("second pop=%v, want %v", got, first)
	}
	if machine.stackTop != 0 {
		t.Fatalf("stackTop=%d after second pop, want 0", machine.stackTop)
	}
}

func TestUpvalueCaptureAndClose(t *testing.T) {
	machine := New()
	machine.stack[0] = value.NewInt(7)
	slot := &machine.stack[0]

	first := machine.captureUpvalue(slot)
	second := machine.captureUpvalue(slot)
	if first != second {
		t.Fatal("capturing the same slot returned distinct upvalues")
	}

	// slot possuido pelo frame (true): a posse migra do slot para o box.
	machine.closeUpvalue(slot, true)
	machine.stack[0] = value.NewInt(42)
	if first.PointsTo(slot) {
		t.Fatal("closed upvalue still points to its former stack slot")
	}
	got, ok := first.Load()
	if !ok || !valuesEqual(got, value.NewInt(7)) {
		t.Fatalf("closed value=%v valid=%v, want 7", got, ok)
	}
	if !first.Store(value.NewInt(9)) {
		t.Fatal("closed upvalue rejected a store")
	}
	got, ok = first.Load()
	if !ok || !valuesEqual(got, value.NewInt(9)) {
		t.Fatalf("stored closed value=%v valid=%v, want 9", got, ok)
	}
	if got := machine.stack[0]; !valuesEqual(got, value.NewInt(42)) {
		t.Fatalf("closed upvalue store mutated former stack slot: %v", got)
	}
	if machine.openUpvalues != nil {
		t.Fatal("closed upvalue remained in the open list")
	}
}
