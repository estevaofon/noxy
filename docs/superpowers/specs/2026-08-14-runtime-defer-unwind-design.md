# Runtime Defer and Unwind Design

**Date:** 2026-08-14
**Status:** Approved design, awaiting written-spec review
**Scope:** Resolve point 10 from PR #17 through deterministic deferred calls

## 1. Purpose

Noxy exposes files, sockets, listeners, SQLite databases, and prepared
statements, but it has no construct that guarantees cleanup after a runtime
error. A call placed at the end of a function is skipped by an early return or
an error such as division by zero. The language also has no `try/finally`, RAII,
or deterministic garbage-collection hook that can fill this role.

This subproject adds a small `defer` statement and the runtime machinery needed
to execute deferred calls safely. The implementation must not merely inject
cleanup calls before explicit returns: normal return, implicit return, module
execution, script execution, and runtime failure all use the same frame-unwind
mechanism.

## 2. Goals

1. Add `defer call(...)` with Go-style registration semantics.
2. Execute deferred calls in last-in, first-out order for every exiting frame.
3. Run deferred calls after normal return and after runtime failure.
4. Keep frame slots and open upvalues valid until that frame's deferred calls
   finish.
5. Continue running older deferred calls when a newer cleanup fails.
6. Preserve the original runtime error and report every cleanup failure with
   its registration location.
7. Leave frames, stack, instruction pointers, and upvalues in a reusable state
   after both successful and failed interpretation.
8. Support deterministic explicit cleanup of files, sockets, listeners,
   SQLite statements, and SQLite databases without changing their ownership.

## 3. Non-goals

This subproject does not add:

- `try`, `catch`, `finally`, or runtime-error recovery in Noxy source;
- automatic resource closing based on frame ownership or garbage collection;
- supervised tasks or changes to detached `spawn` error propagation;
- panic recovery as a language-level error mechanism;
- cancellation, signals, or process-shutdown hooks;
- new public close signatures or changed result shapes for existing builtins;
- a general deferred block or arbitrary deferred expression.

The existing shared-resource ownership from PR #17 remains authoritative. A
resource can outlive the frame that opened it when another shared VM or value
still owns its handle. Only an explicit deferred close changes resource state.

## 4. Source Semantics

### 4.1 Syntax

The only accepted form is a statement whose operand is a real call:

```noxy
defer io.close(file)
defer net.socket_close(socket)
defer sqlite.finalize(statement)
```

`defer` is valid in functions, the main script frame, module initialization,
and functions executed by `spawn`. It is not a lexical-scope cleanup: a
`defer` inside an `if` or loop belongs to the containing call frame. A loop
registers one entry for every executed iteration.

Arbitrary expressions and pseudo-calls that compile without an invocable
callee are rejected. In particular, `defer value`, `defer 1 + 2`, and
`defer addr(ref x)` produce compile errors. Specialized compiler paths remain
eligible when they still prepare a real callable and end in ordinary call
dispatch. This includes the current `append`, `pop`, `delete`, `json_loads`,
`chan_send`, and `chan_recv` paths. Each eligible path must accept an emission
mode that selects `OP_CALL` or `OP_DEFER`; `addr` remains ineligible because it
produces `OP_ADDR` and no deferred callable.

### 4.2 Registration timing

The callee and arguments are evaluated immediately, from left to right, when
the `defer` statement executes. An error while evaluating them prevents that
specific entry from being registered and begins ordinary error unwind; entries
registered earlier still run.

Runtime arity and parameter-mode validation also happen at registration.
Parameters passed by value receive Noxy's normal top-level shallow copy at
that time. Parameters declared `ref` retain the reference. Untyped legacy
natives preserve their current dynamic behavior because they do not publish
parameter-mode metadata.

The prepared callee and arguments are invoked exactly once during unwind.
They are not re-evaluated or copied a second time. Mutating or rebinding a
source variable after registration therefore does not replace the captured
top-level value, while nested composite identities retain the language's
documented shallow-copy behavior.

### 4.3 Execution and results

Entries execute in LIFO order within each frame. A deferred call can invoke a
Noxy closure or native callable and can register its own deferred calls. Its
return value is always discarded. Returning an error-shaped ordinary Noxy
value is not a runtime failure; only a runtime error from the call participates
in unwind error aggregation.

## 5. Frontend and Bytecode

The frontend adds:

- a `DEFER` keyword token;
- `ast.DeferStmt`, containing the source token and `*ast.CallExpression`;
- parser validation that the operand is a call expression;
- compiler validation that the call uses the ordinary callable path.

The compiler factors the ordinary call pipeline into two stages:

1. compile the callee and arguments with the same static type, arity, runtime
   type-marker, and reference-mode checks used by an immediate call;
2. emit either `OP_CALL <argCount>` or `OP_DEFER <argCount>`.

Compiler-special paths that still end in ordinary call dispatch reuse their
existing operand preparation and select `OP_DEFER` as the final instruction.
Paths that do not leave an invocable callee and arguments, currently `addr`,
are rejected with a targeted diagnostic.

`OP_DEFER <argCount>` consumes the callee and arguments from the operand stack,
creates a prepared call, appends it to the current frame, and produces no
stack value. The opcode and its operand are included in opcode naming and
disassembly.

## 6. Runtime Data Model

Conceptually, each frame gains a LIFO collection:

```go
type SourceLocation struct {
	File string
	Line int
}

type PreparedCall struct {
	Callee       value.Value
	Arguments    []value.Value
	Registration SourceLocation
}

type CallFrame struct {
	// Existing closure, IP, and environment fields.
	StackBase int
	LocalBase int
	Deferred []PreparedCall
}
```

`StackBase` is the callee/call slot and the first slot owned by the frame. It
replaces the use of `Slots` as a cleanup boundary. `LocalBase` is a separate,
mandatory offset used exclusively by `OP_GET_LOCAL`, `OP_SET_LOCAL`,
`OP_REF_LOCAL`, and local-upvalue capture. The layouts are explicit:

- main script: `StackBase = 0`, `LocalBase = 1`;
- ordinary function: `StackBase = LocalBase = stackTop - argCount - 1`,
  preserving compiler slot zero for the callee/function instance;
- spawned function: `StackBase = LocalBase = 0` in its independent VM stack.

Terminal cleanup always starts at `StackBase`; local access and capture always
start at `LocalBase`. A successful ordinary return replaces the call slot with
the result. Terminal script and spawned-frame completion clear that slot and
leave no callee residual.

Preparation is defined per callable kind:

| Callable | Registration behavior |
|----------|-----------------------|
| Noxy closure | Validate arity and parameter modes; shallow-copy every non-`ref` parameter and retain explicit references. |
| Signed native | Validate published arity and modes; shallow-copy every non-`ref` parameter and retain explicit references. |
| Legacy unsigned native | Capture each evaluated `value.Value` exactly as current unsigned-native calls receive it; perform no unavailable mode validation or additional copy. |
| Struct constructor | Validate field arity and runtime field types; capture evaluated field values directly, preserving the constructor's existing no-`copyValue` behavior. |

Prepared invocation has a dedicated path and must not re-enter the normal
argument-preparation code. Native handler errors and errors produced by the
deferred Noxy body occur during invocation, not registration.

The final names may follow existing package conventions, but these boundaries
are mandatory:

- preparation validates the callable and captures arguments at registration;
- prepared invocation never repeats parameter copying;
- an entry is removed from the frame before invocation, which guarantees
  exactly-once behavior even when that invocation fails;
- every invocation records its stack base and restores `stackTop` to that base
  after success or failure.

An architecture test prevents direct terminal frame teardown outside the
unwind component. Frame construction can remain beside its current call sites.

## 7. Bounded Unwind Machine

### 7.1 Return and error contracts

The machine exposes two distinct transitions:

- `finishFrame(returnOutcome)` finalizes only the current frame. It is used for
  an explicit or implicit normal return, regardless of the active `run`
  boundary.
- `unwindTo(targetFrameCount, errorOutcome)` finalizes frames until exactly
  `targetFrameCount` frames remain. It is used only after a runtime failure or
  after a deferred failure converts a normal return into an error.

This distinction is mandatory. During the main `run(1)`, a normal return from
an inner function removes only that function and resumes the script. It never
unwinds directly to zero. If one of that function's deferred calls fails,
`finishFrame` completes all of that frame's cleanups, converts the return to an
error outcome, and then `unwindTo` continues toward the active run boundary.

The active error boundary remains execution-context specific:

- main interpretation and detached spawn execution target zero frames;
- recursive module execution targets the importing frame count;
- a deferred Noxy call targets the count that retains its owner frame.

The current recursive `run(minFrameCount)` behavior maps error unwind to
`minFrameCount - 1`. A module error first unwinds only module-created frames
and returns to `OP_IMPORT`; the outer run wraps that structured cause and then
unwinds toward its own boundary. A cleanup error similarly unwinds only frames
created by the cleanup and returns to the still-live owner frame.

### 7.2 Single-frame finalization

Both transitions call the same single-frame finalizer. For each frame being
removed, it performs these steps in order:

1. preserve the pending return value or error outcome;
2. remove the newest prepared call from the frame;
3. record the current stack base and invoke that prepared call;
4. if it creates Noxy frames, run them with the owner frame as their unwind
   boundary;
5. discard the cleanup result and restore the recorded stack base;
6. append any cleanup error and continue with the next prepared call;
7. after the deferred stack is empty, close every upvalue into the frame's
   slots exactly once;
8. clear the frame's complete stack window and nil the frame-array entry;
9. update `frameCount`, `currentFrame`, stack top, chunk, and cached IP;
10. either place a successful return value in the caller's call slot or
    propagate the error outcome to the next frame.

All cached execution state is persisted to its frame before control can enter
another run loop or the unwind machine. In particular, `frame.IP` is saved
before every immediate call, prepared call, recursive module `run`,
`OP_RETURN`, and transfer of a runtime error. Restoring a surviving frame
always reloads chunk and IP from that frame rather than from stale executor
locals.

A frame is never popped by `OP_RETURN` itself; that opcode constructs a return
outcome and calls `finishFrame`. Runtime instruction failures call
`unwindTo` rather than performing separate cleanup.

If a deferred Noxy function fails, its own frames and deferred entries unwind
to the owner boundary. The returned error is recorded against the owner's
registration entry. The owner remains alive and proceeds with its older
entries.

### 7.3 Return-to-error conversion

A normal return remains successful only if every deferred call succeeds. The
first cleanup failure converts the outcome to an unwind error. The current
frame still executes all older entries and completes its single-frame
finalization. The error is then propagated with `unwindTo` through caller
frames up to the active boundary, so deferred calls in those frames also run.
Explicit returns, implicit void returns, script completion, and module
completion all follow this rule.

## 8. Structured Errors

Runtime errors become structured rather than flattened strings:

```go
type RuntimeError struct {
	Location SourceLocation
	Message  string
	Cause    error
}

type DeferredError struct {
	Registration SourceLocation
	Cause        error
}

type UnwindError struct {
	Primary  error
	Deferred []DeferredError
}
```

`RuntimeError.Unwrap` preserves an underlying native or module error when one
exists. `DeferredError.Unwrap` exposes its cause. `UnwindError.Unwrap() []error`
returns the primary error first, when present, followed by the deferred errors
in execution order. This keeps the original cause first while allowing
`errors.Is` and `errors.As` to discover every structured cause. When a normal
return has no primary error, the slice begins with the first deferred failure.

Nested aggregates are preserved, not flattened. If a deferred Noxy call fails
and its own deferred calls also fail, that entire inner `UnwindError` is the
`Cause` of one outer `DeferredError` associated with the owner's registration
site. An import failure is a `RuntimeError` whose `Cause` is the module's
structured error. This preserves frame ownership and ordering at every level.
Rendering recursively indents a nested cause under its single outer prefix; it
does not copy inner entries into the outer slice or manufacture new source
locations.

Every path that adds context to an existing error uses an explicit constructor
with `Cause` or `%w`. Runtime/native/module errors are never passed through
`runtimeError(..., "%s", err)` or another formatter that destroys identity.

The text representation is stable and non-duplicative:

```text
[source.nx:line 12] division by zero
defer registered at source.nx:line 8 failed: [cleanup.nx:line 3] cleanup failed
defer registered at source.nx:line 7 failed: native close failed
```

The original failure is printed first. Each deferred failure prints its
registration site once and retains the cleanup's own execution location in its
cause. `errors.Is` and `errors.As` continue to discover the original wrapped
Go error instead of seeing only formatted text.

## 9. Resource Cleanup

Deferred resource cleanup uses existing public APIs and shared registries:

```noxy
let file: io.File = io.open(path, "w")
defer io.close(file)

let socket: net.Socket = net.connect(host, port)
defer net.socket_close(socket)

let db: sqlite.Database = sqlite.open(path)
defer sqlite.close(db)
let stmt: sqlite.Statement = sqlite.prepare(db, query)
defer sqlite.finalize(stmt)
```

The SQLite order is intentional: database close is registered first so
statement finalization executes first. Socket and listener handles use the
same explicit pattern.

This version does not claim to aggregate operating-system close failures that
existing underlying builtins suppress. `io_close`, `net_close`,
`sqlite_close`, and `sqlite_finalize` currently return success-compatible Noxy
values while some underlying Go close errors are discarded. Changing those
public behaviors or adding strict close APIs is separate work. Here, "cleanup
failure" means a runtime error observable from the deferred call. Tests
combine real resources with a controlled failing native to prove that such a
failure never prevents the remaining resources from closing.

`io.close_result(...)` also returns an ordinary result value. When deferred,
that value is discarded and does not become an unwind error.

## 10. Edge Cases

- An argument-evaluation error does not register the incomplete defer.
- A dynamic callee that is not callable fails at registration, after earlier
  deferred calls have already been retained.
- A deferred constructor uses existing constructor capture semantics: field
  values are evaluated and retained directly without `copyValue`; its
  constructed value is discarded. A composite mutated after registration
  therefore follows the same identity behavior as an immediate constructor.
- A cleanup may register more cleanup work in its own frame. That nested work
  finishes before the owner proceeds to its next entry.
- There is no arbitrary fixed defer-count limit. Entries use heap-backed frame
  storage and remain subject to ordinary process memory limits.
- Internal Go panics are not converted into unwind outcomes by this feature.
- A detached spawned function executes its own deferred calls, but its final
  error remains subject to the existing detached reporting behavior.
- Returning a reference to a local retains existing return-reference rules;
  the local and its upvalue remain alive throughout cleanup and are closed only
  after the deferred stack is empty.

## 11. Testing Strategy

All behavior is developed test-first.

### 11.1 Frontend and compiler

- recognize `defer` as a keyword and preserve its source location;
- parse a valid deferred call;
- reject non-call expressions and unsupported pseudo-calls;
- verify ordinary call type, arity, and reference diagnostics still apply;
- verify disassembly and exact `OP_DEFER` operands;
- verify specialized `append`, `pop`, `delete`, `json_loads`, `chan_send`, and
  `chan_recv` paths can select deferred emission while `addr` is rejected;
- verify argument evaluation order and failure before registration.

### 11.2 Runtime semantics

- prove immediate callee and argument evaluation;
- prove shallow-copy capture and `ref` preservation;
- prove LIFO ordering for explicit return, implicit return, script, module,
  loop registrations, and a spawned function;
- prove deferred results are discarded;
- prove deferred closures can read captured locals and upvalues;
- prove script locals address from `LocalBase = 1` while teardown clears from
  `StackBase = 0`;
- prove the preparation matrix for signed/unsigned natives and constructors;
- prove a cleanup can register and execute its own deferred calls.

### 11.3 Error and boundary behavior

- unwind division-by-zero and contextual-native errors;
- aggregate several cleanup errors without skipping older entries;
- convert a normal return into an error when cleanup fails;
- preserve the original error through `errors.Is` and `errors.As`;
- retain registration and execution locations without duplicate formatting;
- keep an importing frame alive while an inner module run unwinds;
- keep an owner frame alive while a failing Noxy cleanup unwinds;
- preserve a nested cleanup aggregate as one outer deferred cause;
- wrap a failing module without flattening or duplicating its error locations;
- persist and restore IP across a recursive import and a prepared Noxy call;
- restore stack base after pre-call validation failure, native failure, and
  Noxy cleanup failure.

### 11.4 Runtime invariants and resources

- assert `frameCount`, `currentFrame`, frame-array entries, `stackTop`, cached
  IP, and `openUpvalues` after normal and error exits;
- assert main-frame completion leaves `stackTop == 0`, `frames[0] == nil`, and
  no closure residual in `stack[0]`;
- interpret another program successfully on the same VM after a failure;
- close a real temporary file on normal return and runtime error, verify its
  registry entry and closed state, and then remove it as a secondary check;
- close real sockets and listeners and verify registry removal;
- finalize a real SQLite statement before closing its database and verify both
  registries are empty;
- place a controlled failing native between resource cleanups and prove all
  remaining resources still close in LIFO order;
- execute a nested import whose module defer fails while the importer owns
  multiple deferred calls;
- defer a struct constructor with a composite argument mutated after
  registration and verify the documented direct-capture behavior.

### 11.5 Project validation

The completed implementation must pass:

```powershell
gofmt -w <modified-go-files>
go test ./internal/...
go test ./...
go vet ./...
go build ./...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Targeted VM tests run before the full suite, and race-sensitive resource tests
also run under `go test -race` where practical.

## 12. Documentation

`docs/NOXY_LANGUAGE_SPEC.md` will document syntax, immediate evaluation,
shallow-copy/ref capture, frame-level LIFO order, script/module behavior,
return-to-error conversion, and the distinction between runtime errors and
ordinary result values.

`CHANGELOG.md` will record the new language feature and safe error unwind. A
focused Noxy example will demonstrate nested file and SQLite cleanup without
adding an interactive or long-running integration test.

## 13. Success Criteria

The subproject is complete when:

1. every exiting frame executes its registered calls exactly once in LIFO
   order;
2. normal return finalizes exactly one frame, while every surfaced runtime
   error unwinds only to the active boundary;
3. cleanup failure never skips older deferred calls;
4. original and cleanup errors retain structured identity and source context;
5. frame, stack, IP, and upvalue invariants remain valid and the VM is reusable;
6. real file, network, and SQLite resources can be deterministically closed by
   deferred calls on both success and error paths;
7. existing public resource ownership and close APIs remain compatible;
8. all targeted, full, race-sensitive, build, vet, and Noxy integration checks
   pass.
