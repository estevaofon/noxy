// internal/vm/builtins_call_result_test.go
package vm

import (
	"strings"
	"testing"
)

func TestErrorsModuleShapes(t *testing.T) {
	source := `
use errors select *

let f: Failure = Failure("runtime", "boom", "st", [])
let nested: Failure = Failure("runtime", "outer", "st", [f])
let r: CallResult = CallResult(true, 42, null)
test_report(nested.causes[0].message + "|" + to_str(r.ok) + "|" + to_str(r.value))
`
	reported := captureVMSource(t, source)
	text, ok := reported.Obj.(string)
	if !ok || text != "boom|true|42" {
		t.Fatalf("unexpected report: %#v", reported)
	}
}

func TestCallResultOkPaths(t *testing.T) {
	source := `
use errors select *

func dobro(x: int) -> int
    return x * 2
end

func nada()
end

struct P
    x: int
end

let a: CallResult = call_result(dobro, 21)
let b: CallResult = call_result(to_int, "5")
let c: CallResult = call_result(P, 7)
let d: CallResult = call_result(nada)
let inst: any = c.value
test_report(to_str(a.ok) + "|" + to_str(a.value) + "|" + to_str(b.value) + "|" + to_str(inst.x) + "|" + to_str(d.ok) + "|" + to_str(d.value == null) + "|" + to_str(a.failure == null))
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	if text != "true|42|5|7|true|true|true" {
		t.Fatalf("unexpected report: %q", text)
	}
}

func TestCallResultEnvelopeIsMap(t *testing.T) {
	source := `
let r: any = call_result(to_int, "5")
test_report(fmt("%T", r))
`
	reported := captureVMSource(t, source)
	if text, _ := reported.Obj.(string); text != "map" {
		t.Fatalf("envelope should be a map at the dynamic boundary, got %q", text)
	}
}

func TestCallResultCapturesRuntimeError(t *testing.T) {
	source := `
use errors select *

func quebra(texto: string) -> int
    return to_int(texto)
end

let r: CallResult = call_result(quebra, "abc")
let depois: int = 40 + 2
test_report(to_str(r.ok) + "|" + r.failure.kind + "|" + r.failure.message + "|" + to_str(length(r.failure.causes)) + "|" + to_str(r.value == null) + "|" + to_str(depois))
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	parts := strings.Split(text, "|")
	if len(parts) != 6 || parts[0] != "false" || parts[1] != "runtime" || parts[3] != "0" || parts[4] != "true" || parts[5] != "42" {
		t.Fatalf("unexpected report: %q", text)
	}
	if !strings.Contains(parts[2], "cannot convert") || strings.Contains(parts[2], "use to_int_result") {
		t.Fatalf("message wrong or advisory suffix leaked: %q", parts[2])
	}
}

func TestCallResultFailureStackExcludesBoundary(t *testing.T) {
	source := `
func fundo() -> int
    return to_int("x")
end
func meio() -> int
    return fundo()
end
let r: any = call_result(meio)
test_report(r.failure.stack)
`
	reported := captureVMSource(t, source)
	stack, _ := reported.Obj.(string)
	if !strings.Contains(stack, "in fundo") || !strings.Contains(stack, "in meio") {
		t.Fatalf("stack missing inner frames: %q", stack)
	}
	if strings.Contains(stack, "call_result") {
		t.Fatalf("stack must stop before the boundary frame: %q", stack)
	}
}

func TestCallResultCapturesStackOverflow(t *testing.T) {
	source := `
func infinita() -> int
    return infinita()
end
let r: any = call_result(infinita)
test_report(to_str(r.ok) + "|" + r.failure.kind + "|" + r.failure.message)
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	if !strings.HasPrefix(text, "false|runtime|") || !strings.Contains(text, "stack overflow") {
		t.Fatalf("frame exhaustion should be captured: %q", text)
	}
}

func TestCallResultMisuseRaisesSynchronously(t *testing.T) {
	cases := []struct {
		name, source, wantErr string
	}{
		{"non-callable", `call_result(42)`, "call_result expects a callable"},
		{"closure arity", `
func soma(a: int, b: int) -> int
    return a + b
end
call_result(soma, 1)`, "expected 2 arguments but got 1"},
		{"constructor arity", `
struct P
    x: int
end
call_result(P, 1, 2)`, "expected 1 arguments for struct P"},
		{"no arguments at all", `call_result()`, "call_result expects a callable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machine := New()
			err := interpretVMSource(t, machine, tc.source)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want synchronous error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}
