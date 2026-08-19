# call_result Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `call_result(fn, ...args)` synchronous error boundary: a global native that invokes any callable and converts a runtime failure that unwinds out of it into a `CallResult` envelope, per the approved design.

**Architecture:** One new native (`call_result`) built on the VM's existing reentrant invocation machinery (`prepareDeferredCall`/`prepareTaskCall` for synchronous validation, `callPreparedValue` + `vm.run(minFrameCount, &result)` for execution, `UnwindError`/`DeferredError` for failure structure). The envelope is physically a **map** at the dynamic boundary (the `IntResult` precedent), with `stdlib/errors.nx` declaring the `Failure`/`CallResult` shapes for typed annotation. The advisory suffix in `to_int`/`to_float` moves out of the raised message into the fatal diagnostic via a new `AdvisedError` wrapper.

**Tech Stack:** Go (VM in `internal/vm`), Noxy stdlib modules (embedded via `internal/stdlib/embed.go`), Go tests driven by Noxy source strings.

**Spec:** `docs/superpowers/specs/2026-08-19-call-result-design.md` — the plan argues from it; executors read both. The normative language-spec text to merge lives in that file's §2.

## Global Constraints

- Branch: `feat/call-result`, based on `develop` (project convention: PRs target `develop`).
- The envelope is a **map** built with `value.NewMapWithData` — never a real struct instance. Mirror `taskOutcomeEnvelope` (`internal/vm/builtins_tasks.go:155-174`) for construction and RC behavior.
- Envelope fields are exactly `ok`, `value`, `failure`; failure fields exactly `kind`, `message`, `stack`, `causes`. `failure` is Noxy `null` when ok; `value` is Noxy `null` on failure and for void returns.
- `kind` is `"runtime"` or `"panic"` — same vocabulary as the task boundary (`builtins_tasks.go:128-148`).
- Misuse (non-callable, arity/mode/constructor-field mismatch where metadata exists) is a **synchronous error returned by the native** — never captured in the envelope.
- `task_await`'s envelope is **not** touched in this change.
- `internal/stdlib/result.nx` stays functional this release (deprecation comment only).
- All existing tests must keep passing (`go test ./...` on Windows). Two test files will need updates when the builtin list grows / messages change: `internal/vm/builtins_registry_test.go`, `internal/vm/builtins_convert_test.go` — updating their expectations is in-scope, weakening them is not.
- Commit style: conventional commits with scope, PT description (match `git log` style, e.g. `feat(vm): ...`).

## Pre-verified facts (do not re-derive)

- `prepareDeferredCall` (`internal/vm/defer.go:15-71`) validates closures (arity + parameter modes, retains args), signed natives (signature arity + modes, eager-copies args), unsigned natives (no validation), struct constructors (field count + `validateStructConstructorArguments`). It **rejects** a bare `*value.ObjFunction` (non-closure); `prepareTaskCall` (`internal/vm/task_execution.go:22-36`) shows the normalization: wrap an `ObjFunction` with `UpvalueCount == 0` into an `ObjClosure`.
- `invokePreparedCall` (`internal/vm/defer.go:142-187`) is the synchronous same-VM invocation pattern: save `base := vm.stackTop`, push callee+args, `callPreparedValue`, then if `vm.frameCount > ownerFrameCount` run `vm.run(ownerFrameCount+1, nil)`; a Go `defer` zeroes the temp stack slots, restores `stackTop`, and releases retained closure args. On error, `vm.run`'s own deferred handler (`internal/vm/executor.go:52-59`) has already unwound to `minFrameCount-1`, running Noxy defers and aggregating `DeferredError`s into `*UnwindError`.
- `vm.run(minFrameCount, terminalResult)` stores the returning function's value through `terminalResult` when the frame count drops below `minFrameCount` (`executor.go:1200-1218`, `OP_RETURN`); `executePreparedTaskCall` (`task_execution.go:110`) is the precedent for capturing it.
- For native and constructor callees, `callPreparedValue` pushes the result on the VM stack without creating a frame (`internal/vm/calls.go:78-112`); read it with `vm.peek(0)` when `vm.frameCount == ownerFrameCount`.
- Frame exhaustion is an ordinary runtime error `"stack overflow"` (`calls.go:121-122`, `FramesMax = 64` in `vm.go:12`) — capturable by design.
- `UnwindError{Primary, Deferred []DeferredError}`; `DeferredError{Registration SourceLocation, Cause error}`; `RuntimeError{Location, Message, Cause, Stack}`; `deepestRuntimeStack(err)` extracts the Noxy stack (`internal/vm/runtime_errors.go`). Cleanup-first failure arrives as `UnwindError{Primary: nil, Deferred: [...]}` (`unwind.go:110-119` + `finalizeCurrentFrame`).
- Native errors are wrapped by `callNative` as `RuntimeError{Message: "native 'X' failed", Cause: err}` (`calls.go:104-112`).
- Fatal runtime print sites: `cmd/noxy/main.go:271` and `:338` (`fmt.Printf("Runtime error: %s\n", err)`).
- stdlib modules are embedded by `//go:embed *.nx` (`internal/stdlib/embed.go`) and resolved by filename in `loadModule` (`internal/vm/modules.go:101`) — dropping `errors.nx` into `internal/stdlib/` is the whole registration.
- **Audit result (design §7, callback-native audit):** grep shows the only callers of the invocation machinery are defer unwinding, tasks, and the call opcodes themselves — no native invokes a Noxy callback synchronously today (the HTTP server is pure Noxy). The audit deliverable is the grep evidence recorded in the spec text plus the defer/nested-frame propagation tests below; there is nothing to fix.
- Test harness: `interpretVMSource(t, machine, src) error`, `captureVMSource(t, src) value.Value` (uses a `test_report` native), `machine := New()` (`internal/vm/vm_test_helpers_test.go`). Stdlib imports work in tests via the embedded FS.

---

### Task 1: `errors.nx` stdlib module (Failure / CallResult shapes)

**Files:**
- Create: `internal/stdlib/errors.nx`
- Test: `internal/vm/builtins_call_result_test.go` (new file, first test in it)

**Interfaces:**
- Produces: Noxy module `errors` exporting `struct Failure { kind: string, message: string, stack: string, causes: Failure[] }` and `struct CallResult { ok: bool, value: any, failure: Failure }`. Later tasks' Noxy test sources annotate with these.

- [ ] **Step 1: Write the failing test**

```go
// internal/vm/builtins_call_result_test.go
package vm

import (
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm/ -run TestErrorsModuleShapes -count=1`
Expected: FAIL — module `errors` not found (import error).

- [ ] **Step 3: Write the module**

```noxy
// stdlib/errors.nx - Formas do envelope da fronteira sincrona de erro.
//
// call_result e um nativo global; estes structs dao nome e contrato de campos
// ao envelope que ele devolve. Fisicamente o envelope e um MAP na fronteira
// dinamica (mesmo precedente de IntResult em convert.nx): fmt("%T") reporta
// "map" e ele nao compara igual a um CallResult construido a mao. A anotacao
// tipada (`let r: CallResult = ...`) vale pelo contrato de campos.

struct Failure
    kind: string       // "runtime" | "panic"
    message: string
    stack: string
    causes: Failure[]  // falhas de defer agregadas no unwinding, ordem LIFO
end

struct CallResult
    ok: bool
    value: any         // retorno de fn; null para void e em falha
    failure: Failure   // null quando ok
end
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vm/ -run TestErrorsModuleShapes -count=1`
Expected: PASS. (This also locks in the §5 self-reference-through-array fact: `causes: Failure[]` compiles and nests.)

- [ ] **Step 5: Check stdlib hygiene test still passes**

Run: `go test ./internal/vm/ -run TestStdlib -count=1` (then the full package if unsure which test enumerates modules: `go test ./internal/vm/ -count=1`). If a hygiene/registry test enumerates stdlib modules, add `errors` to its expected list — do not weaken the test.

- [ ] **Step 6: Commit**

```bash
git add internal/stdlib/errors.nx internal/vm/builtins_call_result_test.go
git commit -m "feat(stdlib): modulo errors com as formas Failure e CallResult da fronteira call_result"
```

---

### Task 2: `AdvisedError` — advisory suffix out of the capturable message

**Files:**
- Modify: `internal/vm/runtime_errors.go` (append type at end)
- Modify: `internal/vm/builtins_convert.go:133-153` (`to_int`, `to_float`)
- Modify: `cmd/noxy/main.go:271` and `:338`
- Test: `internal/vm/builtins_convert_test.go` (update message assertions), new test in same file

**Interfaces:**
- Produces: exported `vm.AdvisedError{Err error, Advice string}` with `Error()` returning only `Err.Error()` and `Unwrap()` returning `Err`. Task 5's failure envelope gets clean messages for free (nothing renders `Advice` except the fatal printer).

- [ ] **Step 1: Write the failing test**

```go
// append to internal/vm/builtins_convert_test.go
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
```

(Add `"errors"` and `"strings"` to the test file's imports if absent.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm/ -run TestToIntAdvisedError -count=1`
Expected: FAIL — suffix present in message / `AdvisedError` undefined.

- [ ] **Step 3: Implement**

Append to `internal/vm/runtime_errors.go`:

```go
// AdvisedError separa o conselho de uso da mensagem de erro capturável: o
// texto do erro fica limpo (Failure.message, task failure map), e só a saída
// fatal do topo (cmd/noxy) imprime o Advice.
type AdvisedError struct {
	Err    error
	Advice string
}

func (err *AdvisedError) Error() string { return err.Err.Error() }
func (err *AdvisedError) Unwrap() error { return err.Err }
```

In `internal/vm/builtins_convert.go`, replace the two raising returns:

```go
// to_int (line ~139):
return value.NewNull(), &AdvisedError{
	Err:    fmt.Errorf("to_int: %w", convertErr),
	Advice: "use to_int_result to handle failure",
}
// to_float (line ~150):
return value.NewNull(), &AdvisedError{
	Err:    fmt.Errorf("to_float: %w", convertErr),
	Advice: "use to_float_result to handle failure",
}
```

In `cmd/noxy/main.go` (both sites, lines 271 and 338), after the existing print:

```go
fmt.Printf("Runtime error: %s\n", err)
var advised *vm.AdvisedError
if errors.As(err, &advised) {
	fmt.Printf("hint: %s\n", advised.Advice)
}
```

(Add `"errors"` to main.go imports.)

- [ ] **Step 4: Fix stale assertions**

Run: `go test ./... -count=1` and `Grep "use to_int_result to handle failure"` across the repo. Update any test asserting the old inline suffix (expected: `internal/vm/builtins_convert_test.go`; possibly `native_signatures_test.go` messages). Keep asserting the *clean* message (`to_int: cannot convert ...`).

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/vm/ -count=1` then `go build ./...`
Expected: PASS, builds.

- [ ] **Step 6: Commit**

```bash
git add internal/vm/runtime_errors.go internal/vm/builtins_convert.go cmd/noxy/main.go internal/vm/builtins_convert_test.go
git commit -m "refactor(vm): sufixo de conselho de to_int/to_float sai da mensagem capturavel via AdvisedError; hint impresso so no fatal"
```

---

### Task 3: Boundary preparation — synchronous misuse validation

**Files:**
- Create: `internal/vm/builtins_call_result.go`
- Modify: `internal/vm/builtins.go` (or wherever `defineTaskBuiltins()` is wired — grep `defineTaskBuiltins()`; add `defineCallResultBuiltins()` beside it)
- Test: `internal/vm/builtins_call_result_test.go`

**Interfaces:**
- Consumes: `prepareDeferredCall`, `prepareTaskCall`'s ObjFunction normalization pattern, `nativeVM(context)`.
- Produces: `(vm *VM) prepareBoundaryCall(callee value.Value, args []value.Value) (PreparedCall, error)`; native `call_result` registered globally (envelope construction stubbed until Task 4 — for this task the native may return `value.NewNull(), nil` after successful preparation, releasing retained args via `releasePreparedArguments` to stay RC-clean).
- Produces for later tasks: `(vm *VM) runCallBoundary(callee value.Value, args []value.Value) (value.Value, error)` — signature fixed here, body completed in Tasks 4–7.

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/vm/ -run TestCallResultMisuse -count=1`
Expected: FAIL — `call_result` undefined ("undefined global variable 'call_result'" or similar).

- [ ] **Step 3: Implement preparation + registration**

```go
// internal/vm/builtins_call_result.go
package vm

import (
	"fmt"

	"noxy-vm/internal/value"
)

func (vm *VM) defineCallResultBuiltins() {
	vm.DefineContextualNative("call_result", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), fmt.Errorf("call_result expects a callable")
		}
		return machine.runCallBoundary(args[0], args[1:])
	})
}

// prepareBoundaryCall valida sincronamente no chamador (design: misuse nunca
// e capturado). Normaliza ObjFunction sem upvalues para closure — mesmo
// ajuste de prepareTaskCall — e delega o resto a prepareDeferredCall, que ja
// valida closure (aridade+modos), native assinado (assinatura+modos, copia
// ansiosa) e construtor de struct (campos+tipos).
func (vm *VM) prepareBoundaryCall(callee value.Value, args []value.Value) (PreparedCall, error) {
	if callee.Type == value.VAL_FUNCTION {
		if fn, ok := callee.Obj.(*value.ObjFunction); ok && fn != nil && fn.UpvalueCount == 0 {
			callee = value.Value{Type: value.VAL_FUNCTION, Obj: &value.ObjClosure{
				Function:    fn,
				Upvalues:    []*value.ObjUpvalue{},
				Environment: fn.Environment,
			}}
		}
	}
	registration := SourceLocation{File: "?"}
	if frame := vm.currentFrame; frame != nil && frame.Closure != nil && frame.Closure.Function != nil {
		if c, ok := frame.Closure.Function.Chunk.(*chunk.Chunk); ok {
			registration = sourceLocation(c, frame.IP)
		}
	}
	prepared, err := vm.prepareDeferredCall(callee, args, registration)
	if err != nil {
		if callee.Type != value.VAL_FUNCTION && callee.Type != value.VAL_NATIVE {
			if _, isStruct := callee.Obj.(*value.ObjStruct); callee.Type != value.VAL_OBJ || !isStruct {
				return PreparedCall{}, fmt.Errorf("call_result expects a callable, got %s", runtimeValueMode(callee))
			}
		}
		return PreparedCall{}, err
	}
	return prepared, nil
}

// runCallBoundary: corpo completado nas Tasks 4-7. Nesta task, apenas valida
// e devolve null, desfazendo a retencao de preparacao para nao vazar RC.
func (vm *VM) runCallBoundary(callee value.Value, args []value.Value) (value.Value, error) {
	prepared, err := vm.prepareBoundaryCall(callee, args)
	if err != nil {
		return value.NewNull(), err
	}
	if closure, ok := prepared.Callee.Obj.(*value.ObjClosure); ok && prepared.Callee.Type == value.VAL_FUNCTION {
		vm.releasePreparedArguments(prepared.Arguments, closure.Function.Params)
	}
	return value.NewNull(), nil
}
```

Notes for the implementer:
- Add `"noxy-vm/internal/chunk"` to imports.
- The non-callable message: `prepareDeferredCall` returns `"can only call functions and classes"` for non-callables; the wrapper above rewrites it to `"call_result expects a callable, got <mode>"` so the misuse test asserts our name. Keep `prepareDeferredCall`'s own messages for arity/mode/constructor errors (the tests above assert their existing texts).
- Register: grep for the function that calls `defineTaskBuiltins()` (expected `internal/vm/builtins.go`); add `vm.defineCallResultBuiltins()` in the same list.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/vm/ -run TestCallResultMisuse -count=1`
Expected: PASS.

- [ ] **Step 5: Reconcile the builtin registry test**

Run: `go test ./internal/vm/ -run 'TestBuiltin|TestNativeSignatures|Registry' -count=1`. If `builtins_registry_test.go` (or `native_signatures_test.go`) asserts the complete builtin list, add `call_result` following the pattern used by `spawn_task`/`task_await` entries (contextual, variadic, no signature). Then `go test ./internal/vm/ -count=1`.

- [ ] **Step 6: Commit**

```bash
git add internal/vm/builtins_call_result.go internal/vm/builtins.go internal/vm/builtins_call_result_test.go internal/vm/builtins_registry_test.go
git commit -m "feat(vm): call_result — registro do nativo e validacao sincrona de misuse no chamador"
```

---

### Task 4: Happy path — invocation and ok envelope

**Files:**
- Modify: `internal/vm/builtins_call_result.go`
- Test: `internal/vm/builtins_call_result_test.go`

**Interfaces:**
- Consumes: `callPreparedValue`, `vm.run`, `invokePreparedCall`'s cleanup pattern (`defer.go:142-187`).
- Produces: `(vm *VM) invokeBoundaryCall(prepared PreparedCall) (value.Value, error)` — result or raw Go error (not yet enveloped); `callResultOkEnvelope(result value.Value) value.Value`.

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/vm/ -run 'TestCallResultOk|TestCallResultEnvelope' -count=1`
Expected: FAIL — envelope is null (stub) so field access errors / report mismatches.

- [ ] **Step 3: Implement invocation + ok envelope**

Replace the Task 3 stub of `runCallBoundary` and add:

```go
func (vm *VM) runCallBoundary(callee value.Value, args []value.Value) (value.Value, error) {
	prepared, err := vm.prepareBoundaryCall(callee, args)
	if err != nil {
		return value.NewNull(), err
	}
	result, callErr := vm.invokeBoundaryCall(prepared)
	if callErr != nil {
		return callResultFailureEnvelope(callErr), nil // Task 5
	}
	return callResultOkEnvelope(result), nil
}

// invokeBoundaryCall espelha invokePreparedCall (defer.go) com duas
// diferencas: captura o resultado (terminalResult para closures; topo da
// pilha para native/construtor) e nao descarta o valor no cleanup — o
// envelope o carrega. O release da retencao de closure e identico.
func (vm *VM) invokeBoundaryCall(call PreparedCall) (result value.Value, err error) {
	base := vm.stackTop
	if base < 0 || base >= len(vm.stack) || len(call.Arguments) > len(vm.stack)-base-1 {
		return value.NewNull(), vm.runtimeErrorAtCurrentFrame("stack overflow while invoking call_result")
	}
	result = value.NewNull()
	temporaryTop := base
	defer func() {
		cleanupTop := vm.stackTop
		if temporaryTop > cleanupTop {
			cleanupTop = temporaryTop
		}
		for i := base; i < cleanupTop; i++ {
			vm.stack[i] = value.Value{}
		}
		vm.stackTop = base
		if call.Callee.Type == value.VAL_FUNCTION {
			if closure, ok := call.Callee.Obj.(*value.ObjClosure); ok && closure != nil && closure.Function != nil {
				vm.releasePreparedArguments(call.Arguments, closure.Function.Params)
			}
		}
	}()

	ownerFrameCount := vm.frameCount
	vm.push(call.Callee)
	for _, argument := range call.Arguments {
		vm.push(argument)
	}
	temporaryTop = vm.stackTop

	ok, err := vm.callPreparedValue(call.Callee, len(call.Arguments), nil, 0)
	if !ok {
		return value.NewNull(), err
	}
	if vm.frameCount > ownerFrameCount {
		if runErr := vm.run(ownerFrameCount+1, &result); runErr != nil {
			return value.NewNull(), runErr
		}
		return result, nil
	}
	// native/construtor: sem frame novo; resultado no topo da pilha.
	return vm.peek(0), nil
}

func callResultOkEnvelope(result value.Value) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":      value.NewBool(true),
		"value":   result,
		"failure": value.NewNull(),
	})
}

// placeholder ate a Task 5 (mantem o pacote compilando):
func callResultFailureEnvelope(err error) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":      value.NewBool(false),
		"value":   value.NewNull(),
		"failure": value.NewNull(),
	})
}
```

**RC verification note (mandatory, not optional):** the ok `value` travels from a finished frame into the envelope exactly as in `startSupervisedTask` → `taskOutcomeEnvelope` (`builtins_tasks.go:136-173`), which adds no extra retain. If `TestCallResultValueSemantics` (Task 7) crashes or corrupts, add `value.Retain(result)` before the cleanup runs and a matching comment — decide by test evidence, mirroring the task path first.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/vm/ -run 'TestCallResult' -count=1`, then the whole package `go test ./internal/vm/ -count=1` (defer/RC suites must stay green).
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_call_result.go internal/vm/builtins_call_result_test.go
git commit -m "feat(vm): call_result — invocacao sincrona com captura de resultado e envelope ok"
```

---

### Task 5: Failure capture — runtime errors into the envelope

**Files:**
- Modify: `internal/vm/builtins_call_result.go` (real `callResultFailureEnvelope`)
- Test: `internal/vm/builtins_call_result_test.go`

**Interfaces:**
- Consumes: `UnwindError`, `DeferredError`, `RuntimeError`, `deepestRuntimeStack`.
- Produces: `callResultFailureEnvelope(err error) value.Value` and `failureMap(err error) value.Value` (recursive; used by Task 6's assertions).

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/vm/ -run 'TestCallResultCaptures|TestCallResultFailureStack' -count=1`
Expected: FAIL — `r.failure` is null (placeholder), field access on null errors out.

- [ ] **Step 3: Implement the failure envelope**

Replace the placeholder:

```go
func callResultFailureEnvelope(err error) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":      value.NewBool(false),
		"value":   value.NewNull(),
		"failure": failureMap(err),
	})
}

// failureMap converte a arvore de erro do unwinding no shape Failure.
// UnwindError com Primary vira a falha primaria com cada DeferredError em
// causes (ordem LIFO ja garantida por finalizeCurrentFrame); cleanup-first
// (Primary nil) promove a PRIMEIRA falha diferida a primaria e agrega as
// demais sob as causes dela (design §2, "Cleanup as first failure").
func failureMap(err error) value.Value {
	if unwind, ok := err.(*UnwindError); ok {
		if unwind.Primary != nil {
			return failureMapWithCauses(unwind.Primary, unwind.Deferred)
		}
		if len(unwind.Deferred) > 0 {
			primary := deferredFailureMap(&unwind.Deferred[0], unwind.Deferred[1:])
			return primary
		}
	}
	if deferred, ok := err.(*DeferredError); ok {
		return deferredFailureMap(deferred, nil)
	}
	return failureMapWithCauses(err, nil)
}

func failureMapWithCauses(primary error, deferred []DeferredError) value.Value {
	causes := make([]value.Value, 0, len(deferred))
	for index := range deferred {
		causes = append(causes, deferredFailureMap(&deferred[index], nil))
	}
	message := ""
	if primary != nil {
		message = primary.Error()
	}
	return value.NewMapWithData(map[string]value.Value{
		"kind":    value.NewString("runtime"),
		"message": value.NewString(message),
		"stack":   value.NewString(deepestRuntimeStack(primary)),
		"causes":  value.NewArray(causes),
	})
}

// deferredFailureMap constroi a Failure de uma falha diferida: a causa vira a
// falha (aninhando as proprias causes dela recursivamente via failureMap) e a
// localizacao de REGISTRO do defer entra como frame mais externo do stack —
// forma-envelope da promessa da spec de defer ("with its registration
// location"). siblings sao falhas diferidas posteriores promovidas para as
// causes desta (apenas no caso cleanup-first).
func deferredFailureMap(deferred *DeferredError, siblings []DeferredError) value.Value {
	failure := failureMap(deferred.Cause)
	mapping := failure.Obj.(*value.ObjMap)

	stackValue, _ := mapping.Get("stack")
	stack, _ := stackValue.Obj.(string)
	registrationFrame := fmt.Sprintf("[%s] defer registration", deferred.Registration)
	if stack == "" {
		stack = registrationFrame
	} else {
		stack = stack + "\n" + registrationFrame
	}
	mapping.Set("stack", value.NewString(stack))

	if len(siblings) > 0 {
		causes := make([]value.Value, 0, len(siblings))
		for index := range siblings {
			causes = append(causes, deferredFailureMap(&siblings[index], nil))
		}
		mapping.Set("causes", value.NewArray(causes))
	}
	return failure
}
```

Stack extent: `deepestRuntimeStack` returns the stack captured at the failure point by `runtimeErrorCause`, whose `captureNoxyStack` walks only Noxy frames — the boundary is inside a native call, so the `call_result` native itself never appears as a frame; the test locks this in.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/vm/ -run 'TestCallResult' -count=1` then `go test ./internal/vm/ -count=1`.
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_call_result.go internal/vm/builtins_call_result_test.go
git commit -m "feat(vm): call_result — captura de erro de runtime no envelope Failure com stack e kind"
```

---

### Task 6: Defer aggregation — causes, registration location, cleanup-first

**Files:**
- Test only: `internal/vm/builtins_call_result_test.go` (the Task 5 code already implements the shape; this task proves it against the defer machinery and fixes what the tests reveal)

- [ ] **Step 1: Write the failing tests**

```go
func TestCallResultAggregatesDeferFailures(t *testing.T) {
	source := `
func limpeza_ruim()
    to_int("defer-quebrado")
end

func corpo() -> int
    defer limpeza_ruim()
    return to_int("primario")
end

let r: any = call_result(corpo)
let causa: any = r.failure.causes[0]
test_report(to_str(length(r.failure.causes)) + "|" + causa.kind + "|" + causa.message + "|" + causa.stack)
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	parts := strings.SplitN(text, "|", 4)
	if len(parts) != 4 || parts[0] != "1" || parts[1] != "runtime" {
		t.Fatalf("unexpected report: %q", text)
	}
	if !strings.Contains(parts[2], "defer-quebrado") {
		t.Fatalf("cause message should carry the deferred failure: %q", parts[2])
	}
	if !strings.Contains(parts[3], "defer registration") {
		t.Fatalf("cause stack must carry the registration location as outermost frame: %q", parts[3])
	}
}

func TestCallResultCleanupFirstFailure(t *testing.T) {
	source := `
func limpeza_ruim()
    to_int("so-o-defer-quebra")
end

func corpo() -> int
    defer limpeza_ruim()
    return 42
end

let r: any = call_result(corpo)
test_report(to_str(r.ok) + "|" + to_str(r.value == null) + "|" + r.failure.message + "|" + to_str(length(r.failure.causes)))
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	parts := strings.SplitN(text, "|", 4)
	if len(parts) != 4 || parts[0] != "false" || parts[1] != "true" || parts[3] != "0" {
		t.Fatalf("cleanup-first must discard the computed value and promote the deferred failure: %q", text)
	}
	if !strings.Contains(parts[2], "so-o-defer-quebra") {
		t.Fatalf("primary failure should be the deferred one: %q", parts[2])
	}
}

func TestCallResultCallerDefersUnaffected(t *testing.T) {
	source := `
let trilha: string[] = []

func marca(rotulo: string)
    append(trilha, rotulo)
end

func corpo() -> int
    return to_int("x")
end

func chamador() -> string
    defer marca("caller-defer")
    let r: any = call_result(corpo)
    append(trilha, "depois-da-captura")
    return to_str(r.ok)
end

let ok_text: string = chamador()
test_report(ok_text + "|" + trilha[0] + "|" + trilha[1])
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	if text != "false|depois-da-captura|caller-defer" {
		t.Fatalf("caller frame must be unaffected by the capture: %q", text)
	}
}
```

Note: if `append(trilha, ...)` on a global from inside functions trips CoW/global semantics, switch the trail to `ref` parameters — the assertion (execution continues; caller defer runs after) is what matters, adapt the vehicle.

- [ ] **Step 2: Run tests**

Run: `go test ./internal/vm/ -run 'TestCallResultAggregates|TestCallResultCleanup|TestCallResultCallerDefers' -count=1`
Expected: likely PASS from Task 5's implementation; any FAIL here is a real bug in the aggregation mapping — fix `failureMap`/`deferredFailureMap` (not the tests) until green.

- [ ] **Step 3: Commit**

```bash
git add internal/vm/builtins_call_result_test.go internal/vm/builtins_call_result.go
git commit -m "test(vm): call_result — agregacao de falhas de defer, cleanup-first e isolamento do frame chamador"
```

---

### Task 7: Panic recovery, nesting, no-rollback, value semantics

**Files:**
- Modify: `internal/vm/builtins_call_result.go`
- Test: `internal/vm/builtins_call_result_test.go`

**Interfaces:**
- Produces: `(vm *VM) hardUnwindTo(target int)` — frame release without running Noxy defers, for the panic path only.

- [ ] **Step 1: Write the failing tests**

```go
func TestCallResultCapturesGoPanic(t *testing.T) {
	machine := New()
	machine.DefineNative("explode", func(args []value.Value) value.Value {
		panic("boom-nativo")
	})
	source := `
func corpo() -> int
    explode()
    return 1
end
let r: any = call_result(corpo)
test_report(to_str(r.ok) + "|" + r.failure.kind + "|" + r.failure.message)
`
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, source); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	text, _ := captured.Obj.(string)
	if !strings.HasPrefix(text, "false|panic|") || !strings.Contains(text, "boom-nativo") {
		t.Fatalf("panic must be captured as kind=panic: %q", text)
	}
}

func TestCallResultNestedBoundaries(t *testing.T) {
	source := `
func interna() -> int
    return to_int("x")
end
func externa() -> string
    let r: any = call_result(interna)
    return "interna-capturou:" + to_str(r.ok)
end
let fora: any = call_result(externa)
test_report(to_str(fora.ok) + "|" + fora.value)
`
	reported := captureVMSource(t, source)
	if text, _ := reported.Obj.(string); text != "true|interna-capturou:false" {
		t.Fatalf("nearest boundary must capture: %q", text)
	}
}

func TestCallResultNoRollback(t *testing.T) {
	source := `
func muta_e_quebra(alvo: ref int) -> int
    *alvo = 99
    return to_int("x")
end
let caixa: int = 1
let r: any = call_result(muta_e_quebra, ref caixa)
test_report(to_str(r.ok) + "|" + to_str(caixa))
`
	reported := captureVMSource(t, source)
	if text, _ := reported.Obj.(string); text != "false|99" {
		t.Fatalf("mutations before the failure must remain (no rollback): %q", text)
	}
}

func TestCallResultValueSemantics(t *testing.T) {
	source := `
func faz_array() -> int[]
    return [1, 2, 3]
end
let r: any = call_result(faz_array)
let copia: int[] = r.value
copia[0] = 100
let original: any = r.value
test_report(to_str(original[0]) + "|" + to_str(copia[0]) + "|" + to_str(length(original)))
`
	reported := captureVMSource(t, source)
	if text, _ := reported.Obj.(string); text != "1|100|3" {
		t.Fatalf("composite value must obey CoW semantics without corruption: %q", text)
	}
}
```

(`ref` syntax check: `*alvo = 99` per spec §2; if the compiler wants a different deref-store spelling, match the spec — the assertion is `caixa == 99` after capture.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/vm/ -run 'TestCallResultCapturesGoPanic|TestCallResultNested|TestCallResultNoRollback|TestCallResultValueSemantics' -count=1`
Expected: the panic test FAILS (panic propagates out and kills the test / gets caught by Go test harness). The other three may already pass — keep them; they are regression armor.

- [ ] **Step 3: Implement panic recovery**

Add to `invokeBoundaryCall`, wrapping the invocation region — replace the section from `ownerFrameCount := vm.frameCount` to the end with:

```go
	ownerFrameCount := vm.frameCount
	defer func() {
		if recovered := recover(); recovered != nil {
			vm.hardUnwindTo(ownerFrameCount)
			result = value.NewNull()
			err = &boundaryPanicError{payload: fmt.Sprint(recovered), stack: string(debug.Stack())}
		}
	}()
	vm.push(call.Callee)
	for _, argument := range call.Arguments {
		vm.push(argument)
	}
	temporaryTop = vm.stackTop

	ok, err := vm.callPreparedValue(call.Callee, len(call.Arguments), nil, 0)
	if !ok {
		return value.NewNull(), err
	}
	if vm.frameCount > ownerFrameCount {
		if runErr := vm.run(ownerFrameCount+1, &result); runErr != nil {
			return value.NewNull(), runErr
		}
		return result, nil
	}
	return vm.peek(0), nil
```

**Ordering constraint:** Go defers run LIFO — this recover defer must be registered AFTER the Task 4 cleanup defer, so on panic the recover body runs FIRST (restoring frames via `hardUnwindTo`), then the cleanup defer restores the stack window. Keep the two defers in that order in the final function body (cleanup registered first, recover registered second).

Add the helper and the panic error type:

```go
// boundaryPanicError transporta um panico de Go recuperado na fronteira; o
// envelope o converte em Failure{kind: "panic"}. Nunca escapa da fronteira.
type boundaryPanicError struct {
	payload string
	stack   string
}

func (err *boundaryPanicError) Error() string { return err.payload }

// hardUnwindTo libera os frames acima de target sem executar defers Noxy —
// depois de um panico de Go o estado desses frames e suspeito; espelha a
// fronteira de task, que tambem nao roda defers no caminho de panico (o VM
// filho e abandonado). Truncar Deferred antes de finalizar reusa o funil
// unico de release (Owned/upvalues) sem rodar codigo Noxy.
func (vm *VM) hardUnwindTo(target int) {
	for vm.frameCount > target {
		if frame := vm.currentFrame; frame != nil {
			frame.Deferred = frame.Deferred[:0]
		}
		vm.finalizeCurrentFrame(frameOutcome{Err: errBoundaryPanic})
	}
}

var errBoundaryPanic = fmt.Errorf("call_result: unwinding after Go panic")
```

(Add `"runtime/debug"` to imports.) In `failureMap`, handle the panic error first:

```go
	if panicErr, ok := err.(*boundaryPanicError); ok {
		return value.NewMapWithData(map[string]value.Value{
			"kind":    value.NewString("panic"),
			"message": value.NewString(panicErr.payload),
			"stack":   value.NewString(panicErr.stack),
			"causes":  value.NewArray([]value.Value{}),
		})
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/vm/ -run 'TestCallResult' -count=1`, then the full package with race: `go test ./internal/vm/ -count=1 -race` (concurrency suites exercise the shared machinery).
Expected: PASS. If `TestCallResultValueSemantics` fails with corruption/double-release, apply the Task 4 RC note (retain the result before cleanup) and re-run.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_call_result.go internal/vm/builtins_call_result_test.go
git commit -m "feat(vm): call_result — recuperacao de panico Go como kind=panic, fronteiras aninhadas e sem rollback"
```

---

### Task 8: `result.nx` deprecation + example migration

**Files:**
- Modify: `internal/stdlib/result.nx` (deprecation header only)
- Rewrite: `noxy_examples/result_pattern.nx`

- [ ] **Step 1: Deprecation header**

Prepend to `internal/stdlib/result.nx` (keep everything else intact):

```noxy
// DEPRECATED desde 0.8.0: use a convencao `_result` (structs proprios com
// ok/value/error) construida sobre `call_result`, e o modulo `errors`
// (Failure/CallResult). Este modulo sera removido na release seguinte.
// Motivo: terceiro vocabulario de erro; ver
// docs/superpowers/specs/2026-08-19-call-result-design.md §4.
```

- [ ] **Step 2: Migrate the example**

Rewrite `noxy_examples/result_pattern.nx` as the canonical wrap-and-name `_result` twin (this is the design doc's intended idiom demo). Read the current file first to keep any narrative comments that still apply, then replace its mechanism:

```noxy
// result_pattern.nx — o idioma _result construido em noxy puro com call_result.
//
// Antes: modulo `result` (deprecado). Agora: um gemeo `_result` nomeado, com
// struct de resultado tipado, embrulhando uma primitiva que levanta.

use errors select *

struct DivResult
    ok: bool
    value: int
    error: string
end

func divide(a: int, b: int) -> int
    return a / b   // levanta "division by zero" quando b == 0
end

func divide_result(a: int, b: int) -> DivResult
    let r: CallResult = call_result(divide, a, b)
    if r.ok then
        let quociente: int = to_int(r.value)
        return DivResult(true, quociente, "")
    end
    return DivResult(false, 0, r.failure.message)
end

let boa: DivResult = divide_result(10, 2)
let ruim: DivResult = divide_result(1, 0)

if boa.ok then
    print("10 / 2 = " + to_str(boa.value))
end
if !ruim.ok then
    print("1 / 0 falhou: " + ruim.error)
end
```

- [ ] **Step 3: Run the example and the examples suite**

Run: `go run ./cmd/noxy noxy_examples/result_pattern.nx`
Expected output lines: `10 / 2 = 5` and `1 / 0 falhou: ...division by zero...`.
Then run the examples runner used by CI (locate it: `Glob noxy_examples/*test*` / check how `language_semantics_test2.nx` is executed — there is a runner with exclusions per project memory). Run it; all examples green.

- [ ] **Step 4: Commit**

```bash
git add internal/stdlib/result.nx noxy_examples/result_pattern.nx
git commit -m "refactor(stdlib): deprecia o modulo result e migra result_pattern.nx para o idioma _result sobre call_result"
```

---

### Task 9: Spec merge + CHANGELOG

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md`
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Merge the normative sections**

From `docs/superpowers/specs/2026-08-19-call-result-design.md` §2, copy the two sections between the `---` markers ("Errors: raise for bugs, results for data" and "The error boundary: `call_result`") into `docs/NOXY_LANGUAGE_SPEC.md`, inserted after the "Defer and deterministic cleanup" section (before "When / Channel Select", currently line ~1004). Adjustments while merging:
- In the conversions section (~lines 1108-1141), keep the `to_int`/`to_int_result` examples but replace the philosophy paragraphs with one line referencing the new section ("A regra geral levantar-vs-`_result` está em *Errors: raise for bugs, results for data*.").
- Drop design-doc-only phrasing ("this design", "see §N of this document"); the spec text must stand alone.

- [ ] **Step 2: §5 and §12 updates**

- §5 (struct self-reference): add one sentence + example that a struct may reference itself through an **array field without `ref`** (`causes: Failure[]`), noting value semantics cannot form cycles; keep the existing `ref` self-reference text.
- §12 (Standard Library table): add row `| errors | Error boundary envelope shapes (Failure, CallResult) |`. Mark the `result` module row (if listed) as deprecated; if not listed, add nothing for it.

- [ ] **Step 3: CHANGELOG entry**

Add at top of `CHANGELOG.md`, following the existing format (compare the 0.7.1 entry):

```markdown
## [Unreleased]

### Added — `call_result`: fronteira síncrona de erro

- Novo nativo global `call_result(fn, ...args)` converte uma falha de runtime
  que desenrola de `fn` em valor: envelope `{ok, value, failure}` com
  `failure = {kind, message, stack, causes}` — o mesmo vocabulário da
  fronteira de task, estendido com as falhas de defer agregadas (`causes`,
  ordem LIFO, localização de registro no stack). Misuse (não-callable,
  aridade/modos/campos errados onde há metadata) levanta síncrono no
  chamador. Panics de Go viram `kind="panic"`; fatais do runtime Go seguem
  fatais. Sem rollback: mutações via `ref`/globais/upvalues permanecem.
- Novo módulo stdlib `errors` com os shapes `Failure` e `CallResult`
  (fisicamente o envelope é um map na fronteira dinâmica, como `IntResult`).
- Gêmeos `_result` agora são escrevíveis em noxy puro (ver
  `noxy_examples/result_pattern.nx`).

### Changed

- `to_int`/`to_float`: o sufixo "; use to_int_result to handle failure" saiu
  da mensagem de erro (agora limpa e capturável) e virou `hint:` impresso
  apenas na saída fatal do topo.

### Deprecated

- Módulo `result` (`use result`): substituído pela convenção `_result` +
  módulo `errors`. Remoção na próxima release.
```

- [ ] **Step 4: Verify docs coherence**

Grep the spec for `call_result` — the merged text must not reference undefined anchors. Run `go test ./... -count=1` once more (spec edits can't break tests; this is the cheap full-suite checkpoint).

- [ ] **Step 5: Commit**

```bash
git add docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md
git commit -m "docs(spec): call_result — filosofia levantar-vs-result e fronteira sincrona de erro; §5 autorreferencia via array; §12 modulo errors; CHANGELOG"
```

---

### Task 10: Final verification

- [ ] **Step 1: Full suite**

Run: `go test ./... -count=1` and `go vet ./...` and `go build ./...`
Expected: all green.

- [ ] **Step 2: End-to-end smoke on the real binary**

Run: `go run ./cmd/noxy noxy_examples/result_pattern.nx` (expected output per Task 8).
Write a throwaway smoke script in the scratchpad (NOT in the repo) exercising: ok path, captured runtime error, misuse raising, fatal `hint:` line (`to_int("abc")` at top level must print `Runtime error: ...` then `hint: use to_int_result to handle failure`). Run and eyeball.

- [ ] **Step 3: Examples runner**

Run the project's noxy_examples runner (as found in Task 8); all green.

- [ ] **Step 4: Self-review the diff**

Run: `git diff develop --stat` and review each file against the design doc §2 checklist: envelope fields, misuse-synchronous, defer edge cases, panic coverage, representation (map), no `task_await` changes, `result.nx` still functional.
