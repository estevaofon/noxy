package compiler

import (
	"strings"
	"testing"
)

func TestMutatingBuiltinsBorrowAddressableArguments(t *testing.T) {
	_, err := compileFunctionSource(t, `
let values: int[] = [1]
let mapping: map[string, int] = {"a": 1}
append(values, 2)
let removed: int = pop(values)
delete(mapping, "a")`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMutatingBuiltinsRejectNonAddressableArguments(t *testing.T) {
	_, err := compileFunctionSource(t, `append([1], 2)`)
	if err == nil || !strings.Contains(err.Error(), "not addressable") {
		t.Fatalf("error=%v", err)
	}
}

func TestAppendChecksElementType(t *testing.T) {
	_, err := compileFunctionSource(t, `
let values: int[] = [1]
append(values, "wrong")`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestMutatingBuiltinNamesCanBeShadowed(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "global function",
			source: `
func append(left: int, right: int) -> int
    return left + right
end
let answer: int = append(20, 22)`,
		},
		{
			name: "local function",
			source: `
func answer() -> int
    let append: func(int, int) -> int = func(left: int, right: int) -> int
        return left + right
    end
    return append(20, 22)
end`,
		},
		{
			name: "captured function",
			source: `
func make_answer() -> func() -> int
    let append: func(int, int) -> int = func(left: int, right: int) -> int
        return left + right
    end
    return func() -> int
        return append(20, 22)
    end
end`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := compileFunctionSource(t, tt.source); err != nil {
				t.Fatal(err)
			}
		})
	}
}
