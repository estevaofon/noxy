package compiler

import (
	"noxy-vm/internal/chunk"
	"strings"
	"testing"
)

func containsBytecodePattern(code []byte, pattern []int) bool {
	for start := 0; start+len(pattern) <= len(code); start++ {
		matches := true
		for offset, expected := range pattern {
			if expected >= 0 && code[start+offset] != byte(expected) {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func TestReferenceArgumentValidatesArrayIndexType(t *testing.T) {
	_, err := compileFunctionSource(t, `
func set(target: ref int) -> void
    return
end
let values: int[] = [1]
set(values["zero"])`)
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
set(mapping[0])`)
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
set(get_value())`)
	if err == nil || !strings.Contains(err.Error(), "reference argument 'get_value()' is not addressable") {
		t.Fatalf("error=%v", err)
	}
}

func TestReferenceArgumentLoadsStoredReferencesFromPropertiesAndIndexes(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		pattern []int
	}{
		{
			name: "property",
			source: `
struct Holder
    values: ref int[]
end
let values: int[] = [1]
let holder: Holder = Holder(ref values)
append(holder.values, 2)`,
			pattern: []int{int(chunk.OP_GET_GLOBAL), -1, int(chunk.OP_GET_GLOBAL), -1, int(chunk.OP_CONTEXT_REF_PROPERTY), -1, int(chunk.OP_MARK_REF_TARGET_TYPE), -1, -1, int(chunk.OP_CONSTANT), -1, int(chunk.OP_CALL), 2},
		},
		{
			name: "array index",
			source: `
let values: int[] = [1]
let stored: (ref int[])[] = [ref values]
append(stored[0], 2)`,
			pattern: []int{int(chunk.OP_GET_GLOBAL), -1, int(chunk.OP_GET_GLOBAL), -1, int(chunk.OP_CONSTANT), -1, int(chunk.OP_CONTEXT_REF_INDEX), int(chunk.OP_MARK_REF_TARGET_TYPE), -1, -1, int(chunk.OP_CONSTANT), -1, int(chunk.OP_CALL), 2},
		},
		{
			name: "map index",
			source: `
let values: int[] = [1]
let stored: map[string, ref int[]] = {"values": ref values}
append(stored["values"], 2)`,
			pattern: []int{int(chunk.OP_GET_GLOBAL), -1, int(chunk.OP_GET_GLOBAL), -1, int(chunk.OP_CONSTANT), -1, int(chunk.OP_CONTEXT_REF_INDEX), int(chunk.OP_MARK_REF_TARGET_TYPE), -1, -1, int(chunk.OP_CONSTANT), -1, int(chunk.OP_CALL), 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler, err := compileFunctionSource(t, tt.source)
			if err != nil {
				t.Fatal(err)
			}
			if !containsBytecodePattern(compiler.currentChunk.Code, tt.pattern) {
				t.Fatalf("bytecode %v does not contain stored-reference load pattern %v", compiler.currentChunk.Code, tt.pattern)
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
