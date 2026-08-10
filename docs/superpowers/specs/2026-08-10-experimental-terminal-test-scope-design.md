# Experimental Terminal Test Scope

## Context

The `terminal` standard-library module is already implemented for personal
experimentation. Its API is intentionally experimental because Noxy syntax,
error representation, and resource-management conventions may still change.

The current pull request tests several provisional details as durable
contracts. This makes the test suite expensive to maintain while the language
is evolving. At the same time, terminal raw mode has safety consequences: the
VM must not leave the user's terminal in a broken state after execution ends.

## Decision

Keep tests that protect user-visible safety invariants and the smallest useful
module flow. Remove tests that freeze synchronization mechanics, internal
representation, defensive paths unreachable from typed Noxy code, or exact
error-composition behavior.

Experimental means the API may change; it does not mean terminal cleanup may
be unsafe.

## Coverage to Keep

- The embedded `terminal` module compiles and exposes typed bindings.
- Terminal builtins remain registered with their declared signatures.
- A basic `open_raw` -> `read_key` -> `close` session succeeds.
- Opening raw mode rejects non-terminal input.
- Reading requires an active raw session.
- Representative keys are normalized as currently documented.
- Repeated open and close operations do not duplicate driver transitions.
- Root interpretation restores terminal state after success and runtime error.
- `sys.exit` restores shared terminal state before exiting.
- Child VMs share the terminal runtime used by the parent VM.

## Coverage to Remove or Simplify

- Remove the queued-read concurrency test and its stack-inspection helpers.
  Lock scheduling and the exact behavior of reads queued during `close` are
  implementation details, not an experimental public contract.
- Remove exact precedence tests for simultaneous execution and restoration
  failures.
- Remove builtin tests for malformed internal calls and typed-nil struct
  definitions. Typed Noxy programs cannot produce these calls through the
  public module API.
- Remove duplicated EOF and close-failure cases from the builtin layer when the
  underlying runtime behavior is already covered.
- Remove the `sys.exit` restore-failure edge case; retain the normal cleanup
  invariant.
- Where a restore failure remains covered, assert the returned failure rather
  than private fields such as `raw` and `saved`.

## Boundaries

This change only reduces and simplifies tests. It does not alter the terminal
API, VM behavior, standard-library module, Space Invaders example, or language
documentation.

There is no target line count. The goal is for each remaining test to protect a
distinct invariant that should survive likely syntax and API changes.

## Verification

Run:

```text
go test ./internal/...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Success means both commands pass, no VM test references Space Invaders, and the
removed tests do not require production changes.
