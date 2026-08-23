package vm

import (
	"io"
	"os"
	"testing"

	"noxy-vm/internal/value"
)

func assertBuiltinValue(t *testing.T, got, want value.Value) {
	t.Helper()
	if got.Type != want.Type {
		t.Fatalf("type = %v, want %v (value %q)", got.Type, want.Type, got.String())
	}
	switch want.Type {
	case value.VAL_BOOL:
		if got.Bool() != want.Bool() {
			t.Fatalf("bool = %t, want %t", got.Bool(), want.Bool())
		}
	case value.VAL_INT:
		if got.Int() != want.Int() {
			t.Fatalf("int = %d, want %d", got.Int(), want.Int())
		}
	case value.VAL_FLOAT:
		if got.Float() != want.Float() {
			t.Fatalf("float = %v, want %v", got.Float(), want.Float())
		}
	case value.VAL_OBJ, value.VAL_BYTES:
		gotString, gotOK := got.Obj.(string)
		wantString, wantOK := want.Obj.(string)
		if !gotOK || !wantOK {
			t.Fatalf("object payloads are not strings: got %#v, want %#v", got.Obj, want.Obj)
		}
		if gotString != wantString {
			t.Fatalf("payload = %q, want %q", gotString, wantString)
		}
	case value.VAL_NULL:
		if got.Obj != nil {
			t.Fatalf("null payload = %#v, want nil", got.Obj)
		}
	default:
		t.Fatalf("assertBuiltinValue does not support value type %v", want.Type)
	}
}

func TestConversionBuiltins(t *testing.T) {
	machine := New()
	tests := []struct {
		name    string
		builtin string
		args    []value.Value
		want    value.Value
	}{
		{name: "to_str null", builtin: "to_str", args: []value.Value{value.NewNull()}, want: value.NewString("null")},
		{name: "to_str bool", builtin: "to_str", args: []value.Value{value.NewBool(true)}, want: value.NewString("true")},
		{name: "to_str int", builtin: "to_str", args: []value.Value{value.NewInt(-42)}, want: value.NewString("-42")},
		// issue #66 item 2: escalares saem sem requireValidUTF8 e int sem fmt
		{name: "to_str int zero", builtin: "to_str", args: []value.Value{value.NewInt(0)}, want: value.NewString("0")},
		{name: "to_str int min", builtin: "to_str", args: []value.Value{value.NewInt(-9223372036854775808)}, want: value.NewString("-9223372036854775808")},
		{name: "to_str float negative", builtin: "to_str", args: []value.Value{value.NewFloat(-0.5)}, want: value.NewString("-0.500000")},
		{name: "to_str bool false", builtin: "to_str", args: []value.Value{value.NewBool(false)}, want: value.NewString("false")},
		{name: "to_str float", builtin: "to_str", args: []value.Value{value.NewFloat(3.5)}, want: value.NewString("3.500000")},
		{name: "to_str string", builtin: "to_str", args: []value.Value{value.NewString("noxy")}, want: value.NewString("noxy")},
		// to_str/to_int/to_float now raise on bad arity instead of returning
		// a sentinel value ("" / 0); those cases are covered by
		// TestToIntRaisesOnUnconvertibleInput, TestToFloatRaisesOnUnconvertibleInput,
		// and TestStrictConversionRaisesOnBadArity in builtins_convert_test.go.
		// to_str also now raises on invalid UTF-8 bytes; see
		// TestToStrValidatesUTF8 in builtins_convert_test.go.
		{name: "to_int int", builtin: "to_int", args: []value.Value{value.NewInt(-42)}, want: value.NewInt(-42)},
		{name: "to_int float truncates", builtin: "to_int", args: []value.Value{value.NewFloat(3.9)}, want: value.NewInt(3)},
		{name: "to_int integer string", builtin: "to_int", args: []value.Value{value.NewString("123")}, want: value.NewInt(123)},
		{name: "to_float int", builtin: "to_float", args: []value.Value{value.NewInt(-42)}, want: value.NewFloat(-42)},
		{name: "to_float float", builtin: "to_float", args: []value.Value{value.NewFloat(3.5)}, want: value.NewFloat(3.5)},
		{name: "to_float string", builtin: "to_float", args: []value.Value{value.NewString("12.75")}, want: value.NewFloat(12.75)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, tt.builtin, tt.args...), tt.want)
		})
	}
}

func TestEncodingBuiltins(t *testing.T) {
	machine := New()
	tests := []struct {
		name    string
		builtin string
		args    []value.Value
		want    value.Value
	}{
		{name: "hex int", builtin: "hex", args: []value.Value{value.NewInt(255)}, want: value.NewString("0xff")},
		{name: "hex bytes", builtin: "hex", args: []value.Value{value.NewBytes("\x00\xff")}, want: value.NewString("00ff")},
		{name: "hex other value", builtin: "hex", args: []value.Value{value.NewString("go")}, want: value.NewString("go")},
		{name: "hex short args", builtin: "hex", want: value.NewNull()},
		{name: "hex encode string", builtin: "hex_encode", args: []value.Value{value.NewString("Go")}, want: value.NewString("476f")},
		{name: "hex encode bytes", builtin: "hex_encode", args: []value.Value{value.NewBytes("\x00\xff")}, want: value.NewString("00ff")},
		{name: "hex encode short args", builtin: "hex_encode", want: value.NewString("")},
		{name: "hex decode", builtin: "hex_decode", args: []value.Value{value.NewString("476f")}, want: value.NewBytes("Go")},
		{name: "hex decode invalid sentinel", builtin: "hex_decode", args: []value.Value{value.NewString("xyz")}, want: value.NewBytes("")},
		{name: "hex decode short args", builtin: "hex_decode", want: value.NewBytes("")},
		{name: "base64 encode string", builtin: "base64_encode", args: []value.Value{value.NewString("Noxy")}, want: value.NewString("Tm94eQ==")},
		{name: "base64 encode bytes", builtin: "base64_encode", args: []value.Value{value.NewBytes("\x00\xff")}, want: value.NewString("AP8=")},
		{name: "base64 encode short args", builtin: "base64_encode", want: value.NewString("")},
		{name: "base64 decode", builtin: "base64_decode", args: []value.Value{value.NewString("Tm94eQ==")}, want: value.NewBytes("Noxy")},
		{name: "base64 decode invalid sentinel", builtin: "base64_decode", args: []value.Value{value.NewString("%%%")}, want: value.NewBytes("")},
		{name: "base64 decode short args", builtin: "base64_decode", want: value.NewBytes("")},
		{name: "base62 encode zero", builtin: "base62_encode", args: []value.Value{value.NewInt(0)}, want: value.NewString("0")},
		{name: "base62 encode boundary", builtin: "base62_encode", args: []value.Value{value.NewInt(62)}, want: value.NewString("10")},
		{name: "base62 encode negative", builtin: "base62_encode", args: []value.Value{value.NewInt(-62)}, want: value.NewString("-10")},
		{name: "base62 encode invalid type sentinel", builtin: "base62_encode", args: []value.Value{value.NewString("62")}, want: value.NewString("")},
		{name: "base62 encode short args", builtin: "base62_encode", want: value.NewString("")},
		{name: "base62 decode zero", builtin: "base62_decode", args: []value.Value{value.NewString("0")}, want: value.NewInt(0)},
		{name: "base62 decode boundary", builtin: "base62_decode", args: []value.Value{value.NewString("10")}, want: value.NewInt(62)},
		{name: "base62 decode negative", builtin: "base62_decode", args: []value.Value{value.NewString("-10")}, want: value.NewInt(-62)},
		{name: "base62 decode empty sentinel", builtin: "base62_decode", args: []value.Value{value.NewString("")}, want: value.NewInt(0)},
		{name: "base62 decode invalid sentinel", builtin: "base62_decode", args: []value.Value{value.NewString("!")}, want: value.NewInt(0)},
		{name: "base62 decode short args", builtin: "base62_decode", want: value.NewInt(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, tt.builtin, tt.args...), tt.want)
		})
	}
}

func TestFmtBuiltin(t *testing.T) {
	machine := New()
	tests := []struct {
		name string
		args []value.Value
		want string
	}{
		{name: "string", args: []value.Value{value.NewString("hello %s"), value.NewString("Noxy")}, want: "hello Noxy"},
		{name: "decimal", args: []value.Value{value.NewString("%d"), value.NewInt(42)}, want: "42"},
		{name: "float", args: []value.Value{value.NewString("%f"), value.NewFloat(1.25)}, want: "1.250000"},
		{name: "type", args: []value.Value{value.NewString("%T"), value.NewArray(nil)}, want: "array"},
		{name: "escaped percent", args: []value.Value{value.NewString("100%% ready")}, want: "100% ready"},
		{name: "insufficient args", args: []value.Value{value.NewString("x=%d y=%s"), value.NewInt(7)}, want: "x=7 y=%!s(MISSING)"},
		{name: "short args sentinel", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "fmt", tt.args...)
			assertBuiltinValue(t, got, value.NewString(tt.want))
		})
	}
}

func TestEprintWritesToStderrOnly(t *testing.T) {
	stdoutReader, stdoutWriter, _ := os.Pipe()
	stderrReader, stderrWriter, _ := os.Pipe()
	prevOut, prevErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	machine := New()
	err := interpretVMSource(t, machine, "eprint(\"erro\", 42)\neiprint(\"x\")\nprint(\"ok\")\n")
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	os.Stdout, os.Stderr = prevOut, prevErr
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(stdoutReader)
	errText, _ := io.ReadAll(stderrReader)
	if string(out) != "ok\n" || string(errText) != "erro 42\nx" {
		t.Fatalf("stdout=%q stderr=%q", out, errText)
	}
}

func TestTaskHandleIdentityAndFormat(t *testing.T) {
	machine := New()
	handle := value.NewTask()
	if !valuesEqual(handle, handle) {
		t.Fatal("task handle is not equal to itself")
	}
	if valuesEqual(handle, value.NewTask()) {
		t.Fatal("distinct task handles compare equal")
	}
	got := callBuiltin(t, machine, "fmt", value.NewString("%T"), handle)
	assertBuiltinValue(t, got, value.NewString("task"))
}
