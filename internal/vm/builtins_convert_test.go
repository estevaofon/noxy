package vm

import (
	"errors"
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
			if ok.Type != value.VAL_BOOL || !ok.Bool() {
				t.Fatalf("ok = %#v, want true", ok)
			}
			got := convertResultField(t, machine, "convert_to_int_result", test.input, "value")
			if got.Type != value.VAL_INT || got.Int() != test.want {
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
			if ok.Type != value.VAL_BOOL || ok.Bool() {
				t.Fatalf("ok = %#v, want false", ok)
			}
			got := convertResultField(t, machine, "convert_to_int_result", test.input, "value")
			if got.Type != value.VAL_INT || got.Int() != 0 {
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
			if ok.Type != value.VAL_BOOL || !ok.Bool() {
				t.Fatalf("ok = %#v, want true", ok)
			}
			got := convertResultField(t, machine, "convert_to_float_result", test.input, "value")
			if got.Type != value.VAL_FLOAT {
				t.Fatalf("value = %#v, want float", got)
			}
			if math.IsNaN(test.want) {
				if !math.IsNaN(got.Float()) {
					t.Fatalf("value = %v, want NaN", got.Float())
				}
				return
			}
			if got.Float() != test.want {
				t.Fatalf("value = %v, want %v", got.Float(), test.want)
			}
		})
	}
}

func TestConvertToFloatResultAcceptsSpecialStrings(t *testing.T) {
	machine := New()
	for _, literal := range []string{"NaN", "Inf", "+Inf", "-Inf"} {
		t.Run(literal, func(t *testing.T) {
			ok := convertResultField(t, machine, "convert_to_float_result", value.NewString(literal), "ok")
			if ok.Type != value.VAL_BOOL || !ok.Bool() {
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
			if ok.Type != value.VAL_BOOL || ok.Bool() {
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
			if okField.Type != value.VAL_BOOL || okField.Bool() {
				t.Fatalf("ok = %#v, want false", okField)
			}
		})
	}
}

func TestConvertModuleExposesTypedWrappers(t *testing.T) {
	source := `use convert select *
let r = to_int_result("42")
let bad = to_float_result("abc")
if r.ok && r.value == 42 && !bad.ok && bad.failure.message != "" then
    test_report(true)
else
    test_report(false)
end`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_BOOL || !captured.Bool() {
		t.Fatalf("convert module wrappers = %#v, want true", captured)
	}
}

func TestParseUrlRejectsUnparsablePort(t *testing.T) {
	source := `use http_parser select *
let u: HttpUrl = parse_url("http://example.com:notaport/path")
test_report(u.valid)`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_BOOL || captured.Bool() {
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
	if captured.Type != value.VAL_BOOL || !captured.Bool() {
		t.Fatalf("parse_url on a valid port = %#v, want true", captured)
	}
}

func TestParseResponseZeroesUnparsableStatus(t *testing.T) {
	source := `use http_parser select *
let raw: bytes = to_bytes("HTTP/1.1 NOPE Weird\r\nHost: a\r\n\r\n")
let r: HttpResponse = parse_response(raw, length(raw))
test_report(r.status_code)`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_INT || captured.Int() != 0 {
		t.Fatalf("parse_response status_code = %#v, want 0", captured)
	}
}

func requireStrictConversionError(t *testing.T, machine *VM, native string, args ...value.Value) string {
	t.Helper()
	_, err := requireBuiltin(t, machine, native).Invoke(machine, args)
	if err == nil {
		t.Fatalf("%s did not fail", native)
	}
	return err.Error()
}

func TestToIntRaisesOnUnconvertibleInput(t *testing.T) {
	machine := New()
	for _, test := range []struct {
		name  string
		input value.Value
	}{
		{name: "letters", input: value.NewString("abc")},
		{name: "empty string", input: value.NewString("")},
		{name: "decimal point", input: value.NewString("5.5")},
		{name: "overflow", input: value.NewString("9999999999999999999")},
		{name: "not a number", input: value.NewFloat(math.NaN())},
		{name: "infinity", input: value.NewFloat(math.Inf(1))},
		{name: "bool", input: value.NewBool(true)},
		{name: "null", input: value.NewNull()},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := requireStrictConversionError(t, machine, "to_int", test.input)
			if !strings.HasPrefix(message, "to_int: ") {
				t.Fatalf("message = %q, want it to name the function", message)
			}
			if !strings.Contains(message, "cannot convert") {
				t.Fatalf("message = %q, want to describe the conversion failure", message)
			}
		})
	}
}

func TestToIntConvertsAcceptedInput(t *testing.T) {
	machine := New()
	for _, test := range []struct {
		name  string
		input value.Value
		want  int64
	}{
		{name: "int", input: value.NewInt(42), want: 42},
		{name: "positive float truncates", input: value.NewFloat(5.9), want: 5},
		{name: "negative float truncates", input: value.NewFloat(-5.9), want: -5},
		{name: "decimal string", input: value.NewString("-42"), want: -42},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "to_int", test.input)
			if got.Type != value.VAL_INT || got.Int() != test.want {
				t.Fatalf("to_int = %#v, want %d", got, test.want)
			}
		})
	}
}

func TestToFloatRaisesOnUnconvertibleInput(t *testing.T) {
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
	} {
		t.Run(test.name, func(t *testing.T) {
			message := requireStrictConversionError(t, machine, "to_float", test.input)
			if !strings.HasPrefix(message, "to_float: ") {
				t.Fatalf("message = %q, want it to name the function", message)
			}
			if !strings.Contains(message, "cannot convert") {
				t.Fatalf("message = %q, want to describe the conversion failure", message)
			}
		})
	}
}

func TestStrictConversionRaisesOnBadArity(t *testing.T) {
	machine := New()
	for _, native := range []string{"to_int", "to_float", "to_str"} {
		t.Run(native, func(t *testing.T) {
			if message := requireStrictConversionError(t, machine, native); !strings.Contains(message, "exactly 1 argument") {
				t.Fatalf("message = %q, want an arity complaint", message)
			}
		})
	}
}

func TestStrictConversionMessageNamesValueAndType(t *testing.T) {
	machine := New()
	message := requireStrictConversionError(t, machine, "to_int", value.NewString("abc"))
	if !strings.Contains(message, `string "abc"`) {
		t.Fatalf("message = %q, want the rejected type and value", message)
	}
}

func TestToStrValidatesUTF8(t *testing.T) {
	machine := New()
	tests := []struct {
		name       string
		input      string
		wantOffset string
	}{
		{name: "lone 0xFF at start", input: "\xffhi", wantOffset: "offset 0"},
		{name: "lone 0xFF in middle", input: "h\xffi", wantOffset: "offset 1"},
		{name: "truncated multibyte", input: "caf\xc3", wantOffset: "offset 3"},
		{name: "bare continuation byte", input: "\x80", wantOffset: "offset 0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := requireBuiltin(t, machine, "to_str").Invoke(machine, []value.Value{value.NewBytes(test.input)})
			if err == nil {
				t.Fatalf("to_str(%q) did not fail", test.input)
			}
			message := err.Error()
			if !strings.HasPrefix(message, "to_str: ") {
				t.Fatalf("message = %q, want it to name the function", message)
			}
			if !strings.Contains(message, "not valid UTF-8") {
				t.Fatalf("message = %q, want it to say UTF-8", message)
			}
			if !strings.Contains(message, test.wantOffset) {
				t.Fatalf("message = %q, want it to name %s", message, test.wantOffset)
			}
		})
	}
}

func TestToStrRoundTripsValidBytes(t *testing.T) {
	machine := New()
	for _, input := range []string{"", "hello", "café", "\U0001F600 ok", "linha\nquebrada"} {
		got := callBuiltin(t, machine, "to_str", value.NewBytes(input))
		text, ok := got.Obj.(string)
		if !ok || text != input {
			t.Fatalf("to_str(%q) = %#v, want the same bytes back", input, got)
		}
	}
}

func TestToStrLeavesNonBytesArgumentsAlone(t *testing.T) {
	machine := New()
	for _, test := range []struct {
		name  string
		input value.Value
		want  string
	}{
		{name: "int", input: value.NewInt(42), want: "42"},
		{name: "bool", input: value.NewBool(true), want: "true"},
		{name: "string", input: value.NewString("já texto"), want: "já texto"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "to_str", test.input)
			if text, ok := got.Obj.(string); !ok || text != test.want {
				t.Fatalf("to_str = %#v, want %q", got, test.want)
			}
		})
	}
}

// TestToStrRejectsContainerHoldingInvalidBytes covers the laundering path: a
// container is not VAL_BYTES, so to_str used to take its non-bytes branch and
// wrap Value.String() — which renders a nested bytes value as its raw,
// unescaped payload — as a string with no validation at all. The UTF-8
// invariant says to_str is the single choke point, so the rendered result has
// to be checked too, not just the direct bytes payload.
func TestToStrRejectsContainerHoldingInvalidBytes(t *testing.T) {
	machine := New()
	container := value.NewArray([]value.Value{value.NewBytes("h\xffi")})
	_, err := requireBuiltin(t, machine, "to_str").Invoke(machine, []value.Value{container})
	if err == nil {
		t.Fatal("to_str on an array holding invalid UTF-8 bytes did not fail")
	}
	message := err.Error()
	if !strings.HasPrefix(message, "to_str: ") {
		t.Fatalf("message = %q, want it to name the function", message)
	}
	if !strings.Contains(message, "not valid UTF-8") {
		t.Fatalf("message = %q, want it to say UTF-8", message)
	}
}

// TestToStrAcceptsContainerOfOrdinaryValues is the companion guard: the new
// validation must not reject containers whose rendering is ordinary text.
func TestToStrAcceptsContainerOfOrdinaryValues(t *testing.T) {
	machine := New()
	container := value.NewArray([]value.Value{
		value.NewInt(1),
		value.NewString("café"),
		value.NewBool(true),
		value.NewBytes("ok"),
	})
	got := callBuiltin(t, machine, "to_str", container)
	text, ok := got.Obj.(string)
	if !ok {
		t.Fatalf("to_str on an ordinary array = %#v, want a string", got)
	}
	for _, fragment := range []string{"1", "café", "true", "ok"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("to_str = %q, want it to contain %q", text, fragment)
		}
	}
}

func TestToStrBoundsTheEchoedPayload(t *testing.T) {
	machine := New()
	payload := strings.Repeat("a", 500) + "\xff"
	_, err := requireBuiltin(t, machine, "to_str").Invoke(machine, []value.Value{value.NewBytes(payload)})
	if err == nil {
		t.Fatal("to_str did not fail")
	}
	if len([]rune(err.Error())) > 200 {
		t.Fatalf("message is %d characters, want a bounded message", len([]rune(err.Error())))
	}
}

func TestToIntAdvisedError(t *testing.T) {
	machine := New()
	err := interpretVMSource(t, machine, `to_int("abc")`)
	if err == nil {
		t.Fatal("expected runtime error")
	}
	if strings.Contains(err.Error(), "use to_int_result") {
		t.Fatalf("advisory suffix leaked into capturable message: %v", err)
	}
	var advised *AdvisedError
	if !errors.As(err, &advised) || advised.Advice != "use to_int_result to handle failure" {
		t.Fatalf("advice not carried structurally: %v", err)
	}
}
