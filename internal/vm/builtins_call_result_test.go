// internal/vm/builtins_call_result_test.go
package vm

import (
	"errors"
	"strings"
	"testing"

	"noxy-vm/internal/value"
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

// TestFailureMapMergesInnerCausesWithSiblingsOnPromotion cobre um bug real:
// no caso cleanup-first, deferredFailureMap promove o primeiro DeferredError
// a falha primaria. Se a Cause desse promovido ja e um *UnwindError com seu
// proprio Primary+Deferred (i.e. a falha promovida ja tinha causes proprias
// aninhadas), o merge com os siblings (falhas diferidas posteriores no mesmo
// frame) precisa PRESERVAR essas causes internas e so anexar os siblings por
// cima — nunca substituir. Constroi a arvore de erro diretamente em Go (via
// failureMap) porque replicar "defer cujo Cause e um UnwindError aninhado"
// de forma confiavel a partir de fonte Noxy exigiria coordenar timing de
// falhas concorrentes de defer; a arvore de erro e o dado relevante aqui, nao
// o caminho de execucao que a produz.
func TestFailureMapMergesInnerCausesWithSiblingsOnPromotion(t *testing.T) {
	innerSibling := DeferredError{
		Registration: SourceLocation{File: "inner.nx", Line: 3},
		Cause:        errors.New("inner sibling failure"),
	}
	promoted := DeferredError{
		Registration: SourceLocation{File: "outer.nx", Line: 5},
		Cause: &UnwindError{
			Primary:  errors.New("inner primary failure"),
			Deferred: []DeferredError{innerSibling},
		},
	}
	outerSibling := DeferredError{
		Registration: SourceLocation{File: "outer.nx", Line: 9},
		Cause:        errors.New("outer sibling failure"),
	}
	unwind := &UnwindError{
		Primary:  nil,
		Deferred: []DeferredError{promoted, outerSibling},
	}

	result := failureMap(unwind)
	mapping, ok := result.Obj.(*value.ObjMap)
	if !ok {
		t.Fatalf("failureMap did not return a map: %#v", result)
	}
	message, _ := mapping.Get("message")
	if text, _ := message.Obj.(string); text != "inner primary failure" {
		t.Fatalf("promoted primary should keep the inner UnwindError's message, got %q", text)
	}

	causesValue, ok := mapping.Get("causes")
	if !ok {
		t.Fatalf("promoted failure missing causes key")
	}
	array, ok := causesValue.Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("causes is not an array: %#v", causesValue)
	}
	if len(array.Elements) != 2 {
		t.Fatalf("want 2 causes (1 inner + 1 outer sibling), got %d: %#v", len(array.Elements), array.Elements)
	}

	causeMessage := func(v value.Value) string {
		m, ok := v.Obj.(*value.ObjMap)
		if !ok {
			t.Fatalf("cause is not a map: %#v", v)
		}
		msg, _ := m.Get("message")
		text, _ := msg.Obj.(string)
		return text
	}
	if got := causeMessage(array.Elements[0]); got != "inner sibling failure" {
		t.Fatalf("first cause should be the promoted failure's own inner cause, got %q", got)
	}
	if got := causeMessage(array.Elements[1]); got != "outer sibling failure" {
		t.Fatalf("second cause should be the outer sibling, got %q", got)
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
