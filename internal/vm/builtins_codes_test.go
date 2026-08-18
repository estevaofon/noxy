package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// codesOf calls the strings_codes native directly and returns the decoded
// codepoints as a plain slice.
func codesOf(t *testing.T, machine *VM, input value.Value) []int64 {
	t.Helper()
	got := callBuiltin(t, machine, "strings_codes", input)
	if got.Type != value.VAL_OBJ {
		t.Fatalf("strings_codes returned %#v, want an array", got)
	}
	array, ok := got.Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("strings_codes returned %#v, want an array", got)
	}
	out := make([]int64, 0, len(array.Elements))
	for i, element := range array.Elements {
		if element.Type != value.VAL_INT {
			t.Fatalf("element %d = %#v, want int", i, element)
		}
		out = append(out, element.AsInt)
	}
	return out
}

func equalCodes(a []int64, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCodesDecodesRunes(t *testing.T) {
	machine := New()
	tests := []struct {
		name  string
		input string
		want  []int64
	}{
		{name: "empty", input: "", want: []int64{}},
		{name: "ascii", input: "GET", want: []int64{71, 69, 84}},
		{name: "control characters", input: "a\r\n\x00b", want: []int64{97, 13, 10, 0, 98}},
		{name: "del", input: "a\x7fb", want: []int64{97, 127, 98}},
		{name: "multibyte", input: "café", want: []int64{99, 97, 102, 233}},
		{name: "astral", input: "\U0001F600", want: []int64{128512}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := codesOf(t, machine, value.NewString(test.input))
			if !equalCodes(got, test.want) {
				t.Fatalf("codes(%q) = %v, want %v", test.input, got, test.want)
			}
		})
	}
}

// codes is the linear alternative to calling char_at in a loop, which
// re-decodes the whole string on every call. It must therefore agree with
// char_at/ord exactly, or callers cannot migrate to it.
func TestCodesAgreesWithCharAtOrd(t *testing.T) {
	source := `use strings select *
let s: string = "caf" + from_char_code(233) + "-ok"
let cs: int[] = codes(s)
let n: int = length(s)
if length(cs) != n then
    test_report(false)
else
    let i: int = 0
    let same: bool = true
    while i < n do
        if cs[i] != char_code(char_at(s, i)) then
            same = false
        end
        i = i + 1
    end
    test_report(same)
end`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_BOOL || !captured.AsBool {
		t.Fatalf("codes disagrees with char_at/ord: %#v", captured)
	}
}

func TestCodesRejectsBytesArgument(t *testing.T) {
	source := `use strings select *
test_report(codes(b"raw"))`
	machine := New()
	machine.DefineNative("test_report", func([]value.Value) value.Value {
		return value.NewNull()
	})
	// Task 12 (§8): `use strings select *` now predeclares `codes` with its
	// declared type instead of erasing it to nil, so the mismatch is caught
	// at compile time instead of at runtime — same message, earlier stage.
	// interpretOrCompileErr accepts either.
	err := interpretOrCompileErr(t, machine, source)
	if err == nil {
		t.Fatal("codes(bytes) returned with no error; want a raised type error")
	}
	if !strings.Contains(err.Error(), "expected string, got bytes") {
		t.Fatalf("error = %q, want it to name the type mismatch", err.Error())
	}
}

func TestCodesRequiresExactlyOneArgument(t *testing.T) {
	machine := New()
	_, err := requireBuiltin(t, machine, "strings_codes").Invoke(machine, []value.Value{})
	if err == nil {
		t.Fatal("strings_codes() with no argument returned no error")
	}
}
