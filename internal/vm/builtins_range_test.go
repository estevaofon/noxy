package vm

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func intValues(numbers ...int64) []value.Value {
	values := make([]value.Value, len(numbers))
	for i, n := range numbers {
		values[i] = value.NewInt(n)
	}
	return values
}

func TestRangeBuiltinProducesPythonStyleSequences(t *testing.T) {
	cases := []struct {
		call string
		want []value.Value
	}{
		{"range(5)", intValues(0, 1, 2, 3, 4)},
		{"range(2, 5)", intValues(2, 3, 4)},
		{"range(0, 10, 3)", intValues(0, 3, 6, 9)},
		{"range(10, 0, -3)", intValues(10, 7, 4, 1)},
		{"range(0)", intValues()},
		{"range(-3)", intValues()},
		{"range(5, 5)", intValues()},
		{"range(3, 0)", intValues()},
		{"range(0, 3, -1)", intValues()},
		{"range(-6, -1, 2)", intValues(-6, -4, -2)},
	}
	for _, tc := range cases {
		t.Run(tc.call, func(t *testing.T) {
			got := runTypedFunctionProgram(t, "test_report("+tc.call+")")
			assertBuiltinArray(t, got, tc.want)
		})
	}
}

func TestRangeBuiltinIsAvailableWithoutImport(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let total: int = 0
for i in range(1, 4) do
    total = total + i
end
test_report(total)`)
	testExpectedObject(t, 6, got)
}

func TestRangeBuiltinRejectsZeroStep(t *testing.T) {
	err := runTypedFunctionProgramError(t, `let xs: int[] = range(0, 5, 0)`)
	if err == nil || !strings.Contains(err.Error(), "range: step must not be zero") {
		t.Fatalf("error=%v", err)
	}
}

func TestRangeBuiltinRejectsNonIntegerArgumentsAtRuntime(t *testing.T) {
	// O compilador ja recusa tipos estaticos errados; o native ainda valida
	// por conta propria (chamada dinamica, plugin, valor `any` em runtime).
	machine := New()
	native := requireBuiltin(t, machine, "range")
	for _, args := range [][]value.Value{
		{value.NewString("3")},
		{value.NewInt(0), value.NewFloat(2.5)},
		{value.NewInt(0), value.NewInt(5), value.NewBool(true)},
	} {
		if _, err := native.Invoke(machine, args); err == nil || !strings.Contains(err.Error(), "range: expects int arguments") {
			t.Fatalf("args=%v: error=%v", args, err)
		}
	}
	if _, err := native.Invoke(machine, nil); err == nil || !strings.Contains(err.Error(), "range: expects 1 to 3 arguments, got 0") {
		t.Fatalf("no args: error=%v", err)
	}
}

func TestRangeBuiltinRejectsSequencesThatCannotBeAllocated(t *testing.T) {
	// int64 inteiro: o native nao pode deixar o make() panicar com
	// "len out of range" (o panic atravessa a VM e nao tem linha do script).
	machine := New()
	native := requireBuiltin(t, machine, "range")
	if _, err := native.Invoke(machine, []value.Value{value.NewInt(-1 << 63), value.NewInt(1<<63 - 1)}); err == nil || !strings.Contains(err.Error(), "range: sequence too large") {
		t.Fatalf("error=%v", err)
	}
}
