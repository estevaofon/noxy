# `call_result` — Synchronous Error Boundary Design

## Goal and Scope

Noxy has a deliberate two-channel error philosophy: raising is for program
bugs, `_result` structs are for untrusted data (`NOXY_LANGUAGE_SPEC.md`,
conversions section). The philosophy is sound, but the language does not close
over it: every `_result` twin in existence is born in Go
(`internal/vm/builtins_convert.go`), because no Noxy code can observe a runtime
error and survive. The evidence is `noxy_examples/int_to_result_noxy.nx`, where
reimplementing `to_int_result` in pure Noxy required ~55 lines reimplementing
`strconv.ParseInt` by hand — not because the parse is interesting, but because
`to_int` raises and nothing can catch.

The language already believes in catching errors at boundaries: a runtime
error inside a supervised task does not kill the parent, and `task_await`
reports it as a structured value. That boundary is asynchronous and costs a
child VM. This design adds the synchronous, per-call form of the same
boundary.

**Goal:** user code can build its own `_result` twins; the stdlib's twins
become expressible (not necessarily implemented) in pure Noxy.

**Non-goals:**

- `try`/`catch`/`rescue` syntax. A block construct invites using the bug
  channel as the data channel. If an expression form is ever added, it must
  compile down to this boundary, not compete with it.
- A generically typed `Result<T>` envelope in v1. The decisive fact:
  `call_result` is a native, and monomorphization (§6) reaches only Noxy code
  — no generic signature can bind it. Supporting facts, verified against the
  binary: `null` does not satisfy primitive contracts (§4.2), so a
  `Result<int>` error case has no constructible `value`; `any` is not a valid
  instantiation target (§6.5); and the enclosing function's declared return
  type does not anchor a generic call in a `return` statement (`return
  vazia()` inside `func f() -> Caixa<int>` fails to infer `T` — a `let`
  annotation does anchor, which cannot help a native). `value: any` is the
  documented dynamic floor for callables whose result is not statically known
  (§4.2), and it is the cheap-to-deprecate choice if sum types ever arrive.
- Migrating the existing native twins (`to_int_result`, `to_float_result`) as
  part of this change. See "Migration and performance policy".

**Design history.** A first draft carried `error: string` and was revised
after independent review: the string collided with the task boundary's
existing failure shape (`kind`, `message`, `stack`) and with the defer
section's requirement that deferred failures remain "nested, structured causes
rather than being flattened into one message". A second independent review of
the revised spec confirmed those resolutions and demanded: a decision on the
envelope's physical representation, a position on `stdlib/result.nx`, the
`spawn_task` analogy corrected, and destinations for the defer registration
location and the constructor/frame-overflow edge cases — all incorporated
below. A comparison with Rust's `catch_unwind` contributed the discarded-
envelope diagnostic, the native-frame traversal rule, and the unwind-safety
guidance.

## 1. The mechanism

One global native and two stdlib struct shapes:

```noxy
// stdlib/errors.nx
struct Failure
    kind: string       // "runtime" | "panic"
    message: string
    stack: string
    causes: Failure[]  // deferred failures aggregated during unwinding, LIFO
end

struct CallResult
    ok: bool
    value: any         // fn's return value; null for void and on failure
    failure: Failure   // null when ok
end
```

```noxy
call_result(fn, ...args)   // global native, like spawn_task
```

`call_result` invokes `fn` with `args` in the current routine, synchronously,
on the current frame stack. If the call completes, the envelope carries its
value. If a runtime failure unwinds out of it, the unwinding stops at the
`call_result` frame and the envelope carries a `Failure`.

The name closes the existing convention — `io.close`/`io.close_result`,
`to_int`/`to_int_result`, `call`/`call_result` — and the envelope reuses the
failure vocabulary the task boundary already defined. `call_result` is not a
second error idiom; it is the factory for the first one (and §4 retires the
accidental third one).

## 2. Normative spec section

The text below is written to merge into `NOXY_LANGUAGE_SPEC.md`. Placement:
the philosophy subsection replaces/absorbs the "raising vs `_result`" passage
currently embedded in the conversions section (which then references it); the
boundary subsection goes immediately after "Defer and deterministic cleanup",
which it depends on.

Merge checklist for the same spec change: register the `errors` module in §12
(Standard Library); update §5 to sanction struct self-reference through an
array field without `ref` (`causes: Failure[]` — verified against the current
binary: it compiles, constructs, and nests; value semantics without `ref`
cannot form cycles).

---

### Errors: raise for bugs, results for data

Noxy has two failure channels, and they are not interchangeable.

A **runtime error** means the program is wrong: an impossible conversion of a
value the program itself produced, an out-of-range index, a violated native
contract. It unwinds call frames, runs deferred calls along the way, and —
unobserved — terminates the script with a diagnostic.

A **result struct** means the input was allowed to be bad: untrusted text,
user input, wire data. Failure is an expected outcome, so it is data — an
`ok` flag the caller must branch on. Because Noxy functions return a single
value, the result struct occupies the place of Go's `value, err` pair.

API design rule: an operation whose failure indicates a caller bug raises; an
operation whose failure is an expected outcome of untrusted data returns a
result struct. When both kinds of caller exist, provide the raising form and a
`_result` twin: `to_int` / `to_int_result`, `io.close` / `io.close_result`.

Two boundaries convert the first channel into the second, and they are the
only two: the supervised-task boundary (`spawn_task` / `task_await`), and the
synchronous `call_result` described next. Advisory hints such as "use
to_int_result to handle failure" belong to the top-level fatal diagnostic
output, not to the raised message itself — a captured `Failure.message`
carries the error, not usage advice.

### The error boundary: `call_result`

`call_result(fn, ...args)` invokes a callable and converts a runtime failure
that unwinds out of it into a value:

```noxy
use errors select *

let r: CallResult = call_result(to_int, entrada)
if r.ok then
    let n: int = to_int(r.value)   // narrowing only: r.value is already int
    print(n)
else
    print("entrada inválida: " + r.failure.message)
end
```

**Callable and validation.** `fn` may be any callable value: a typed Noxy
function or closure, a function value held as an exact or bare `func`, a
struct constructor, or a native. (A native reaches the boundary as `any` or
in argument position; the compiler does not admit `let f: func = to_int`.)
Passing a non-callable is a synchronous runtime error in the caller.
Arguments are evaluated in the caller's frame, before the boundary exists; an
error during argument evaluation is not captured. Where the callee exposes
parameter metadata — typed Noxy functions and closures, struct constructors,
signed natives — arity and parameter-mode mismatches are synchronous runtime
errors in the caller. The timing mirrors `spawn_task`'s synchronous
validation; the domain is wider — `spawn_task` accepts only Noxy functions
and closures, while `call_result` also accepts constructors and natives.
Callees without metadata (legacy untyped natives) validate during invocation,
so those failures are captured and are indistinguishable from failures of the
callee's own body. *Compatibility note:* giving a legacy native a signature
in a later release moves its misuse failures from captured to synchronous —
an observable change that rides the signing, and one more reason to sign
natives eagerly.

For a struct constructor, a completed call yields the constructed instance as
`value`, under the constructor semantics the defer section already gives that
callee category.

**Argument semantics are unchanged.** Arguments follow §4.3 exactly as in a
direct call: composite values are independent copy-on-write values, explicit
`ref` arguments keep reference identity. `call_result` adds no isolation — it
is the same call, wearing a boundary.

**Envelope.** `call_result` returns a `CallResult` (module `errors`):

- `fn` completes: `ok = true`, `value` is its return value (`null` for a
  `void` return), `failure = null`. A composite `value` preserves the identity
  an ordinary call would give it.
- A runtime failure unwinds out of `fn`: deferred calls registered by `fn` and
  its nested frames run under the normal unwinding rules, the unwinding stops
  at the `call_result` frame, and the envelope is `ok = false`,
  `value = null`, with `failure` describing the primary error.
- `failure.kind` is `"runtime"` for a Noxy runtime error (with a Noxy stack
  captured at the failure point) or `"panic"` for a recovered Go panic (with a
  Go stack) — the same vocabulary as the task boundary. A `"runtime"` stack
  covers the frames from the failure point down to, and excluding, the
  `call_result` frame itself.
- `failure.causes` holds deferred failures observed during that unwinding, in
  LIFO execution order, each a `Failure` whose own `causes` nest further, per
  the defer section's aggregation rules. It is empty when no deferred call
  failed. Each cause's `stack` is captured at that deferred failure point and
  carries the deferred call's registration location as its outermost frame —
  the envelope form of the defer section's promise that each deferred failure
  is collected "with its registration location".
- Cleanup as first failure: if `fn` returns successfully but one of its
  deferred calls raises, the boundary reports `ok = false` and the computed
  return value is discarded, mirroring "converts an otherwise successful
  return into a runtime error" in the defer section. The first deferred
  failure is the primary `failure`; deferred failures observed after it
  aggregate under the primary's `causes` in LIFO order, each nesting its own
  `causes` further.

**Representation.** `call_result` is a native, and natives return values
across the dynamic boundary (§4.2). Like every result envelope a native
produces today (`convert_to_int_result` and its `IntResult`), the envelope is
physically a map whose fields match the declared shapes; the `errors` module
declarations exist so Noxy code can annotate (`let r: CallResult = ...`) and
so the field names are a compile-checkable contract at typed use sites. The
consequences are observable and identical to the existing `IntResult`
precedent: `fmt("%T", r)` reports `map`, and the envelope does not compare
equal to a hand-constructed `CallResult(...)` instance. Promoting the
envelopes (this one and `task_await`'s) to genuine struct instances is a
single future change, gated on natives being able to construct
stdlib-declared structs — see "Relation to the task boundary".

**What the boundary does not change.**

- The caller's own deferred calls and frames are unaffected; after
  `call_result` returns, execution continues normally.
- Boundaries nest: a failure is captured by the nearest enclosing
  `call_result` frame.
- Detached routines started by `fn` via `spawn` keep running after capture,
  per the `spawn` contract.
- `call_result` is an ordinary call and is legal anywhere a call is,
  including module initialization; frame rules follow the defer section
  unchanged.
- **Through native frames.** A failure raised in Noxy code that a native
  invoked (a callback, an HTTP handler) propagates through the intervening
  native frame to the nearest boundary; the native observes the failure
  through its existing error path and releases its resources before
  propagation continues. A callback-invoking native must forward Noxy
  failures, never swallow them.
- **No rollback.** Mutations `fn` performed before failing — through `ref`
  arguments, globals, closure upvalues, or native resources — remain. Noxy's
  copy-on-write value semantics can suggest that everything is isolated; the
  boundary is control flow, not a transaction. The risky shape has a name:
  a callable that mutates `ref` arguments, globals, or upvalues can leave a
  broken invariant behind when captured mid-flight — prefer value-in/value-out
  callables under a boundary, and treat mutating ones as candidates for
  explicit coordination, exactly as across the task boundary.
- **Panic coverage.** Go panics are recovered in the executing routine only,
  not in independent goroutines started by native code — the same coverage
  rule as supervised tasks. Exhaustion of the VM's Noxy frame limit is an
  ordinary runtime error and is capturable (`kind = "runtime"`): by the time
  the boundary observes it, unwinding has already freed frames. Conditions
  the Go runtime treats as unrecoverable (fatal errors such as concurrent map
  writes, Go stack exhaustion, out of memory) remain process-fatal; no
  boundary observes them.

**Intended idiom.** `call_result` exists to build named `_result` twins, not
to decorate call sites. The inline form over a closure is legal:

```noxy
let r: CallResult = call_result(func() -> int
    return to_int(campo) * fator
end)
```

but the idiom is wrap-and-name: a `_result` function with a typed result
struct is a contract; an inline boundary is a site the reader must decode. The
boundary's design leans on auditability — `call_result` is one grep away from
every place errors become data — and on the envelope being deliberately
untyped (`value: any`), so a named twin that narrows the value is always the
more comfortable form. Discarding the envelope — `call_result(f, x)` as a
bare statement — swallows every failure `f` can produce and is almost
certainly a bug; a compile-time diagnostic for it is tracked as follow-up.

---

## 3. Relation to the task boundary

`task_await`'s `"error"` envelope carries a failure map with string fields
`kind`, `message`, and `stack`. `Failure` is that shape, promoted to a named
declaration and extended with `causes`. Direction, not part of this change:
`task_await` adopts the same shape (`causes` is additive on the existing map;
the map-to-struct promotion is the breaking part and rides a major version).
The honest obstacle to full unification is machinery, not intent: the task
envelope is built by the VM in Go, and constructing a genuine
stdlib-declared struct from native code is capability the VM does not have
today — the same gate recorded under "Representation". Until then the two
boundaries share vocabulary and semantics, not a nominal type.

The differences that remain are real and intended: `call_result` is
synchronous, runs on the current frame stack with no child VM, provides no
timeout, and returns exactly once — there is no completion replay.

## 4. The `result` module is absorbed

`internal/stdlib/result.nx` predates this design: `Result{value: any,
err: Error{code, msg}}` with `Ok`/`Err`/`is_ok`/`is_err`/`unwrap`, whose own
comment concedes "we don't have panic/unwrap safely". It is a third failure
vocabulary — after the `_result` structs and the task failure map — with a
single consumer in the tree, `noxy_examples/result_pattern.nx`.

This design retires it: `Failure` supersedes `Error` (kind/stack/causes are
strictly richer than code/msg), and the `_result` convention supersedes
`Result`. The module is marked deprecated in the release that ships
`call_result` and removed in the following one; the example migrates to a
named `_result` twin over `call_result` in the same change. Without this,
`call_result`'s claim of not being a second error idiom would be false — it
would be the third.

## 5. Migration and performance policy

- **The immediate value is user-side capability.** New `_result` twins —
  stdlib and user — can be written in Noxy against raising primitives.
- **Hot native twins stay native until a benchmark approves.** Rewriting
  `to_int_result` over `call_result` turns 1 native frame into ~4 frames plus
  an envelope allocation, in functions that live inside parse loops, in a
  project whose current performance front is per-call validation cost.
  `convert.nx` may migrate as a proof of concept only if the numbers hold.
- **Raised-message cleanup.** `to_int`'s raised message currently embeds the
  advisory suffix "; use to_int_result to handle failure"
  (`builtins_convert.go`). The suffix moves to the fatal diagnostic output;
  the capturable message stays clean. This is a small observable change to
  fatal output wording and lands with the builtin.

## 6. Alternatives and precedent

- **Closure-only form** (`call_result(fn)` with no varargs) is Rust's choice
  for `std::panic::catch_unwind`, and Rust could afford it: its
  monomorphization reaches the standard library, so the closure types the
  success side. Noxy gets no such benefit (the boundary is a native), the
  varargs form is a strict superset (a zero-arg closure still works), and
  `spawn_task(function, ...arguments)` already fixed the convention. Varargs
  stands.
- **Rust parallels that support the shape as designed.** The most statically
  typed of the three languages also left its boundary payload dynamic
  (`Box<dyn Any>`); its documentation confines the boundary to boundaries
  ("catch_unwind is not recommended for general error handling") — the
  wrap-and-name idiom; `UnwindSafe` is the type-system form of the no-rollback
  guidance (and even Rust could do no better than a bypassable marker);
  `JoinHandle::join` returning the panic payload is `task_await`; aborts stay
  fatal there as Go-fatal stays fatal here.
- **`rescue`/`or` expression syntax**: out of scope; if ever added, it
  compiles down to this boundary rather than competing with it.
- **Error as plain string**: rejected in review round one — it collided with
  the task boundary's structured shape and the defer section's structured
  aggregation, and a string propagated into every `_result` signature would be
  the one irreversible choice in this design.

## 7. Open questions and follow-ups

- **`errors` module name.** `CallResult` and `Failure` need a home;
  `stdlib/errors.nx` is proposed. The native `call_result` is global (like
  `spawn_task`), so the import is only needed to *annotate* — dynamic access
  (`let r: any = call_result(f)`) works without it, which keeps casual use
  cheap and typed use explicit.
- **Discarded-envelope diagnostic.** `call_result(...)` in statement position
  with the envelope discarded is the empty-catch antipattern; a compile-time
  diagnostic (precedent: Rust's `#[must_use]`) is future work — the compiler
  knows the native's name.
- **Callback-native audit.** The "through native frames" rule requires every
  callback-invoking native to forward Noxy failures; implementation must
  audit the existing ones (HTTP server handlers foremost) and add a
  regression test per category.
- **Typed variant later.** If sum types or native-reaching generics arrive, a
  typed boundary (`Ok(T) | Err(Failure)`) can coexist; `CallResult` is an
  ordinary shape and deprecates cheaply. Nothing in this design leaks a poor
  error type into user signatures — `Failure` is already the rich form.
- **`fmt("%T", ...)` / `type_of`.** Building twins over `any` inputs needs
  runtime type dispatch; today that is the undocumented `%T` verb. The
  companion proposal (a documented `type_of` builtin) is tracked separately.
