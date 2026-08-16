package vm

import (
	"math"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

func convertResultField(t *testing.T, machine *VM, native string, arg value.Value, field string) value.Value {
	t.Helper()
	result := callBuiltin(t, machine, native, arg)
	mapping, ok := result.Obj.(*value.ObjMap)
	if !ok {
		t.Fatalf("%s returned %#v, want map", native, result)
	}
	item, exists := mapping.Get(field)
	if !exists {
		t.Fatalf("%s result is missing field %q", native, field)
	}
	return item
}

func TestConvertToIntResultAccepts(t *testing.T) {
	machine := New()
	tests := []struct {
		name  string
		input value.Value
		want  int64
	}{
		{name: "int passes through", input: value.NewInt(42), want: 42},
		{name: "positive float truncates toward zero", input: value.NewFloat(5.9), want: 5},
		{name: "negative float truncates toward zero", input: value.NewFloat(-5.9), want: -5},
		{name: "decimal string", input: value.NewString("42"), want: 42},
		{name: "negative string", input: value.NewString("-42"), want: -42},
		{name: "signed string", input: value.NewString("+42"), want: 42},
		{name: "zero string", input: value.NewString("0"), want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ok := convertResultField(t, machine, "convert_to_int_result", test.input, "ok")
			if ok.Type != value.VAL_BOOL || !ok.AsBool {
				t.Fatalf("ok = %#v, want true", ok)
			}
			got := convertResultField(t, machine, "convert_to_int_result", test.input, "value")
			if got.Type != value.VAL_INT || got.AsInt != test.want {
				t.Fatalf("value = %#v, want %d", got, test.want)
			}
			reason := convertResultField(t, machine, "convert_to_int_result", test.input, "error")
			if reason.String() != "" {
				t.Fatalf("error = %q, want empty", reason.String())
			}
		})
	}
}

func TestConvertToIntResultRejects(t *testing.T) {
	machine := New()
	tests := []struct {
		name  string
		input value.Value
	}{
		{name: "letters", input: value.NewString("abc")},
		{name: "empty string", input: value.NewString("")},
		{name: "decimal point", input: value.NewString("5.5")},
		{name: "leading space", input: value.NewString(" 5")},
		{name: "hex literal", input: value.NewString("0x10")},
		{name: "comma list", input: value.NewString("5, 5")},
		{name: "overflow", input: value.NewString("99999999999999999999")},
		{name: "nineteen nines overflows", input: value.NewString("9999999999999999999")},
		{name: "not a number float", input: value.NewFloat(math.NaN())},
		{name: "positive infinity", input: value.NewFloat(math.Inf(1))},
		{name: "negative infinity", input: value.NewFloat(math.Inf(-1))},
		{name: "float out of int range", input: value.NewFloat(1e300)},
		{name: "bool", input: value.NewBool(true)},
		{name: "null", input: value.NewNull()},
		{name: "bytes", input: value.NewBytes("5")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ok := convertResultField(t, machine, "convert_to_int_result", test.input, "ok")
			if ok.Type != value.VAL_BOOL || ok.AsBool {
				t.Fatalf("ok = %#v, want false", ok)
			}
			got := convertResultField(t, machine, "convert_to_int_result", test.input, "value")
			if got.Type != value.VAL_INT || got.AsInt != 0 {
				t.Fatalf("value = %#v, want 0", got)
			}
			reason := convertResultField(t, machine, "convert_to_int_result", test.input, "error")
			if reason.String() == "" {
				t.Fatal("error is empty, want a reason")
			}
			if strings.Contains(reason.String(), "to_int_result") {
				t.Fatalf("error = %q, want no recommendation clause", reason.String())
			}
		})
	}
}

func TestConvertToFloatResultAccepts(t *testing.T) {
	machine := New()
	tests := []struct {
		name  string
		input value.Value
		want  float64
	}{
		{name: "float passes through", input: value.NewFloat(2.5), want: 2.5},
		{name: "int widens", input: value.NewInt(7), want: 7},
		{name: "decimal string", input: value.NewString("2.5"), want: 2.5},
		{name: "integer string", input: value.NewString("7"), want: 7},
		{name: "exponent string", input: value.NewString("1e3"), want: 1000},
		{name: "not a number float passes through", input: value.NewFloat(math.NaN()), want: math.NaN()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ok := convertResultField(t, machine, "convert_to_float_result", test.input, "ok")
			if ok.Type != value.VAL_BOOL || !ok.AsBool {
				t.Fatalf("ok = %#v, want true", ok)
			}
			got := convertResultField(t, machine, "convert_to_float_result", test.input, "value")
			if got.Type != value.VAL_FLOAT {
				t.Fatalf("value = %#v, want float", got)
			}
			if math.IsNaN(test.want) {
				if !math.IsNaN(got.AsFloat) {
					t.Fatalf("value = %v, want NaN", got.AsFloat)
				}
				return
			}
			if got.AsFloat != test.want {
				t.Fatalf("value = %v, want %v", got.AsFloat, test.want)
			}
		})
	}
}

func TestConvertToFloatResultAcceptsSpecialStrings(t *testing.T) {
	machine := New()
	for _, literal := range []string{"NaN", "Inf", "+Inf", "-Inf"} {
		t.Run(literal, func(t *testing.T) {
			ok := convertResultField(t, machine, "convert_to_float_result", value.NewString(literal), "ok")
			if ok.Type != value.VAL_BOOL || !ok.AsBool {
				t.Fatalf("ok = %#v, want true", ok)
			}
		})
	}
}

func TestConvertToFloatResultRejects(t *testing.T) {
	machine := New()
	for _, test := range []struct {
		name  string
		input value.Value
	}{
		{name: "letters", input: value.NewString("abc")},
		{name: "empty string", input: value.NewString("")},
		{name: "trailing text", input: value.NewString("2.5kg")},
		{name: "bool", input: value.NewBool(false)},
		{name: "null", input: value.NewNull()},
		{name: "bytes", input: value.NewBytes("2.5")},
	} {
		t.Run(test.name, func(t *testing.T) {
			ok := convertResultField(t, machine, "convert_to_float_result", test.input, "ok")
			if ok.Type != value.VAL_BOOL || ok.AsBool {
				t.Fatalf("ok = %#v, want false", ok)
			}
		})
	}
}

func TestConvertResultTruncatesLongInputInReason(t *testing.T) {
	machine := New()
	long := strings.Repeat("z", 500)
	reason := convertResultField(t, machine, "convert_to_int_result", value.NewString(long), "error").String()
	if !strings.Contains(reason, "...") {
		t.Fatalf("error = %q, want a truncation marker", reason)
	}
	if len([]rune(reason)) > 200 {
		t.Fatalf("error is %d characters, want a bounded message", len([]rune(reason)))
	}
}

func TestConvertResultNeverFailsOnBadArity(t *testing.T) {
	machine := New()
	for _, native := range []string{"convert_to_int_result", "convert_to_float_result"} {
		t.Run(native, func(t *testing.T) {
			result := callBuiltin(t, machine, native)
			mapping, ok := result.Obj.(*value.ObjMap)
			if !ok {
				t.Fatalf("%s returned %#v, want map", native, result)
			}
			okField, _ := mapping.Get("ok")
			if okField.Type != value.VAL_BOOL || okField.AsBool {
				t.Fatalf("ok = %#v, want false", okField)
			}
		})
	}
}

func TestConvertModuleExposesTypedWrappers(t *testing.T) {
	source := `use convert select *
let r: IntResult = to_int_result("42")
let bad: FloatResult = to_float_result("abc")
if r.ok && r.value == 42 && !bad.ok && bad.error != "" then
    test_report(true)
else
    test_report(false)
end`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_BOOL || !captured.AsBool {
		t.Fatalf("convert module wrappers = %#v, want true", captured)
	}
}

func TestParseUrlRejectsUnparsablePort(t *testing.T) {
	source := `use http_parser select *
let u: HttpUrl = parse_url("http://example.com:notaport/path")
test_report(u.valid)`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_BOOL || captured.AsBool {
		t.Fatalf("parse_url valid = %#v, want false for an unparsable port", captured)
	}
}

func TestParseUrlKeepsValidPort(t *testing.T) {
	source := `use http_parser select *
let u: HttpUrl = parse_url("http://example.com:8080/path")
if u.valid && u.port == 8080 && u.host == "example.com" && u.path == "/path" then
    test_report(true)
else
    test_report(false)
end`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_BOOL || !captured.AsBool {
		t.Fatalf("parse_url on a valid port = %#v, want true", captured)
	}
}

func TestParseResponseZeroesUnparsableStatus(t *testing.T) {
	source := `use http_parser select *
let raw: bytes = to_bytes("HTTP/1.1 NOPE Weird\r\nHost: a\r\n\r\n")
let r: HttpResponse = parse_response(raw, length(raw))
test_report(r.status_code)`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_INT || captured.AsInt != 0 {
		t.Fatalf("parse_response status_code = %#v, want 0", captured)
	}
}
