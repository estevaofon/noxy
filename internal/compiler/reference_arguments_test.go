package compiler

import (
	"noxy-vm/internal/chunk"
	"strings"
	"testing"
)

func TestReferenceArgumentValidatesArrayIndexType(t *testing.T) {
	_, err := compileFunctionSource(t, `
func set(target: ref int) -> void
    return
end
let values: int[] = [1]
set(ref values["zero"])`)
	if err == nil || !strings.Contains(err.Error(), "array reference index must be int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestReferenceArgumentValidatesMapKeyType(t *testing.T) {
	_, err := compileFunctionSource(t, `
func set(target: ref int) -> void
    return
end
let mapping: map[string, int] = {"value": 1}
set(ref mapping[0])`)
	if err == nil || !strings.Contains(err.Error(), "map reference key must be string, got int") {
		t.Fatalf("error=%v", err)
	}
}

func TestReferenceArgumentAcceptsReferenceReturningCall(t *testing.T) {
	_, err := compileFunctionSource(t, `
let value: int = 1
func get_reference() -> ref int
    return ref value
end
func set(target: ref int) -> void
    *target = 2
end
set(get_reference())`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReferenceArgumentRejectsOrdinaryCallResult(t *testing.T) {
	_, err := compileFunctionSource(t, `
func get_value() -> int
    return 1
end
func set(target: ref int) -> void
    return
end
set(ref get_value())`)
	if err == nil || !strings.Contains(err.Error(), "reference argument 'get_value()' is not addressable") {
		t.Fatalf("error=%v", err)
	}
}

// TestStoredRefSlotIsPassedAsValueNotReferenced e R1 (spec
// 2026-08-24-explicit-ref): um slot ja `ref T` (campo, elemento de array,
// valor de map) e passado para um parametro `ref T` como QUALQUER valor —
// sem `ref` no call site, e sem OP_CONTEXT_REF_PROPERTY/OP_CONTEXT_REF_INDEX
// (que so existiam para essa leitura contextual, agora eliminada; os
// opcodes e os cases da VM continuam existindo, so deixam de ser emitidos).
// `ref` sobre esse mesmo slot e erro ("ja e uma referencia").
func TestStoredRefSlotIsPassedAsValueNotReferenced(t *testing.T) {
	tests := []struct {
		name           string
		source         string
		forwardSource  string
		forwardSubject string
		forbidden      []chunk.OpCode
	}{
		{
			name: "property",
			source: `
struct Holder
    values: ref int[]
end
let values: int[] = [1]
let holder: Holder = Holder(ref values)
func consume(target: ref int[]) -> void
    return
end
consume(holder.values)`,
			forwardSource: `
struct Holder
    values: ref int[]
end
let values: int[] = [1]
let holder: Holder = Holder(ref values)
func consume(target: ref int[]) -> void
    return
end
consume(ref holder.values)`,
			forwardSubject: "holder.values",
			forbidden:      []chunk.OpCode{chunk.OP_CONTEXT_REF_PROPERTY},
		},
		{
			name: "array index",
			source: `
let values: int[] = [1]
let stored: (ref int[])[] = [ref values]
func consume(target: ref int[]) -> void
    return
end
consume(stored[0])`,
			forwardSource: `
let values: int[] = [1]
let stored: (ref int[])[] = [ref values]
func consume(target: ref int[]) -> void
    return
end
consume(ref stored[0])`,
			forwardSubject: "stored[0]",
			forbidden:      []chunk.OpCode{chunk.OP_CONTEXT_REF_INDEX},
		},
		{
			name: "map index",
			source: `
let values: int[] = [1]
let stored: map[string, ref int[]] = {"values": ref values}
func consume(target: ref int[]) -> void
    return
end
consume(stored["values"])`,
			forwardSource: `
let values: int[] = [1]
let stored: map[string, ref int[]] = {"values": ref values}
func consume(target: ref int[]) -> void
    return
end
consume(ref stored["values"])`,
			forwardSubject: "stored[values]",
			forbidden:      []chunk.OpCode{chunk.OP_CONTEXT_REF_INDEX},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler, err := compileFunctionSource(t, tt.source)
			if err != nil {
				t.Fatalf("passing the stored ref slot directly should compile: %v", err)
			}
			for _, op := range tt.forbidden {
				if containsOpcode(compiler.currentChunk.Code, op) {
					t.Fatalf("bytecode %v still emits %s — forwarding must read the slot as a plain value", compiler.currentChunk.Code, op)
				}
			}
			_, err = compileFunctionSource(t, tt.forwardSource)
			want := "'" + tt.forwardSubject + "' is already a reference"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error=%v, want %q", err, want)
			}
		})
	}
}

func TestReferenceArgumentStillBorrowsPlainPropertyAndIndexSlots(t *testing.T) {
	tests := []struct {
		name   string
		source string
		opcode chunk.OpCode
	}{
		{
			name: "property",
			source: `
struct Holder
    values: int[]
end
let holder: Holder = Holder([1])
append(holder.values, 2)`,
			opcode: chunk.OP_REF_PROPERTY,
		},
		{
			name: "index",
			source: `
let stored: int[][] = [[1]]
append(stored[0], 2)`,
			opcode: chunk.OP_REF_INDEX,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler, err := compileFunctionSource(t, tt.source)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, instruction := range compiler.currentChunk.Code {
				if instruction == byte(tt.opcode) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("bytecode %v does not contain %s", compiler.currentChunk.Code, tt.opcode)
			}
		})
	}
}
