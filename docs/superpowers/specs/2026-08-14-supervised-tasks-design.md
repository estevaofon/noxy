# Supervised Tasks Design

**Date:** 2026-08-14

**Status:** Approved for implementation planning

## Goal

Resolve point 3 from PR #17 by adding supervised tasks that expose a stable,
opaque handle, replay a terminal result or failure to any number of waiters,
support non-terminal timeouts, and recover internal panics. The existing
detached `spawn` builtin remains compatible.

## Public API

The feature adds two dynamic builtins. It does not add syntax, opcodes, or a
source-level `task` type.

```noxy
let task: any = spawn_task(worker, argument)

// Wait indefinitely.
let terminal: any = task_await(task)

// Wait for at most 500 milliseconds.
let bounded: any = task_await(task, 500)
```

`spawn_task(function, ...arguments)` accepts only Noxy functions and closures.
It validates the callable, arity, and parameter modes before returning a
handle. Invalid input is a synchronous runtime error in the calling VM and
does not create a task.

`task_await(handle)` waits until the task reaches a terminal state.
`task_await(handle, timeout_ms)` waits for at most the supplied number of
milliseconds. A zero timeout is an immediate poll. A negative timeout, a
non-integer timeout, or an invalid handle is a synchronous runtime error in
the calling VM.

## Result Contract

Every successful `task_await` call returns a fresh `map[string, any]` envelope
with exactly the following semantic fields:

| Status | `value` | `error` |
|---|---|---|
| `"ok"` | The task's return value, including `null` for a void return | `null` |
| `"error"` | `null` | A structured failure map |
| `"timeout"` | `null` | `null` |

The failure map has these fields:

| Field | Meaning |
|---|---|
| `kind` | `"runtime"` for a Noxy runtime error or `"panic"` for a recovered Go panic |
| `message` | The original error or panic message |
| `stack` | A Noxy stack captured at the failure point for runtime errors, or `debug.Stack()` for panics |

The implementation retains the original Go error internally rather than only
its rendered message. This preserves an integration seam for typed or
aggregate unwind errors added later.

## Opaque Handle and Terminal State

The runtime adds `VAL_TASK` and `ObjTask`. Noxy code can store the value behind
`any`, pass it around, print it as an opaque task handle, and compare handles
by identity, but cannot construct or inspect `ObjTask` directly.

An `ObjTask` owns:

- a `done chan struct{}` used only as a completion broadcast;
- a private immutable terminal outcome containing either a return value or a
  failure;
- a single-publication guard.

The worker stores its complete terminal outcome before closing `done`. Closing
the channel publishes the stored outcome to all current and future waiters.
Publication happens exactly once even if panic recovery is involved.

Each wait creates a new public envelope, so user mutation cannot corrupt the
task's private terminal state. The returned `value` preserves the identity and
normal return semantics of the original Noxy value. Multiple waits therefore
mean the same outcome and the same returned-value identity, not independent
deep snapshots. Shared arrays, maps, structs, references, and closure upvalues
still require explicit concurrency coordination.

## Task Execution Boundary

Task execution uses a new internal closure execution entrypoint with an
explicit `(value.Value, error)` result. It must not discover the result by
peeking or popping incidental stack state after `run` returns. This entrypoint
owns preparation of the child VM, frame setup, execution, and extraction of
the terminal return value.

The child VM shares the caller's `SharedState`, VM configuration, closure, and
global environment. Before the goroutine starts, call arguments are validated
with the normal callable contract. Non-`ref` parameters receive Noxy's normal
top-level shallow copy; `ref` parameters preserve the supplied reference.

This execution entrypoint is a stable seam for later runtime unwind work. It
must continue returning the terminal value explicitly if unwind cleanup later
clears the child stack.

## Failure and Panic Handling

A Noxy runtime error terminates only the supervised task. The worker captures
the Noxy stack while its frames still describe the failure, before any unwind
or cleanup can clear them, and publishes a `runtime` failure.

The task goroutine has a top-level `recover`. A recovered panic becomes a
`panic` failure and includes `debug.Stack()`. Recovery covers only the task's
main goroutine. A native that starts unrelated goroutines owns their panic
boundaries; `spawn_task` cannot recover panics in those goroutines.

Every exit path publishes a terminal outcome and closes `done`, so a runtime
error or recovered panic cannot leave waiters blocked forever.

## Timeout Semantics

Timeout is local to one `task_await` call. It does not cancel the task, consume
its result, mutate its state, or prevent a later wait.

Completion is preferred whenever it is observably available before
`task_await` returns. The wait algorithm therefore:

1. checks `done` before creating or considering a timeout;
2. uses `time.NewTimer` for positive bounded waits;
3. schedules `timer.Stop()` when the bounded wait returns;
4. checks `done` again after the timer fires before returning `"timeout"`.

This prevents a terminal task from randomly returning timeout when both the
timer and `done` are ready, including zero-time polls. Millisecond conversion
must reject values that overflow `time.Duration`.

## Detached Spawn Compatibility

The existing `spawn` builtin remains detached and retains its current public
behavior:

- it returns `null` immediately;
- it does not expose a handle or propagate a worker result;
- it prints its existing validation, runtime-error, and panic diagnostics to
  the same destinations;
- a worker failure does not become an error in the caller.

`spawn_task` must not silently change `spawn` by routing the legacy builtin
through stricter validation or argument-copy behavior. Shared low-level code
is acceptable only where characterization tests prove the observable legacy
behavior remains identical.

## Alternatives Considered

### Shared registry with numeric handles

A runtime-wide task registry would allow integer handles, but introduces
forgery, retention, eviction, and cleanup policy that this feature does not
need. The object handle carries its own lifecycle and becomes collectible when
Noxy code releases it.

### Result channel as the handle

A one-shot result channel fits the existing concurrency primitives but makes
the first receive consume the result. Supporting repeated waits would require
another cache layered over the channel, effectively recreating `ObjTask` with
a less precise public contract.

### Raising worker failures from `task_await`

Propagating an awaited failure as a caller runtime error would terminate the
caller because Noxy has no catch mechanism. A structured envelope lets the
caller inspect and recover from task failures while preserving synchronous
runtime errors for misuse of the task API itself.

## Testing Strategy

Implementation follows test-first development. VM tests cover:

- successful primitive, composite, `null`, and void results;
- repeated sequential waits and concurrent waiters;
- preserved identity for returned composite values;
- runtime failure message and Noxy stack capture;
- recovered panic message and stack capture;
- a timeout followed by a later successful wait;
- zero-time polling before and after completion;
- completion winning at the deadline boundary;
- invalid callable, arity, parameter mode, handle, and timeout;
- duration overflow rejection;
- shared globals, module environment, runtime state, and VM configuration;
- shallow-copy behavior for ordinary parameters and identity for `ref`;
- task handle formatting, identity equality, reference validation, JSON
  rejection, and runtime type reporting;
- race-detector coverage for publication and multiple waiters.

Characterization tests lock down detached `spawn` behavior for missing or
invalid callables, wrong arity, immediate `null` return, diagnostic text and
destination, runtime errors, panics, and absence of propagation to the caller.

An executable Noxy example demonstrates success, failure, timeout followed by
success, and repeated waits. The concurrency guide and language specification
document the API and guarantees.

## Out of Scope

- task cancellation or forced interruption;
- supervisor trees, restart strategies, or task groups;
- result-retention registries or explicit handle disposal;
- a public `task` type or generic `Task<T>`;
- catching arbitrary runtime errors outside supervised tasks;
- recovering panics from goroutines started independently by native code;
- changing detached `spawn` behavior.

## Validation

The implementation must pass:

```bash
go test ./internal/...
go test ./...
go test -race ./internal/vm
go vet ./...
go build ./...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```
