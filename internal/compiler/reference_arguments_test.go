package compiler

import (
	"strings"
	"testing"
)

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
