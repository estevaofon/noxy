# Standard Library Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix seven standard library and runtime defects that share one shape — an API that fails without saying so.

**Architecture:** Six of the seven changes are Go native implementations under `internal/vm/`; one is a comment repair in embedded `.nx` sources. Numeric conversion moves out of `builtins_core.go` into a dedicated `builtins_convert.go` that owns both the strict and the recoverable forms. Three architecture guard tests prevent the classes of regression involved: duplicate native registration, shipped debug output, and lossy source encoding.

**Tech Stack:** Go (VM natives, `go/ast` for guard tests), Noxy stdlib `.nx` modules.

**Spec:** `docs/superpowers/specs/2026-08-15-stdlib-reliability-design.md`

## Global Constraints

- Branch is `fix/stdlib-reliability`, already created off `develop`.
- **Task ordering is load-bearing.** The recoverable forms are added first, then every caller is migrated, and only then do `to_int`/`to_float` become strict. Every task leaves the test suite green. Do not reorder.
- `DefineNative` and `DefineNativeWithSignature` resolve to `DefineLocalIfAbsent`, so the **first** registration of a name wins. `DefineContextualNative` resolves to `SetGlobal`, so the **last** wins. Never assume one rule for both.
- User-facing text — runtime error messages, module docs, the language spec — says **character**, never "rune". `rune` appears only in Go code.
- Noxy has no `continue` keyword; use `if`/`else` inside loops.
- Noxy has no `min`/`max` builtin; clamp with explicit `if`.
- Noxy string literals support only `\n`, `\r`, `\t`, `\"`, `\'`, `\\`. There is no `\u` escape; build a character with `from_char_code(code)`.
- A top-level variable reassigned inside a function must be declared `global`, not `let`.
- `length(s)` on a `string` counts characters; on `bytes` it counts octets.
- Commit after every task. Commit messages end with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/vm/builtins_convert.go` | Create | shared conversion logic plus `to_int`, `to_float`, `convert_to_int_result`, `convert_to_float_result` |
| `internal/vm/builtins_convert_test.go` | Create | conversion rules, error text, result envelopes |
| `internal/vm/builtins_core.go` | Modify | remove the lenient `to_int` / `to_float` |
| `internal/vm/builtins.go` | Modify | register `defineConvertBuiltins` |
| `internal/stdlib/convert.nx` | Create | `IntResult`, `FloatResult`, `to_int_result`, `to_float_result` |
| `internal/vm/builtins_strings.go` | Modify | character-based `index_of`, `bytes` rejection, `ord` code point, delete dead duplicate registrations |
| `internal/vm/builtins_strings_test.go` | Modify | tests for the above |
| `internal/stdlib/strings.nx` | Modify | export `char_code`, repair comments |
| `internal/stdlib/http_parser.nx` | Modify | migrate two `to_int` call sites |
| `internal/stdlib/http_client.nx` | Modify | remove the debug print |
| `internal/vm/builtins_net.go` | Modify | remove the debug print |
| `internal/stdlib/http.nx`, `internal/stdlib/time.nx` | Modify | repair comments |
| `internal/vm/stdlib_hygiene_test.go` | Create | guards: no duplicate registration, no debug markers, valid UTF-8 |
| `noxy_examples/test_convert.nx` | Create | Noxy-level conversion tests |
| `docs/NOXY_LANGUAGE_SPEC.md`, `CHANGELOG.md` | Modify | contract and release notes |

---

### Task 1: Recoverable conversion forms

Add `to_int_result` and `to_float_result`. Purely additive — `to_int` and `to_float` are untouched, so nothing can break.

**Files:**
- Create: `internal/vm/builtins_convert.go`
- Create: `internal/vm/builtins_convert_test.go`
- Create: `internal/stdlib/convert.nx`
- Modify: `internal/vm/builtins.go`

**Interfaces:**
- Consumes: `runtimeValueMode(v value.Value) string` from `internal/vm/call_validation.go:8`.
- Produces:
  - Go: `func convertValueToInt(v value.Value) (int64, error)`, `func convertValueToFloat(v value.Value) (float64, error)`, `func describeConversionInput(v value.Value) string`
  - Natives: `convert_to_int_result`, `convert_to_float_result`, each returning a map with keys `ok` (bool), `value` (int/float), `error` (string)
  - Noxy: `struct IntResult { ok: bool, value: int, error: string }`, `struct FloatResult { ok: bool, value: float, error: string }`, `func to_int_result(v: any) -> IntResult`, `func to_float_result(v: any) -> FloatResult`

- [ ] **Step 1: Write the failing test**

Create `internal/vm/builtins_convert_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run TestConvert -v`

Expected: FAIL — `convert_to_int_result` is not a registered builtin.

- [ ] **Step 3: Create the conversion implementation**

Create `internal/vm/builtins_convert.go`:

```go
package vm

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"noxy-vm/internal/value"
)

// conversionInputLimit bounds how much of a rejected value appears in an error
// message, measured in characters.
const conversionInputLimit = 64

// int64 bounds as exactly representable float64 values (-2^63 and 2^63).
const (
	minInt64AsFloat       = -9223372036854775808.0
	maxInt64ExclusiveFloat = 9223372036854775808.0
)

func truncateConversionInput(text string) string {
	characters := []rune(text)
	if len(characters) <= conversionInputLimit {
		return text
	}
	return string(characters[:conversionInputLimit]) + "..."
}

// conversionTypeName refines runtimeValueMode, which reports every heap value
// as "object", so a rejected string is named as a string.
func conversionTypeName(item value.Value) string {
	if item.Type == value.VAL_OBJ {
		if _, ok := item.Obj.(string); ok {
			return "string"
		}
	}
	return runtimeValueMode(item)
}

func describeConversionInput(item value.Value) string {
	switch item.Type {
	case value.VAL_OBJ, value.VAL_BYTES:
		if text, ok := item.Obj.(string); ok {
			return fmt.Sprintf("%s %q", conversionTypeName(item), truncateConversionInput(text))
		}
	}
	return fmt.Sprintf("%s %s", conversionTypeName(item), truncateConversionInput(item.String()))
}

func convertValueToInt(item value.Value) (int64, error) {
	switch item.Type {
	case value.VAL_INT:
		return item.AsInt, nil
	case value.VAL_FLOAT:
		if math.IsNaN(item.AsFloat) || math.IsInf(item.AsFloat, 0) {
			return 0, fmt.Errorf("cannot convert %s to int", describeConversionInput(item))
		}
		truncated := math.Trunc(item.AsFloat)
		if truncated < minInt64AsFloat || truncated >= maxInt64ExclusiveFloat {
			return 0, fmt.Errorf("cannot convert %s to int: out of range", describeConversionInput(item))
		}
		return int64(truncated), nil
	case value.VAL_OBJ:
		if text, ok := item.Obj.(string); ok {
			parsed, parseErr := strconv.ParseInt(text, 10, 64)
			if parseErr != nil {
				if errors.Is(parseErr, strconv.ErrRange) {
					return 0, fmt.Errorf("cannot convert %s to int: out of range", describeConversionInput(item))
				}
				return 0, fmt.Errorf("cannot convert %s to int", describeConversionInput(item))
			}
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("cannot convert %s to int", describeConversionInput(item))
}

func convertValueToFloat(item value.Value) (float64, error) {
	switch item.Type {
	case value.VAL_FLOAT:
		return item.AsFloat, nil
	case value.VAL_INT:
		return float64(item.AsInt), nil
	case value.VAL_OBJ:
		if text, ok := item.Obj.(string); ok {
			parsed, parseErr := strconv.ParseFloat(text, 64)
			if parseErr != nil {
				if errors.Is(parseErr, strconv.ErrRange) {
					return 0, fmt.Errorf("cannot convert %s to float: out of range", describeConversionInput(item))
				}
				return 0, fmt.Errorf("cannot convert %s to float", describeConversionInput(item))
			}
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("cannot convert %s to float", describeConversionInput(item))
}

func conversionResult(ok bool, converted value.Value, reason string) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":    value.NewBool(ok),
		"value": converted,
		"error": value.NewString(reason),
	})
}

func (vm *VM) defineConvertBuiltins() {
	vm.DefineNative("convert_to_int_result", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return conversionResult(false, value.NewInt(0),
				fmt.Sprintf("to_int_result expects exactly 1 argument, got %d", len(args)))
		}
		converted, convertErr := convertValueToInt(args[0])
		if convertErr != nil {
			return conversionResult(false, value.NewInt(0), convertErr.Error())
		}
		return conversionResult(true, value.NewInt(converted), "")
	})

	vm.DefineNative("convert_to_float_result", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return conversionResult(false, value.NewFloat(0),
				fmt.Sprintf("to_float_result expects exactly 1 argument, got %d", len(args)))
		}
		converted, convertErr := convertValueToFloat(args[0])
		if convertErr != nil {
			return conversionResult(false, value.NewFloat(0), convertErr.Error())
		}
		return conversionResult(true, value.NewFloat(converted), "")
	})
}
```

- [ ] **Step 4: Register the new builtin group**

In `internal/vm/builtins.go`, add the call to `defineBuiltins`, immediately after `vm.defineCoreBuiltins()`:

```go
func (vm *VM) defineBuiltins() {
	vm.defineCoreBuiltins()
	vm.defineConvertBuiltins()
	vm.defineConcurrencyBuiltins()
	// ... remaining calls unchanged
```

- [ ] **Step 5: Create the Noxy module**

Create `internal/stdlib/convert.nx`:

```noxy
// stdlib/convert.nx - Conversões numéricas verificáveis
//
// to_int e to_float levantam erro quando a conversão é impossível.
// Use as formas _result quando a falha for um dado esperado e não um bug.

struct IntResult
    ok: bool
    value: int
    error: string
end

struct FloatResult
    ok: bool
    value: float
    error: string
end

func to_int_result(v: any) -> IntResult
    return convert_to_int_result(v)
end

func to_float_result(v: any) -> FloatResult
    return convert_to_float_result(v)
end
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run TestConvert -v`

Expected: PASS, every subtest.

- [ ] **Step 7: Verify nothing regressed**

Run: `go build ./... && go vet ./... && go test ./internal/...`

Expected: PASS. `to_int` and `to_float` are still lenient at this point, which is intentional.

- [ ] **Step 8: Commit**

```bash
git add internal/vm/builtins_convert.go internal/vm/builtins_convert_test.go internal/stdlib/convert.nx internal/vm/builtins.go
git commit -m "feat(convert): add verifiable numeric conversion

to_int_result and to_float_result report failure through an IntResult or
FloatResult instead of a sentinel, following the io.close/close_result
convention the standard library already uses. Purely additive; to_int and
to_float are unchanged in this commit.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Migrate callers that parse untrusted input

Move every call site that can receive non-numeric input to the `_result` form, **before** the strict forms land. The suite stays green throughout because the lenient `to_int` is still in place.

**Files:**
- Modify: `internal/stdlib/http_parser.nx:132`, `:233`
- Modify: `noxy_examples/form_app.nx:123`
- Modify: `noxy_examples/todo_app.nx:196`
- Modify: `noxy_examples/password_manager/server.nx:203`
- Modify: `noxy_examples/web_app.nx:148`
- Test: `internal/vm/builtins_convert_test.go`

**Interfaces:**
- Consumes: `to_int_result`, `IntResult` from Task 1.
- Produces: no new names. `parse_url` gains the behavior that an unparsable port makes the URL invalid.

- [ ] **Step 1: Write the failing test**

Append to `internal/vm/builtins_convert_test.go`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run 'TestParseUrl|TestParseResponse' -v`

Expected: `TestParseUrlRejectsUnparsablePort` FAILS — `to_int("notaport")` currently yields `0` and `valid` is still set to `true`. The other two pass already; they are pinned so the migration cannot change them.

- [ ] **Step 3: Migrate `parse_url`**

In `internal/stdlib/http_parser.nx`, add the module import at the top, after `use strings select *`:

```noxy
use convert select *
```

Replace the host/port block (currently lines 128-135):

```noxy
    // 5. Host:Port
    let split_port: SplitResult = split(authority, ":")
    if split_port.count > 1 then
        res.host = split_port.parts[0]
        let parsed_port: IntResult = to_int_result(split_port.parts[1])
        if !parsed_port.ok then
            return res
        end
        res.port = parsed_port.value
    else
        res.host = authority
    end
```

`res` still carries `valid = false` at that point, so returning early yields an invalid URL rather than a URL with a fabricated port.

- [ ] **Step 4: Migrate `parse_response`**

In `internal/stdlib/http_parser.nx`, replace the status code line (currently line 233):

```noxy
        if s_parts.count > 1 then
            let parsed_status: IntResult = to_int_result(s_parts.parts[1])
            if parsed_status.ok then
                resp.status_code = parsed_status.value
            else
                resp.status_code = 0
            end
        end
```

Assigning `0` preserves today's observable result for a malformed status line while making it a decision instead of a silent fallback.

- [ ] **Step 5: Migrate `form_app.nx`**

This call site relies on the `0` sentinel as its error signal. Replace lines 119-127 of `noxy_examples/form_app.nx`:

```noxy
let port_env: EnvResult = getenv("PORT")
if port_env.ok then
    let parsed_port: IntResult = to_int_result(port_env.value)
    if parsed_port.ok && parsed_port.value > 0 then
        port = parsed_port.value
    else
        print("PORT inválida, usando " + to_str(port))
    end
end
```

Add `use convert select *` to the imports at the top of the file.

- [ ] **Step 6: Migrate `todo_app.nx`**

This call site has no guard at all today: a malformed `PORT` silently becomes `0`. Replace lines 193-197 of `noxy_examples/todo_app.nx`:

```noxy
let port_str: string = getenv("PORT").value
let port: int = 8080
if !is_empty(port_str) then
    let parsed_port: IntResult = to_int_result(port_str)
    if parsed_port.ok && parsed_port.value > 0 then
        port = parsed_port.value
    else
        print("PORT inválida, usando 8080")
    end
end
```

Add `use convert select *` to the imports at the top of the file.

- [ ] **Step 7: Migrate `password_manager/server.nx`**

This call site also relies on the `0` sentinel. Replace lines 202-206 of `noxy_examples/password_manager/server.nx`:

```noxy
func handle_delete_password(id_str: string) -> http_parser.HttpResponse
    let parsed_id: IntResult = to_int_result(id_str)
    if !parsed_id.ok || parsed_id.value <= 0 then
        return http_server.response_error(400, "Invalid ID")
    end
    let id: int = parsed_id.value
```

Add `use convert select *` to the imports at the top of the file.

- [ ] **Step 8: Migrate `web_app.nx`**

The age arrives from decoded JSON and may be any type. Replace line 148 of `noxy_examples/web_app.nx`:

```noxy
                if has_key(m, "age") then
                    let parsed_age: IntResult = to_int_result(m["age"])
                    if parsed_age.ok then
                        req.age = parsed_age.value
                    end
                end
```

Add `use convert select *` to the imports at the top of the file.

- [ ] **Step 9: Audit the remaining call sites**

These fourteen sites read values that are already numeric. Open each, confirm the value cannot arrive as arbitrary text, and leave `to_int` / `to_float` in place. If any turns out to read parsed or external input, migrate it with the same pattern used above.

```text
noxy_examples/benchmark_parallel.nx:51            to_int(partial)
noxy_examples/concurrency_parallel_sum.nx:58      to_int(part)
noxy_examples/concurrency_producer_consumer.nx:26 to_int(v)
noxy_examples/password_manager/server.nx:191      to_int(id_val)
noxy_examples/result_pattern.nx:28                to_int(result.unwrap(res1))
noxy_examples/result_pattern.nx:45                to_float(result.unwrap(val1))
noxy_examples/result_pattern.nx:46                to_float(result.unwrap(val2))
noxy_examples/signal_demo.nx:20                   to_int(sig)
noxy_examples/statics.nx:74                       to_float(data[i])
noxy_examples/statics.nx:77                       to_float(length(data))
noxy_examples/statics.nx:86                       to_float(sorted_data[mid - 1]), to_float(sorted_data[mid])
noxy_examples/statics.nx:88                       to_float(sorted_data[mid])
noxy_examples/statics.nx:130                      to_float(data[i])
noxy_examples/statics.nx:134                      to_float(length(data))
```

`noxy_examples/aws_lambda/runtime.nx:78` is inside a comment and needs no change.

- [ ] **Step 10: Run the tests**

Run: `go test ./internal/vm/ -run 'TestParseUrl|TestParseResponse' -v`

Expected: PASS, all three.

- [ ] **Step 11: Run both example suites**

```bash
go run cmd/noxy/main.go noxy_examples/run_all_tests.nx
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: PASS, with the same count as before the branch. Record that count — Task 3 compares against it.

- [ ] **Step 12: Commit**

```bash
git add internal/stdlib/http_parser.nx noxy_examples/form_app.nx noxy_examples/todo_app.nx noxy_examples/password_manager/server.nx noxy_examples/web_app.nx internal/vm/builtins_convert_test.go
git commit -m "refactor: parse untrusted input with to_int_result

Four of these call sites used the silent 0 from to_int as their error
signal: parse_url fabricated port 0 for a malformed authority, form_app
and password_manager branched on the sentinel, and todo_app had no guard
at all. They now branch on IntResult.ok, which is both correct today and
a prerequisite for making to_int strict.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: Strict `to_int` and `to_float`

Flip the default. Every caller that could receive non-numeric input was migrated in Task 2, so the suite must stay green.

**Files:**
- Modify: `internal/vm/builtins_convert.go`
- Modify: `internal/vm/builtins_core.go:45-88`
- Modify: `internal/vm/builtins_convert_test.go`
- Create: `noxy_examples/test_convert.nx`

**Interfaces:**
- Consumes: `convertValueToInt`, `convertValueToFloat` from Task 1.
- Produces: `to_int` and `to_float` raise a synchronous runtime error on an unconvertible input or an argument count other than one.

- [ ] **Step 1: Write the failing test**

Append to `internal/vm/builtins_convert_test.go`:

```go
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
			if !strings.Contains(message, "use to_int_result to handle failure") {
				t.Fatalf("message = %q, want the recoverable alternative", message)
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
			if got.Type != value.VAL_INT || got.AsInt != test.want {
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
			if !strings.Contains(message, "use to_float_result to handle failure") {
				t.Fatalf("message = %q, want the recoverable alternative", message)
			}
		})
	}
}

func TestStrictConversionRaisesOnBadArity(t *testing.T) {
	machine := New()
	for _, native := range []string{"to_int", "to_float"} {
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run 'TestToInt|TestToFloat|TestStrictConversion' -v`

Expected: FAIL — `to_int` currently returns `0` instead of an error.

- [ ] **Step 3: Delete the lenient implementations**

In `internal/vm/builtins_core.go`, delete the whole `vm.DefineNative("to_int", ...)` block (currently lines 45-67) and the whole `vm.DefineNative("to_float", ...)` block (currently lines 68-88). Remove the `strconv` import if nothing else in the file uses it; run `go build ./...` to confirm.

- [ ] **Step 4: Add the strict implementations**

Append to `defineConvertBuiltins` in `internal/vm/builtins_convert.go`. Registration uses `DefineContextualNative` because the contextual signature is the only one in this VM that returns an `error`; `NativeFunc` returns a bare `value.Value` and cannot raise:

```go
	vm.DefineContextualNative("to_int", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) != 1 {
			return value.NewNull(), fmt.Errorf("to_int: expects exactly 1 argument, got %d", len(args))
		}
		converted, convertErr := convertValueToInt(args[0])
		if convertErr != nil {
			return value.NewNull(), fmt.Errorf("to_int: %w; use to_int_result to handle failure", convertErr)
		}
		return value.NewInt(converted), nil
	})

	vm.DefineContextualNative("to_float", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) != 1 {
			return value.NewNull(), fmt.Errorf("to_float: expects exactly 1 argument, got %d", len(args))
		}
		converted, convertErr := convertValueToFloat(args[0])
		if convertErr != nil {
			return value.NewNull(), fmt.Errorf("to_float: %w; use to_float_result to handle failure", convertErr)
		}
		return value.NewFloat(converted), nil
	})
```

`DefineContextualNative` resolves through `SetGlobal`, so it overwrites rather than being skipped. Verify with `go test ./internal/vm/ -run TestToIntConvertsAcceptedInput` that the new definition is the live one; if the old registration still wins, the deletion in Step 3 was incomplete.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run 'TestToInt|TestToFloat|TestStrictConversion|TestConvert' -v`

Expected: PASS, every subtest.

- [ ] **Step 6: Write the Noxy-level test**

Create `noxy_examples/test_convert.nx`:

```noxy
// Testes de conversão numérica
use convert select *

global passed: int = 0
global failed: int = 0

func check(name: string, condition: bool) -> void
    if condition then
        passed = passed + 1
        print("PASS: " + name)
    else
        failed = failed + 1
        print("FAIL: " + name)
    end
end

// Forma estrita, apenas entradas válidas
check("to_int de string decimal", to_int("42") == 42)
check("to_int de string negativa", to_int("-42") == -42)
check("to_int trunca float positivo", to_int(5.9) == 5)
check("to_int trunca float negativo", to_int(-5.9) == -5)
check("to_float de string", to_float("2.5") == 2.5)
check("to_float alarga int", to_float(7) == 7.0)

// Forma recuperável
let ok_int: IntResult = to_int_result("42")
check("to_int_result aceita", ok_int.ok && ok_int.value == 42)

let bad_int: IntResult = to_int_result("abc")
check("to_int_result rejeita texto", !bad_int.ok)
check("to_int_result explica a falha", bad_int.error != "")
check("to_int_result zera o valor", bad_int.value == 0)

let overflow: IntResult = to_int_result("9999999999999999999")
check("to_int_result detecta overflow", !overflow.ok)

let decimal: IntResult = to_int_result("5.5")
check("to_int_result rejeita decimal", !decimal.ok)

let bad_float: FloatResult = to_float_result("abc")
check("to_float_result rejeita texto", !bad_float.ok)

let ok_float: FloatResult = to_float_result("2.5")
check("to_float_result aceita", ok_float.ok && ok_float.value == 2.5)

print("")
print("passed=" + to_str(passed) + " failed=" + to_str(failed))
if failed > 0 then
    print("CONVERT TESTS FAILED")
else
    print("CONVERT TESTS OK")
end
```

- [ ] **Step 7: Run it**

Run: `go run cmd/noxy/main.go noxy_examples/test_convert.nx`

Expected: every line prints `PASS:` and the last line is `CONVERT TESTS OK`.

- [ ] **Step 8: Run both example suites**

```bash
go run cmd/noxy/main.go noxy_examples/run_all_tests.nx
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: PASS with at least the count recorded in Task 2 Step 11, plus one for `test_convert.nx`. **If any example now fails, it is a call site Task 2 Step 9 misclassified** — migrate it to the `_result` form rather than relaxing the strict conversion.

- [ ] **Step 9: Commit**

```bash
git add internal/vm/builtins_convert.go internal/vm/builtins_core.go internal/vm/builtins_convert_test.go noxy_examples/test_convert.nx
git commit -m "fix!: to_int and to_float raise instead of returning a sentinel

BREAKING CHANGE: to_int(\"abc\") returned 0, indistinguishable from
to_int(\"0\"), so no caller could detect a failed conversion. Both now
raise a runtime error naming the rejected value and pointing at the
_result form.

The validate-first alternative is unsound in Noxy: is_digit passes for
\"9999999999999999999\", which overflows int64, and range cannot be
checked without converting. There is no is_float at all.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Character-based `index_of`

**Files:**
- Modify: `internal/vm/builtins_strings.go:29-34`
- Modify: `internal/vm/builtins_strings_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `strings_index_of` returns an index measured in characters.

- [ ] **Step 1: Write the failing test**

Append to `internal/vm/builtins_strings_test.go`:

```go
func TestIndexOfReturnsCharacterIndex(t *testing.T) {
	machine := New()
	tests := []struct {
		name    string
		subject string
		needle  string
		want    int64
	}{
		{name: "ascii matches byte offset", subject: "abc:def", needle: ":", want: 3},
		{name: "multibyte before match", subject: "münchen.de/path", needle: "/", want: 10},
		{name: "multibyte needle", subject: "café bar", needle: "é", want: 3},
		{name: "emoji before match", subject: "\U0001F600x:y", needle: ":", want: 3},
		{name: "absent", subject: "abc", needle: "z", want: -1},
		{name: "empty needle", subject: "abc", needle: "", want: 0},
		{name: "match at start", subject: ":abc", needle: ":", want: 0},
		{name: "empty subject", subject: "", needle: "a", want: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "strings_index_of", value.NewString(test.subject), value.NewString(test.needle))
			if got.Type != value.VAL_INT || got.AsInt != test.want {
				t.Fatalf("strings_index_of(%q, %q) = %#v, want %d", test.subject, test.needle, got, test.want)
			}
		})
	}
}

func TestIndexOfComposesWithSubstring(t *testing.T) {
	source := `use strings select *
let line: string = "München: Bayern"
let name: string = substring(line, 0, index_of(line, ":"))
test_report(name)`
	captured := captureVMSource(t, strings.ReplaceAll(source, `ü`, "ü"))
	got, ok := captured.Obj.(string)
	if !ok || got != "München" {
		t.Fatalf("substring with index_of = %#v, want %q", captured, "München")
	}
}

func TestParseUrlKeepsMultibyteHostSeparateFromPath(t *testing.T) {
	source := "use http_parser select *\n" +
		"let u: HttpUrl = parse_url(\"http://münchen.de/path\")\n" +
		"test_report(u.host + \"|\" + u.path)"
	captured := captureVMSource(t, source)
	got, ok := captured.Obj.(string)
	if !ok || got != "münchen.de|/path" {
		t.Fatalf("parse_url = %#v, want %q", captured, "münchen.de|/path")
	}
}
```

Add `"strings"` to the import block of `internal/vm/builtins_strings_test.go` if it is not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run 'TestIndexOf|TestParseUrlKeepsMultibyte' -v`

Expected: FAIL — `münchen.de/path` reports `11` (bytes) instead of `10` (characters), and `parse_url` returns host `münchen.de/` with path `path`.

- [ ] **Step 3: Convert the offset**

Replace the `strings_index_of` registration in `internal/vm/builtins_strings.go` (currently lines 29-34):

```go
	vm.DefineNative("strings_index_of", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(-1)
		}
		subject := args[0].String()
		byteOffset := strings.Index(subject, args[1].String())
		if byteOffset < 0 {
			return value.NewInt(-1)
		}
		// Noxy indexes strings by character, so translate the byte offset
		// that strings.Index reports into a character index.
		return value.NewInt(int64(utf8.RuneCountInString(subject[:byteOffset])))
	})
```

Add `"unicode/utf8"` to the import block of `internal/vm/builtins_strings.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run 'TestIndexOf|TestParseUrlKeepsMultibyte' -v`

Expected: PASS, every subtest.

- [ ] **Step 5: Run the full suite**

Run: `go test ./internal/... && go run cmd/noxy/main.go noxy_examples/run_all_tests.nx`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/vm/builtins_strings.go internal/vm/builtins_strings_test.go
git commit -m "fix: index_of returns a character index

strings.Index reports a byte offset, but substring, char_at, length, and
slice all measure in characters, so composing them was silently wrong for
any non-ASCII text. parse_url did exactly that: http://muenchen.de/path
with a real umlaut yielded host \"münchen.de/\" and path \"path\".

ASCII text is unaffected, since the two units coincide there.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Text functions reject `bytes`

**Files:**
- Modify: `internal/vm/builtins_strings.go`
- Modify: `internal/vm/builtins_strings_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: Go helper `func requireTextArgument(function string, args []value.Value, index int) error`; every `strings_*` text parameter and `ord` reject `VAL_BYTES`.

- [ ] **Step 1: Write the failing test**

Append to `internal/vm/builtins_strings_test.go`:

```go
func TestStringNativesRejectBytes(t *testing.T) {
	machine := New()
	text := value.NewString("x")
	number := value.NewInt(1)
	payload := value.NewBytes("hello")

	tests := []struct {
		native string
		args   []value.Value
	}{
		{native: "strings_contains", args: []value.Value{payload, text}},
		{native: "strings_contains", args: []value.Value{text, payload}},
		{native: "strings_starts_with", args: []value.Value{payload, text}},
		{native: "strings_ends_with", args: []value.Value{payload, text}},
		{native: "strings_index_of", args: []value.Value{payload, text}},
		{native: "strings_index_of", args: []value.Value{text, payload}},
		{native: "strings_count", args: []value.Value{payload, text}},
		{native: "strings_to_upper", args: []value.Value{payload}},
		{native: "strings_to_lower", args: []value.Value{payload}},
		{native: "strings_trim", args: []value.Value{payload}},
		{native: "strings_reverse", args: []value.Value{payload}},
		{native: "strings_repeat", args: []value.Value{payload, number}},
		{native: "strings_replace", args: []value.Value{payload, text, text}},
		{native: "strings_replace_first", args: []value.Value{payload, text, text}},
		{native: "strings_pad_left", args: []value.Value{payload, number, text}},
		{native: "strings_substring", args: []value.Value{payload, number, number}},
		{native: "strings_is_empty", args: []value.Value{payload}},
		{native: "strings_is_digit", args: []value.Value{payload}},
		{native: "strings_is_alpha", args: []value.Value{payload}},
		{native: "strings_is_alnum", args: []value.Value{payload}},
		{native: "strings_is_space", args: []value.Value{payload}},
		{native: "strings_char_at", args: []value.Value{payload, number}},
		{native: "ord", args: []value.Value{payload}},
	}
	for _, test := range tests {
		t.Run(test.native, func(t *testing.T) {
			_, err := requireBuiltin(t, machine, test.native).Invoke(machine, test.args)
			if err == nil {
				t.Fatalf("%s accepted a bytes argument", test.native)
			}
			if !strings.Contains(err.Error(), "expected string, got bytes") {
				t.Fatalf("message = %q, want it to name the type mismatch", err.Error())
			}
			if !strings.Contains(err.Error(), "to_str") {
				t.Fatalf("message = %q, want it to point at to_str", err.Error())
			}
		})
	}
}

func TestStringNativesStillAcceptStrings(t *testing.T) {
	machine := New()
	got := callBuiltin(t, machine, "strings_contains", value.NewString("hello"), value.NewString("ell"))
	if got.Type != value.VAL_BOOL || !got.AsBool {
		t.Fatalf("strings_contains = %#v, want true", got)
	}
}

func TestSplitRejectsBytesButAcceptsItsStructArgument(t *testing.T) {
	source := `use strings select *
let parts: SplitResult = split(to_str(b"a,b"), ",")
test_report(parts.count)`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_INT || captured.AsInt != 2 {
		t.Fatalf("split after explicit to_str = %#v, want 2", captured)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run 'TestStringNativesReject|TestStringNativesStill|TestSplitRejects' -v`

Expected: FAIL — every native currently accepts `bytes` and silently operates on the `b"..."` display form.

- [ ] **Step 3: Add the guard helper**

Add near the top of `internal/vm/builtins_strings.go`, after the imports:

```go
// requireTextArgument rejects a bytes value where text is expected. Value.String()
// renders bytes as its display form b"...", so accepting it would make the
// function operate on text the caller never wrote.
func requireTextArgument(function string, args []value.Value, index int) error {
	if index >= len(args) {
		return nil
	}
	if args[index].Type == value.VAL_BYTES {
		return fmt.Errorf("%s: expected string, got bytes; use to_str(value) to convert explicitly", function)
	}
	return nil
}
```

Add `"fmt"` to the import block if absent.

- [ ] **Step 4: Convert the affected natives to the contextual form**

Each native listed in the Step 1 table must return an error, so it moves from `DefineNative` to `DefineContextualNative`. Apply this shape to every one, changing only the registration wrapper and the guard — the existing body is unchanged:

```go
	vm.DefineContextualNative("strings_contains", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 2 {
			return value.NewBool(false), nil
		}
		for _, index := range []int{0, 1} {
			if err := requireTextArgument("strings.contains", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		return value.NewBool(strings.Contains(args[0].String(), args[1].String())), nil
	})
```

The text parameter indices per native are:

| Native | Public name in the message | Text parameter indices |
|---|---|---|
| `strings_contains` | `strings.contains` | 0, 1 |
| `strings_starts_with` | `strings.starts_with` | 0, 1 |
| `strings_ends_with` | `strings.ends_with` | 0, 1 |
| `strings_index_of` | `strings.index_of` | 0, 1 |
| `strings_count` | `strings.count` | 0, 1 |
| `strings_to_upper` | `strings.to_upper` | 0 |
| `strings_to_lower` | `strings.to_lower` | 0 |
| `strings_trim` | `strings.trim` | 0 |
| `strings_reverse` | `strings.reverse` | 0 |
| `strings_repeat` | `strings.repeat` | 0 |
| `strings_replace` | `strings.replace` | 0, 1, 2 |
| `strings_replace_first` | `strings.replace_first` | 0, 1, 2 |
| `strings_pad_left` | `strings.pad_left` | 0, 2 |
| `strings_split` | `strings.split` | 0, 1 |
| `strings_join_count` | `strings.join_count` | 1 |
| `strings_substring` | `strings.substring` | 0 |
| `strings_is_empty` | `strings.is_empty` | 0 |
| `strings_is_digit` | `strings.is_digit` | 0 |
| `strings_is_alpha` | `strings.is_alpha` | 0 |
| `strings_is_alnum` | `strings.is_alnum` | 0 |
| `strings_is_space` | `strings.is_space` | 0 |
| `strings_char_at` | `strings.char_at` | 0 |
| `ord` | `ord` | 0 |

`strings_from_char_code` takes an `int` and is unchanged. `strings_split` index 2 is a struct definition and `strings_join_count` index 0 is an array; neither is guarded.

**Registration order matters here.** `DefineContextualNative` overwrites while `DefineNative` does not, so a native that still has a leftover `DefineNative` registration elsewhere would be shadowed inconsistently. Task 6 removes the duplicates; if a test in this task behaves as though the change did not take effect, that duplicate is the cause.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run 'TestStringNativesReject|TestStringNativesStill|TestSplitRejects' -v`

Expected: PASS, every subtest.

- [ ] **Step 6: Run the full suite**

Run: `go test ./internal/... && go run cmd/noxy/main.go noxy_examples/run_all_tests.nx`

Expected: PASS. A failure here means a `.nx` source was passing `bytes` to a text function and relying on the `b"..."` rendering; fix that call site with an explicit `to_str`.

- [ ] **Step 7: Commit**

```bash
git add internal/vm/builtins_strings.go internal/vm/builtins_strings_test.go
git commit -m "fix: text functions reject bytes instead of reading b\"...\"

Value.String() renders a bytes value as its display form, so
contains(payload, \"x\") searched a string carrying a literal b\" prefix
and trailing quote, and index_of returned an offset shifted by two.

Text parameters now raise and point at to_str, establishing the boundary:
string is text indexed by character, bytes is octets indexed by octet.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: One registration per native, and `ord` reads a character

Two independent fixes in the same file, sharing one guard test.

**Files:**
- Modify: `internal/vm/builtins_strings.go`
- Modify: `internal/stdlib/strings.nx`
- Create: `internal/vm/stdlib_hygiene_test.go`
- Modify: `internal/vm/builtins_strings_test.go`

**Interfaces:**
- Consumes: `requireTextArgument` from Task 5.
- Produces: `ord(s)` returns the code point of a single-character string; `strings.nx` exports `func char_code(s: string) -> int`.

- [ ] **Step 1: Write the failing test**

Create `internal/vm/stdlib_hygiene_test.go`:

```go
package vm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"noxy-vm/internal/stdlib"
)

var nativeRegistrationHelpers = map[string]bool{
	"DefineNative":                      true,
	"DefineNativeWithSignature":         true,
	"DefineContextualNative":            true,
	"DefineContextualNativeWithSignature": true,
}

// collectNativeRegistrations returns every native name registered in non-test
// Go sources under internal/vm, with the file:line of each registration.
func collectNativeRegistrations(t *testing.T) map[string][]string {
	t.Helper()
	sources, err := filepath.Glob(filepath.Join(".", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	registrations := make(map[string][]string)
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, source, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", source, parseErr)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !nativeRegistrationHelpers[selector.Sel.Name] {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			name, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil {
				return true
			}
			position := fileSet.Position(call.Pos())
			registrations[name] = append(registrations[name],
				filepath.Base(position.Filename)+":"+strconv.Itoa(position.Line))
			return true
		})
	}
	return registrations
}

func TestEveryNativeIsRegisteredExactlyOnce(t *testing.T) {
	registrations := collectNativeRegistrations(t)
	if len(registrations) == 0 {
		t.Fatal("no native registrations were found; the collector is broken")
	}
	for name, positions := range registrations {
		if len(positions) > 1 {
			t.Errorf("native %q is registered %d times: %s", name, len(positions), strings.Join(positions, ", "))
		}
	}
}

func TestNoShippedDebugOutput(t *testing.T) {
	goSources, err := filepath.Glob(filepath.Join(".", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range goSources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		content, readErr := os.ReadFile(source)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, marker := range []string{"DEBUG:", "Debug:"} {
			if strings.Contains(string(content), marker) {
				t.Errorf("%s contains the debug marker %q", source, marker)
			}
		}
	}

	entries, err := stdlib.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nx") {
			continue
		}
		content, readErr := stdlib.FS.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, marker := range []string{"DEBUG:", "Debug:"} {
			if strings.Contains(string(content), marker) {
				t.Errorf("stdlib/%s contains the debug marker %q", entry.Name(), marker)
			}
		}
	}
}

func TestEmbeddedStdlibSourcesAreValidUTF8(t *testing.T) {
	entries, err := stdlib.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nx") {
			continue
		}
		content, readErr := stdlib.FS.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		checked++
		if !utf8.Valid(content) {
			t.Errorf("stdlib/%s is not valid UTF-8", entry.Name())
		}
		if strings.ContainsRune(string(content), utf8.RuneError) {
			t.Errorf("stdlib/%s contains a U+FFFD replacement character", entry.Name())
		}
	}
	if checked == 0 {
		t.Fatal("no embedded .nx sources were checked; the walk is broken")
	}
}
```

Append the `ord` tests to `internal/vm/builtins_strings_test.go`:

```go
func TestOrdReturnsCodePoint(t *testing.T) {
	machine := New()
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{name: "ascii", input: "A", want: 65},
		{name: "latin1 supplement", input: "é", want: 233},
		{name: "cjk", input: "中", want: 20013},
		{name: "emoji", input: "\U0001F600", want: 128512},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "ord", value.NewString(test.input))
			if got.Type != value.VAL_INT || got.AsInt != test.want {
				t.Fatalf("ord(%q) = %#v, want %d", test.input, got, test.want)
			}
		})
	}
}

func TestOrdRoundTripsWithFromCharCode(t *testing.T) {
	machine := New()
	for _, code := range []int64{65, 233, 20013, 128512} {
		character := callBuiltin(t, machine, "strings_from_char_code", value.NewInt(code))
		back := callBuiltin(t, machine, "ord", character)
		if back.Type != value.VAL_INT || back.AsInt != code {
			t.Fatalf("ord(from_char_code(%d)) = %#v, want %d", code, back, code)
		}
	}
}

func TestOrdRequiresExactlyOneCharacter(t *testing.T) {
	machine := New()
	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "two characters", input: "ab"},
		{name: "two multibyte characters", input: "éé"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := requireBuiltin(t, machine, "ord").Invoke(machine, []value.Value{value.NewString(test.input)}); err == nil {
				t.Fatalf("ord(%q) did not fail", test.input)
			}
		})
	}
}

func TestCharCodeIsExportedByStrings(t *testing.T) {
	source := `use strings select *
test_report(char_code("é") == 233 && from_char_code(233) == "é")`
	captured := captureVMSource(t, strings.ReplaceAll(source, `é`, "é"))
	if captured.Type != value.VAL_BOOL || !captured.AsBool {
		t.Fatalf("char_code round trip = %#v, want true", captured)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vm/ -run 'TestEveryNativeIsRegistered|TestOrd|TestCharCode' -v`

Expected: FAIL —
`TestEveryNativeIsRegisteredExactlyOnce` reports `strings_contains` and `strings_replace` registered twice;
`TestOrdReturnsCodePoint` reports `195` for `é`;
`TestCharCodeIsExportedByStrings` reports `char_code` undefined.
`TestNoShippedDebugOutput` also fails; Task 7 fixes it.

- [ ] **Step 3: Delete the dead duplicate registrations**

In `internal/vm/builtins_strings.go`, delete the **second** registration of each name — the dead one, since `DefineNative` resolves to `DefineLocalIfAbsent` and the first wins:

- delete the `strings_contains` block at lines 159-166
- delete the `strings_replace` block at lines 167-175

Keep the registrations at lines 11 and 77, which are the live ones. If Task 5 already converted those two to `DefineContextualNative`, the surviving copy is that converted one; delete the untouched `DefineNative` leftovers regardless of position, so exactly one registration per name remains.

- [ ] **Step 4: Fix `ord`**

Replace the `ord` registration in `internal/vm/builtins_strings.go` (currently lines 149-158):

```go
	vm.DefineContextualNative("ord", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) != 1 {
			return value.NewNull(), fmt.Errorf("ord: expects exactly 1 argument, got %d", len(args))
		}
		if err := requireTextArgument("ord", args, 0); err != nil {
			return value.NewNull(), err
		}
		characters := []rune(args[0].String())
		if len(characters) != 1 {
			return value.NewNull(), fmt.Errorf("ord: expects a single character, got %d", len(characters))
		}
		return value.NewInt(int64(characters[0])), nil
	})
```

- [ ] **Step 5: Export `char_code`**

In `internal/stdlib/strings.nx`, add to the conversion section, immediately before `from_char_code`:

```noxy
func char_code(s: string) -> int
    return ord(s)
end
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run 'TestEveryNativeIsRegistered|TestOrd|TestCharCode' -v`

Expected: PASS.

- [ ] **Step 7: Run the full suite**

Run: `go test ./internal/... && go run cmd/noxy/main.go noxy_examples/run_all_tests.nx`

Expected: PASS, except `TestNoShippedDebugOutput` and `TestEmbeddedStdlibSourcesAreValidUTF8`, which Tasks 7 and 8 address.

- [ ] **Step 8: Commit**

```bash
git add internal/vm/builtins_strings.go internal/stdlib/strings.nx internal/vm/stdlib_hygiene_test.go internal/vm/builtins_strings_test.go
git commit -m "fix: one registration per native, and ord reads a character

strings_contains and strings_replace were each registered twice. Because
DefineNative resolves to DefineLocalIfAbsent, the first won and the second
was dead code that would silently swallow a correction.

ord returned the first UTF-8 byte while from_char_code is code point based,
so ord(from_char_code(233)) was 195 rather than 233. ord now returns the
code point of a single-character string and strings exports it as
char_code, documented as the inverse of from_char_code.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Remove shipped debug output

**Files:**
- Modify: `internal/vm/builtins_net.go:556`
- Modify: `internal/stdlib/http_client.nx:87`

**Interfaces:**
- Consumes: `TestNoShippedDebugOutput` from Task 6.
- Produces: no new names.

- [ ] **Step 1: Confirm the guard is failing**

Run: `go test ./internal/vm/ -run TestNoShippedDebugOutput -v`

Expected: FAIL, naming `builtins_net.go` and `stdlib/http_client.nx`.

- [ ] **Step 2: Remove the native debug print**

In `internal/vm/builtins_net.go`, inside the `net_send` registration, replace:

```go
		socket, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			fmt.Printf("DEBUG: net_send args[0] not map: %T %v\n", args[0].Obj, args[0].Obj)
			return value.NewNull(), nil
		}
```

with:

```go
		socket, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull(), nil
		}
```

Run `go build ./...` afterwards; if `fmt` is now unused in the file, remove the import.

- [ ] **Step 3: Remove the client debug print**

In `internal/stdlib/http_client.nx`, delete line 87 entirely:

```noxy
    print("Debug: request start " + url)
```

Also update the file's header comment on line 1 from `// stdlib/http_client.nx - Debug` to `// stdlib/http_client.nx - Cliente HTTP`, since the file is no longer a debug variant.

- [ ] **Step 4: Write the behavioral test**

Append to `internal/vm/stdlib_hygiene_test.go`:

```go
func TestHTTPClientDoesNotPrintOnRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		buffer := make([]byte, 4096)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, _ = conn.Read(buffer)
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"))
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	source := fmt.Sprintf(`use http_client select *
let r: ClientResponse = get("http://127.0.0.1:%d/")
test_report(r.ok)`, port)

	original := os.Stdout
	read, write, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	os.Stdout = write

	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	interpretErr := interpretVMSource(t, machine, source)

	_ = write.Close()
	os.Stdout = original
	printed, _ := io.ReadAll(read)

	if interpretErr != nil {
		t.Fatal(interpretErr)
	}
	if captured.Type != value.VAL_BOOL || !captured.AsBool {
		t.Fatalf("client request = %#v, want ok", captured)
	}
	if len(printed) != 0 {
		t.Fatalf("http client printed %q, want nothing", string(printed))
	}
}
```

Add `"fmt"`, `"io"`, `"net"`, `"time"`, and `"noxy-vm/internal/value"` to the import block of `internal/vm/stdlib_hygiene_test.go`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run 'TestNoShippedDebugOutput|TestHTTPClientDoesNotPrint' -v`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/vm/builtins_net.go internal/stdlib/http_client.nx internal/vm/stdlib_hygiene_test.go
git commit -m "fix: stop shipping debug output to end users

net_send printed a DEBUG line to stdout for a malformed argument, and the
HTTP client printed a Debug line on every single request, corrupting the
output of any program that used it. A guard test now fails if either
marker returns.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Repair source encoding

**Files:**
- Modify: `internal/stdlib/http.nx` (2 lines)
- Modify: `internal/stdlib/strings.nx` (6 lines)
- Modify: `internal/stdlib/time.nx` (15 lines)

**Interfaces:**
- Consumes: `TestEmbeddedStdlibSourcesAreValidUTF8` from Task 6.
- Produces: no new names. Comments only; no identifier, literal, or behavior changes.

- [ ] **Step 1: Repair `http.nx`**

| Line | From | To |
|---|---|---|
| 1 | `// stdlib/http.nx - M?dulo HTTP Unificado` | `// stdlib/http.nx - Módulo HTTP Unificado` |
| 8 | `// para que o usu?rio possa fazer: 'use http select *'` | `// para que o usuário possa fazer: 'use http select *'` |

- [ ] **Step 2: Repair `strings.nx`**

| Line | From | To |
|---|---|---|
| 6 | `// Busca e Verifica??o` | `// Busca e Verificação` |
| 28 | `// Transforma??o` | `// Transformação` |
| 50 | `// Manipula??o` | `// Manipulação` |
| 68 | `// Divis?o e Jun??o` | `// Divisão e Junção` |
| 78 | `// Verifica??o de Caracteres` | `// Verificação de Caracteres` |
| 100 | `// Convers?o` | `// Conversão` |

- [ ] **Step 3: Repair `time.nx`**

| Line | From | To |
|---|---|---|
| 10 | `    weekday: int      // 0=domingo, 6=s?bado` | `    weekday: int      // 0=domingo, 6=sábado` |
| 35 | `    // Passamos a defini??o da struct DateTime para a fun??o nativa` | `    // Passamos a definição da struct DateTime para a função nativa` |
| 39 | `// Retorna timestamp em milissegundos (para medi??es precisas)` | `// Retorna timestamp em milissegundos (para medições precisas)` |
| 44 | `// Pausa a execu??o por ms milissegundos` | `// Pausa a execução por ms milissegundos` |
| 50 | `// Convers?o` | `// Conversão` |
| 69 | `// Formata??o` | `// Formatação` |
| 73 | `// Formato: "YYYY-MM-DD HH:MM:SS" (padr?o)` | `// Formato: "YYYY-MM-DD HH:MM:SS" (padrão)` |
| 79 | `// %Y=ano, %m=m?s, %d=dia, %H=hora, %M=min, %S=seg` | `// %Y=ano, %m=mês, %d=dia, %H=hora, %M=min, %S=seg` |
| 109 | `// Aritm?tica` | `// Aritmética` |
| 122 | `// Diferen?a entre dois timestamps (em segundos)` | `// Diferença entre dois timestamps (em segundos)` |
| 127 | `// Diferen?a como Duration` | `// Diferença como Duration` |
| 133 | `// Compara??o` | `// Comparação` |
| 147 | `// Utilit?rios` | `// Utilitários` |
| 155 | `// Dias no m?s` | `// Dias no mês` |
| 165 | `// Nome do m?s` | `// Nome do mês` |

- [ ] **Step 4: Verify no mojibake remains**

Run: `grep -n "?" internal/stdlib/*.nx`

Expected: no output, or only lines where a question mark is genuinely intended. Inspect any remaining match before accepting it.

- [ ] **Step 5: Run the guard and the full suite**

Run: `go test ./internal/vm/ -run TestEmbeddedStdlibSourcesAreValidUTF8 -v && go test ./internal/... && go run cmd/noxy/main.go noxy_examples/run_all_tests.nx`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/stdlib/http.nx internal/stdlib/strings.nx internal/stdlib/time.nx
git commit -m "fix: restore accented characters in stdlib comments

Twenty-three comment lines across http.nx, strings.nx, and time.nx lost
their accents to a lossy encoding pass, leaving a literal question mark in
place of each one. Comments only; no identifier, literal, or behavior is
touched.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Documentation

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Read the current conventions**

Run: `grep -n "substring\|## " docs/NOXY_LANGUAGE_SPEC.md | head -40` and `head -40 CHANGELOG.md`

Note the section numbering of the language spec and the changelog's heading style so the additions match.

- [ ] **Step 2: Document string indexing**

Add to the string section of `docs/NOXY_LANGUAGE_SPEC.md`:

```markdown
### Indexação de strings

Uma `string` é indexada por **caractere** (code point Unicode), não por byte.
`length`, `substring`, `char_at`, `index_of`, `slice` e `reverse` usam todos a
mesma unidade, então compõem entre si:

```noxy
let nome: string = substring(linha, 0, index_of(linha, ":"))
```

`length("café")` é `4`, não `5`.

Um code point não é sempre um caractere percebido pelo usuário: `é` pode ser um
code point ou dois, e um emoji com modificador é vários. Só um modelo de
grapheme cluster seria exato, e ele não oferece índice inteiro em tempo
constante. Noxy adota a aproximação por code point, como Python.

`bytes` é o oposto: indexado por **octeto**, através de `length`, `slice` e
acesso por elemento. As funções de `strings` recusam um `bytes` e apontam
`to_str`, que é a ponte explícita entre os dois tipos.
```

- [ ] **Step 3: Document the conversion contract**

Add to `docs/NOXY_LANGUAGE_SPEC.md`:

```markdown
### Conversões numéricas

`to_int` e `to_float` **levantam erro de runtime** quando a conversão é
impossível. Use-os quando uma falha seria um bug do programa.

```noxy
to_int(5.9)      // 5, truncamento em direção a zero
to_int("5")      // 5
to_int("5.5")    // erro: uma string decimal não é um inteiro
to_int("abc")    // erro
to_int(true)     // erro: bool não é número em Noxy
```

Para entrada não confiável, use a forma `_result`, do módulo `convert`, que
nunca levanta:

```noxy
use convert select *

let porta: IntResult = to_int_result(getenv("PORT").value)
if porta.ok then
    print("porta " + to_str(porta.value))
else
    print("PORT inválida: " + porta.error)
end
```

Essa é a mesma convenção de `io.close` / `io.close_result`. Como funções Noxy
têm retorno único, o struct de resultado ocupa o lugar do par `value, err` do
Go.

Validar antes de converter não é uma alternativa correta: `is_digit` aceita
`"9999999999999999999"`, que estoura `int64`, e não há como checar o intervalo
sem converter. Não existe `is_float`.
```

- [ ] **Step 4: Add the changelog entry**

Add to `CHANGELOG.md`, matching its existing format:

```markdown
### Changed (BREAKING)
- **`to_int` e `to_float` levantam erro** em vez de devolver `0` / `0.0` quando
  a conversão é impossível. `to_int("abc")` era indistinguível de
  `to_int("0")`. Migração: chamadas sobre entrada não confiável passam a usar
  `to_int_result` / `to_float_result` do módulo `convert`, com ramo explícito
  de falha.
- **`index_of` devolve índice em caractere**, não em byte, alinhado a
  `substring`, `char_at`, `length` e `slice`. Texto ASCII não é afetado.
- **Funções de `strings` recusam `bytes`** e apontam `to_str`. Antes operavam
  sobre a forma de exibição `b"..."`.
- **`ord` devolve o code point** de uma string de um caractere e exige
  exatamente um caractere. Antes devolvia o primeiro byte UTF-8.

### Added
- **Módulo `convert`** com `to_int_result`, `to_float_result`, `IntResult` e
  `FloatResult`.
- **`strings.char_code(s)`**, inverso de `from_char_code(code)`.
- **Guards de arquitetura**: nenhum native registrado duas vezes, nenhum marcador
  de debug em fonte de produção, fontes embarcados da stdlib em UTF-8 válido.

### Fixed
- **`parse_url` cortava host e path no lugar errado** para autoridade com
  caractere não-ASCII: `http://münchen.de/path` devolvia host `münchen.de/` e
  path `path`.
- **`net_send` e o cliente HTTP imprimiam linhas de debug** em stdout; o cliente
  a cada requisição.
- **`strings_contains` e `strings_replace` estavam registrados duas vezes**, com
  a segunda cópia inalcançável.
- **23 linhas de comentário da stdlib** tiveram os acentos restaurados.
```

- [ ] **Step 5: Full validation**

```bash
go build ./...
go vet ./...
go test ./internal/...
go test -race ./internal/vm/
go run cmd/noxy/main.go noxy_examples/run_all_tests.nx
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: all pass, with the example suites at the count recorded in Task 2 Step 11 plus one for `test_convert.nx`.

- [ ] **Step 6: Commit**

```bash
git add docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md
git commit -m "docs: document conversion and string indexing contracts

Strict to_int/to_float with the _result companion, string indexing by
character with the code-point approximation stated explicitly, and the
string/bytes boundary bridged by to_str.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Pull request

- [ ] **Step 1: Push the branch**

```bash
git push -u origin fix/stdlib-reliability
```

- [ ] **Step 2: Open the PR against `develop`**

```bash
gh pr create --base develop --title "fix/stdlib-reliability - Make silent failures loud" --body "$(cat <<'EOF'
## Summary
- Make `to_int` and `to_float` raise instead of returning a sentinel, and add `to_int_result` / `to_float_result` for input whose failure is expected data rather than a bug.
- Make `index_of` return a character index so it composes with `substring`, `char_at`, `length`, and `slice`, fixing a live defect in `parse_url`.
- Make `strings` functions reject `bytes` instead of silently operating on the `b"..."` display form, establishing the string/bytes boundary.
- Stop shipping debug output to end users, remove two dead native registrations, make `ord` read a character, and restore 23 comment lines whose accents were lost to a lossy encoding pass.
- Add three architecture guards so each class of defect fails the build if it returns.

## Components
- `internal/vm/builtins_convert.go` (new): shared conversion logic, strict `to_int`/`to_float`, `convert_to_int_result`/`convert_to_float_result`.
- `internal/stdlib/convert.nx` (new): `IntResult`, `FloatResult`, `to_int_result`, `to_float_result`.
- `internal/vm/builtins_strings.go`: character-based `index_of`, `bytes` rejection across text parameters, `ord` code point, dead duplicate registrations deleted.
- `internal/vm/builtins_net.go`, `internal/stdlib/http_client.nx`: debug output removed.
- `internal/stdlib/http_parser.nx`, plus four examples: call sites parsing untrusted input migrated to the `_result` form.
- `internal/stdlib/http.nx`, `strings.nx`, `time.nx`: comment encoding repaired.
- `internal/vm/stdlib_hygiene_test.go` (new): duplicate-registration, debug-output, and UTF-8 guards.
- `docs/NOXY_LANGUAGE_SPEC.md`, `CHANGELOG.md`.

## Related Issues
- No linked Jira card. Prerequisite for `feat/http-streaming-server`, whose `Content-Length` guard depended on a check that cannot be written correctly in Noxy today.

## Test Plan
- [x] Implementação
- [x] Testes unitários passando (`go test ./internal/...`)
- [x] Race detector limpo (`go test -race ./internal/vm/`)
- [x] Build e vet passando
- [x] Testado integrado (`run_all_tests.nx` e `run_all_tests_concurrent.nx`)
- [ ] Revisão de código

## Breaking Changes
`to_int` and `to_float` now raise for input they previously converted to `0`. Migration: a call that may receive non-numeric input becomes the `_result` form with an explicit failure branch. All 20 call sites in the repository were audited; the four that used the `0` sentinel as their error signal are migrated in this PR.

## Follow-up Work
- A structured error-catching construct, building on the unwind machine from PR #18.
- Multiple return values, which would allow Go's `value, err` shape directly.
- Character indexing is `O(n)` per access, so `char_at` in a loop is `O(n²)`; a caching or iterator-based fix is a separate optimization subproject.
- Update `docs/superpowers/plans/2026-08-15-http-streaming-server.md` on the `feat/http-streaming-server` branch to use `to_int_result` in `resolve_body_length` instead of the `is_digit` + length-19 guard, which does not actually prevent overflow.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Report the PR URL to the user**

---

## Notes for the implementer

**Why the task order cannot change.** Task 3 makes `to_int` strict. If it ran before Task 2, the example suite would break in ways that look like implementation bugs but are really unmigrated callers. Additive first, migrate second, flip third — every task leaves a green tree, so a red tree always means the current task is wrong.

**Which duplicate registration is live.** `DefineNative` and `DefineNativeWithSignature` resolve to `DefineLocalIfAbsent`, so the **first** wins. `DefineContextualNative` resolves to `SetGlobal`, so the **last** wins. This matters in Task 5, which converts several natives from the first form to the second: if a stale `DefineNative` for the same name survives elsewhere, whether the new definition takes effect depends on relative order. Task 6's guard is what makes this impossible to get wrong going forward.

**Why `is_digit` is not a substitute for strict conversion.** `is_digit("9999999999999999999")` is true and the value overflows `int64`. Checking the range without converting requires comparing against `9223372036854775807`, which requires converting. There is no `is_float` at all. This is why the strict form ships with the `_result` companion rather than a documented "validate first" recipe.

**Byte versus character.** `strings.Index`, `strings.Contains`, and `strings.TrimSpace` operate on bytes and are safe to keep, because they return booleans or substrings rather than offsets. Only `index_of` leaked an offset, and only that one needed translating. Do not convert the others to rune-based scanning; it would be slower and would change nothing observable.

**Contextual natives are testable the same way.** `NewNative` and `NewContextualNative` both produce `Value{Type: VAL_NATIVE, Obj: *ObjNative}`, differing only in which of the `Fn` and `Contextual` fields is set, and `ObjNative.Invoke` dispatches between them. So `requireBuiltin` and `callBuiltin` keep working for every native this plan converts, and only the tests that assert an error need `Invoke` directly.

**Registration helper by capability.** A native that must raise has to be registered with `DefineContextualNative`, since `NativeFunc` returns a bare `value.Value` with no error channel. That is why Tasks 3, 5, and 6 convert registrations rather than editing bodies in place.
