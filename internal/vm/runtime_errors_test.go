package vm

import (
	"errors"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
	"strings"
	"testing"
)

func TestRuntimeErrorPreservesCause(t *testing.T) {
	sentinel := errors.New("native sentinel")
	err := &RuntimeError{
		Location: SourceLocation{File: "main.nx", Line: 4},
		Message:  "native failed",
		Cause:    sentinel,
	}

	if !errors.Is(err, sentinel) || err.Error() != "[main.nx:line 4] native failed: native sentinel" {
		t.Fatalf("error=%q unwrap=%v", err, errors.Is(err, sentinel))
	}
}

func TestUnwindErrorPreservesLIFOOrderAndNestedCause(t *testing.T) {
	original := errors.New("original")
	nested := &UnwindError{Deferred: []DeferredError{{
		Registration: SourceLocation{File: "cleanup.nx", Line: 9},
		Cause:        errors.New("nested"),
	}}}
	err := &UnwindError{Primary: original, Deferred: []DeferredError{
		{Registration: SourceLocation{File: "main.nx", Line: 8}, Cause: nested},
		{Registration: SourceLocation{File: "main.nx", Line: 7}, Cause: errors.New("older")},
	}}

	if !errors.Is(err, original) {
		t.Fatal("primary cause lost")
	}
	text := err.Error()
	if strings.Index(text, "line 8") > strings.Index(text, "line 7") || strings.Count(text, "cleanup.nx:line 9") != 1 {
		t.Fatalf("bad nested rendering:\n%s", text)
	}
}

func TestNilUnwindErrorUnwrapIsSafe(t *testing.T) {
	var unwind *UnwindError

	t.Run("direct", func(t *testing.T) {
		if causes := unwind.Unwrap(); causes != nil {
			t.Fatalf("causes=%v, want nil", causes)
		}
	})

	t.Run("errors Is", func(t *testing.T) {
		if errors.Is(unwind, errors.New("sentinel")) {
			t.Fatal("typed-nil unwind unexpectedly matched sentinel")
		}
	})
}

func TestNativeRuntimeErrorPreservesNativeCause(t *testing.T) {
	sentinel := errors.New("native sentinel")
	machine := New()
	native := value.NewContextualNative("explode", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), sentinel
	})
	machine.push(native)
	source := &chunk.Chunk{FileName: "native.nx", Lines: []int{17}}

	ok, err := machine.callValue(native, 0, source, 1)
	if ok || !errors.Is(err, sentinel) {
		t.Fatalf("ok=%v errors.Is(err, sentinel)=%v", ok, errors.Is(err, sentinel))
	}
	if got, want := err.Error(), "[native.nx:line 17] native 'explode' failed: native sentinel"; got != want {
		t.Fatalf("error=%q, want %q", got, want)
	}
}

func TestNativeValidationErrorPreservesCause(t *testing.T) {
	machine := New()
	native := value.NewNativeWithSignature("needs_ref", value.NativeSignature{
		Arity:      1,
		Params:     []value.ParamInfo{{IsRef: true, TypeName: "ref int"}},
		ReturnType: "void",
	}, func([]value.Value) value.Value {
		return value.NewNull()
	})
	machine.push(native)
	machine.push(value.NewInt(1))

	ok, err := machine.callValue(native, 1, &chunk.Chunk{FileName: "native.nx", Lines: []int{18}}, 1)
	if ok || err == nil {
		t.Fatalf("ok=%v error=%v, want native validation failure", ok, err)
	}
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) || runtimeErr.Cause == nil {
		t.Fatalf("error did not preserve validation cause: %T %[1]v", err)
	}
	if got, want := runtimeErr.Cause.Error(), "function 'needs_ref' argument 1: expected ref int, got int"; got != want {
		t.Fatalf("cause=%q, want %q", got, want)
	}
}

func TestImportRuntimeErrorPreservesModuleCause(t *testing.T) {
	source := &chunk.Chunk{
		// OP_IMPORT's constant-pool index is a 16-bit big-endian operand (see
		// emitOpWithConstantIndex), so constant 0 is two zero bytes, not one.
		Code:      []byte{byte(chunk.OP_IMPORT), 0, 0},
		Constants: []value.Value{value.NewString("missing_runtime_error_module")},
		Lines:     []int{6, 6, 6},
		FileName:  "importer.nx",
	}
	err := New().Interpret(source)
	if err == nil {
		t.Fatal("missing import succeeded")
	}
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) || runtimeErr.Cause == nil {
		t.Fatalf("error did not preserve a structured import cause: %T %[1]v", err)
	}
	if got, want := err.Error(), "[importer.nx:line 6] failed to import module 'missing_runtime_error_module': module not found: missing_runtime_error_module"; got != want {
		t.Fatalf("error=%q, want %q", got, want)
	}
}

// Recursao infinita esgota o teto de FramesMax (100 000 frames) dentro de
// call_result: sem corte, captureNoxyStack renderizaria uma linha por frame —
// uma string de ~16 MB retida em r.failure.stack (medido na issue #56). O
// corte para em maxCapturedStackFrames e deixa a marca "frames omitted".
func TestCallResultStackCapBoundsDeepOverflow(t *testing.T) {
	source := `
use errors select *
func forever() -> int
    return forever()
end
let r: any = call_result(forever)
test_report(r.failure.stack)
`
	reported := captureVMSource(t, source)
	stack, _ := reported.Obj.(string)
	if len(stack) >= 64_000 {
		t.Fatalf("stack capturado tem %d bytes, esperado < 64000 (corte em %d frames nao aplicado?)", len(stack), maxCapturedStackFrames)
	}
	if !strings.Contains(stack, "frames omitted") {
		t.Fatalf("stack capturado sem a marca de omissao: %q", stack[:200])
	}
}

// Um erro raso (bem abaixo de maxCapturedStackFrames) nao aciona o corte —
// nenhuma linha "frames omitted" deve aparecer, e o frame mais externo
// (raso) segue presente.
func TestCallResultStackHasNoOmissionForShallowOverflow(t *testing.T) {
	source := `
use errors select *
func rec(n: int) -> int
    if n == 0 then return to_int("x") end
    return rec(n - 1)
end
func raso() -> int
    return rec(20)
end
let r: any = call_result(raso)
test_report(r.failure.stack)
`
	reported := captureVMSource(t, source)
	stack, _ := reported.Obj.(string)
	if strings.Contains(stack, "frames omitted") {
		t.Fatalf("stack raso nao deveria ser cortado: %q", stack)
	}
	if !strings.Contains(stack, "in raso") {
		t.Fatalf("stack sem o frame mais externo: %q", stack)
	}
}
