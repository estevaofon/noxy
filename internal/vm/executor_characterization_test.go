package vm

import (
	"github.com/estevaofon/noxy/internal/value"
	"testing"
)

func TestExecutorCharacterization(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   value.Value
	}{
		{"integer modulo", `test_report(17 % 5)`, value.NewInt(2)},
		{"float arithmetic", `test_report((2.5 + 1.5) * 2.0)`, value.NewFloat(8)},
		{"bitwise", `test_report((6 & 3) | (1 << 3))`, value.NewInt(10)},
		{"short circuit true", `let called: bool = false
func side_effect() -> bool
    called = true
    return false
end
let result: bool = true || side_effect()
test_report(result && !called)`, value.NewBool(true)},
		{"short circuit false", `let called: bool = false
func side_effect() -> bool
    called = true
    return true
end
let result: bool = false && side_effect()
test_report(!result && !called)`, value.NewBool(true)},
		{"string concatenate", `test_report("no" + "xy")`, value.NewString("noxy")},
		{"bytes equality", `test_report(b"abc" == b"abc")`, value.NewBool(true)},
		{"mixed numeric equality", `test_report(42 == 42.0)`, value.NewBool(true)},
		{"loop and locals", `let total: int = 0
let i: int = 0
while i < 5 do
    total = total + i
    i = i + 1
end
test_report(total)`, value.NewInt(10)},
		{"array index update", `let values: int[] = [1, 2]
values[1] = 42
test_report(values[1])`, value.NewInt(42)},
		{"map index update", `let values: map[string, int] = {"x": 1}
values["x"] = 42
test_report(values["x"])`, value.NewInt(42)},
		{"struct property update", `struct Box
    value: int
end
let box: Box = Box(1)
box.value = 42
test_report(box.value)`, value.NewInt(42)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := captureVMSource(t, tt.source)
			if !valuesEqual(got, tt.want) {
				t.Fatalf("got %s (%v), want %s (%v)", got.String(), got.Type, tt.want.String(), tt.want.Type)
			}
		})
	}
}
