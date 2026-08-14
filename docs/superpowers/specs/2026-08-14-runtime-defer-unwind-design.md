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
defer net.close(socket)
defer sqlite.finalize(statement)
```

`defer` is valid in functions, the main script frame, module initialization,
and functions executed by `spawn`. It is not a lexical-scope cleanup: a
`defer` inside an `if` or loop belongs to the containing call frame. A loop
registers one entry for every executed iteration.

Arbitrary expressions and pseudo-calls that compile without `OP_CALL` are
rejected. In particular, `defer value`, `defer 1 + 2`, and `defer addr(ref x)`
produce compile errors. Compiler-special operations such as channel
pseudo-calls are rejected unless they are later represented as ordinary
callables and emit the standard call sequence.

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

Compiler-special builtins that directly emit collection, channel, reference,
or other dedicated opcodes cannot use the second stage and are rejected in a
defer statement with a targeted diagnostic.

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
	// Existing closure, IP, slots, and environment fields.
	Deferred []PreparedCall
}
```

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

### 7.1 Boundary contract

Unwind is defined as `unwindFrames(targetFrameCount, outcome)`: remove frames
until exactly `targetFrameCount` frames remain. Frames below that boundary are
never touched by the operation.

This single contract covers all execution contexts:

- main interpretation unwinds to zero frames;
- recursive module execution unwinds only the module frames and leaves the
  importing frame intact;
- a Noxy cleanup call unwinds only frames created by that cleanup and leaves
  the frame that owns the remaining deferred entries intact;
- detached spawn execution unwinds its independent VM to zero frames.

The current recursive `run(minFrameCount)` behavior maps to a target of
`minFrameCount - 1`. A module error is first unwound to its caller boundary and
returned to `OP_IMPORT`; the outer run then treats it as its own runtime failure
and unwinds its remaining frames. This sequencing ensures that cached
`frame`, chunk, and instruction-pointer state never references a frame removed
by an inner run.

### 7.2 Frame transition

For each frame being removed, the machine performs these steps in order:

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

The caller's IP is saved before every immediate or prepared call. A frame is
never popped by `OP_RETURN` itself; that opcode only constructs a return
outcome and transfers control to the unwind machine. Runtime instruction
failures follow the same transfer path rather than performing separate cleanup.

If a deferred Noxy function fails, its own frames and deferred entries unwind
to the owner boundary. The returned error is recorded against the owner's
registration entry. The owner remains alive and proceeds with its older
entries.

### 7.3 Return-to-error conversion

A normal return remains successful only if every deferred call succeeds. The
first cleanup failure converts the outcome to an unwind error. That failure is
then propagated through caller frames, so deferred calls in those frames also
run. Explicit returns, implicit void returns, script completion, and module
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
exists. `UnwindError` preserves the primary error as its principal unwrap
cause; when a normal return has no primary error, its first deferred failure is
the unwrap cause. All deferred failures remain available through a typed
accessor and in deterministic LIFO execution order.

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

let db: sqlite.Database = sqlite.open(path)
defer sqlite.close(db)
let stmt: sqlite.Statement = sqlite.prepare(db, query)
defer sqlite.finalize(stmt)
```

The SQLite order is intentional: database close is registered first so
statement finalization executes first. Socket and listener handles use the
same explicit pattern.

This version does not claim to aggregate operating-system close failures that
existing builtins suppress. `io_close`, `net_close`, `sqlite_close`, and
`sqlite_finalize` currently return success-compatible Noxy values while some
underlying Go close errors are discarded. Changing those public behaviors or
adding strict close APIs is separate work. Here, "cleanup failure" means a
runtime error observable from the deferred call. Tests combine real resources
with a controlled failing native to prove that such a failure never prevents
the remaining resources from closing.

## 10. Edge Cases

- An argument-evaluation error does not register the incomplete defer.
- A dynamic callee that is not callable fails at registration, after earlier
  deferred calls have already been retained.
- A deferred constructor is allowed only if it uses the ordinary `OP_CALL`
  path; its constructed value is discarded.
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
- verify argument evaluation order and failure before registration.

### 11.2 Runtime semantics

- prove immediate callee and argument evaluation;
- prove shallow-copy capture and `ref` preservation;
- prove LIFO ordering for explicit return, implicit return, script, module,
  loop registrations, and a spawned function;
- prove deferred results are discarded;
- prove deferred closures can read captured locals and upvalues;
- prove a cleanup can register and execute its own deferred calls.

### 11.3 Error and boundary behavior

- unwind division-by-zero and contextual-native errors;
- aggregate several cleanup errors without skipping older entries;
- convert a normal return into an error when cleanup fails;
- preserve the original error through `errors.Is` and `errors.As`;
- retain registration and execution locations without duplicate formatting;
- keep an importing frame alive while an inner module run unwinds;
- keep an owner frame alive while a failing Noxy cleanup unwinds;
- restore stack base after pre-call validation failure, native failure, and
  Noxy cleanup failure.

### 11.4 Runtime invariants and resources

- assert `frameCount`, `currentFrame`, frame-array entries, `stackTop`, cached
  IP, and `openUpvalues` after normal and error exits;
- interpret another program successfully on the same VM after a failure;
- close and remove a real temporary file on normal return and runtime error;
- close real sockets and listeners and verify registry removal;
- finalize a real SQLite statement before closing its database and verify both
  registries are empty;
- place a controlled failing native between resource cleanups and prove all
  remaining resources still close in LIFO order.

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
2. normal return and every surfaced runtime-error path use bounded central
   unwind;
3. cleanup failure never skips older deferred calls;
4. original and cleanup errors retain structured identity and source context;
5. frame, stack, IP, and upvalue invariants remain valid and the VM is reusable;
6. real file, network, and SQLite resources can be deterministically closed by
   deferred calls on both success and error paths;
7. existing public resource ownership and close APIs remain compatible;
8. all targeted, full, race-sensitive, build, vet, and Noxy integration checks
   pass.
