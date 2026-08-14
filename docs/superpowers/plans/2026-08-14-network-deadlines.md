# Network Deadlines Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add portable positive network timeouts and restoration of indefinite blocking while preserving the legacy `setblocking(false)` call until true non-blocking I/O lands with the platform poller.

**Architecture:** Shared listener and socket resources store one validated `time.Duration`, where zero means indefinite blocking and positive means a fresh per-operation deadline. A dedicated `deadlineMu` serializes configuration and temporary `net_select` transitions without holding `stateMu` across external setters; a generation revalidation detects concurrent close. Direction locks continue to serialize I/O. The public API adds `net.settimeout`, `setblocking(true)` clears deadlines, and the deprecated `false` branch remains an unconditional no-op.

**Tech Stack:** Go 1.24+, Go `net`, `time`, `errors`, and `os`; Noxy standard-library wrappers and VM tests.

## Global Constraints

- Support only indefinite blocking and positive portable timeouts in this subproject.
- Never simulate non-blocking mode with an expired deadline.
- Preserve `setblocking(false)` as an unconditional deprecated no-op, including for stale handles; true non-blocking belongs to `feat/network-poller`.
- Preserve `Socket`, `NetResult`, `net_accept`, and `net_select` result shapes.
- Keep timeout milliseconds as `int64` through overflow validation; never narrow through `int`.
- Share configuration across related VMs and never hold `stateMu` or registry locks during external deadline setters, close, or blocking I/O.
- Use lock order direction mutex, then `deadlineMu`, then short-lived `stateMu`; configuration starts at `deadlineMu`, and close never acquires `deadlineMu`.
- The latest successfully linearized supported configuration wins for persistent state and pending ordinary I/O, but an active select deadline remains an upper bound that cannot be removed or extended.
- If rollback cannot restore a known live deadline, fail closed and close the resource so pending I/O cannot hang.
- Do not change ordinary `net_select` Read/Accept error handling; only new deadline-management failures are synchronous.

## File Map

| File | Responsibility |
|---|---|
| `internal/vm/resources.go` | Shared listener/socket timeout state. |
| `internal/vm/builtins_net.go` | Validation, configuration, deadline application/restoration, and error normalization. |
| `internal/vm/builtins_net_test.go` | Real and controlled deadline, concurrency, partial-I/O, and select tests. |
| `internal/vm/builtins_registry_test.go` | New contextual-native registry guard. |
| `internal/stdlib/net.nx` | Typed `settimeout` wrapper. |
| `docs/NOXY_LANGUAGE_SPEC.md` | Public contract and precedence. |
| `CHANGELOG.md` | Release-facing summary. |
| `.github/workflows/network-deadlines.yml` | Execute public deadline behavior on Windows and Unix. |

---

### Task 1: Shared timeout state and validation

**Files:**
- Modify: `internal/vm/resources.go`
- Modify: `internal/vm/builtins_net.go`
- Test: `internal/vm/builtins_net_test.go`

**Interfaces:**
- Produces: `validateNetworkTimeout(milliseconds int64) (time.Duration, error)`.
- Produces: internal `deadlineListener` combining `net.Listener` and `SetDeadline(time.Time) error`.
- Produces: listener `deadlineMu`, `deadlineGeneration`, `ioTimeout`, `acceptProbeDeadline`, and `lastAcceptDeadline` state.
- Produces: socket `deadlineMu`, `deadlineGeneration`, `ioTimeout`, `readProbeDeadline`, `lastReadDeadline`, and `lastWriteDeadline` state.
- Produces: shared listener/socket handle resolution for configuration natives.

- [ ] **Step 1: Write failing validation and shared-state tests**

Table-test literal inputs `1`, `0`, `-1`, `math.MaxInt64/int64(time.Millisecond)`, and one greater than the maximum. Require exact conversion for safe positives and stable errors for the rest. Configure a socket through a child VM and observe the same field from its parent's shared registry. Verify that a real TCP listener satisfies `deadlineListener`; a controlled listener without `SetDeadline` is closed and `net_listen` returns the existing closed-socket result. Add handle extraction tests requiring `Type == VAL_OBJ`, non-nil `*ObjMap` or `*ObjInstance`, existing exact-`VAL_INT` `fd`, listener-first lookup, and the maximum/minimum platform `int`. Include typed nil map/instance objects. When `strconv.IntSize < 64`, also test representable `int64` values immediately outside the `int` range; on 64-bit, assert all `int64` values round-trip. Require only `fd` to affect lookup while mutated `open`, `addr`, and `port` are ignored. These tests catch panic paths, overflow, narrowing/wrap, invalid zero/negative input, VM-local state, and unvalidated listener capability.

- [ ] **Step 2: Run tests and verify RED**

```text
go test ./internal/vm -run 'TestValidateNetworkTimeout|TestNetworkTimeoutStateIsShared' -count=1
```

Expected: FAIL because state and validation do not exist.

- [ ] **Step 3: Add minimal state and validation**

```go
func validateNetworkTimeout(milliseconds int64) (time.Duration, error) {
    if milliseconds <= 0 {
        return 0, fmt.Errorf("network timeout must be positive")
    }
    const maximum = int64(math.MaxInt64) / int64(time.Millisecond)
    if milliseconds > maximum {
        return 0, fmt.Errorf("network timeout is too large")
    }
    return time.Duration(milliseconds) * time.Millisecond, nil
}
```

Define:

```go
type deadlineListener interface {
    net.Listener
    SetDeadline(time.Time) error
}
```

Store that interface in `ListenerResource` and validate the assertion before registration. Add a dedicated deadline-transition mutex, generation, timeout, active-probe, and last-successful-absolute-deadline fields to each resource. `stateMu` guards lifecycle and metadata but is never held across an external setter. Add one nil-safe parser: require `VAL_OBJ`, type-switch to non-nil `*ObjMap` or `*ObjInstance`, read an existing exact-integer `fd`, verify `int64(int(fd)) == fd`, then look up listener before socket. Ignore `open`, `addr`, and `port`. Effective configuration paths reject malformed, unknown, or closed handles rather than selecting descriptor zero.

- [ ] **Step 4: Run focused tests and verify GREEN**

```text
go test ./internal/vm -run 'TestValidateNetworkTimeout|TestNetworkTimeoutStateIsShared' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```text
git add internal/vm/resources.go internal/vm/builtins_net.go internal/vm/builtins_net_test.go
git commit -m "feat(net): add shared timeout state"
```

---

### Task 2: Read and write deadline behavior

**Files:**
- Modify: `internal/vm/builtins_net.go`
- Test: `internal/vm/builtins_net_test.go`

**Interfaces:**
- Consumes: `SocketResource.ioTimeout`.
- Produces: per-operation read/write deadline helpers.
- Produces: deadline classification through `errors.Is(err, os.ErrDeadlineExceeded)`.

- [ ] **Step 1: Write failing I/O tests**

Use loopback TCP to prove a default read waits for later data and a positive read timeout returns within a generous bound. Use `net.Pipe` or a controlled `net.Conn` for deterministic blocked and partial writes. Require a partial receive plus deadline error to remain successful with its bytes; require a partial send to return `ok=false`, its actual count, and `"operation timed out"`. Add controlled `SetReadDeadline`/`SetWriteDeadline` failures.

Seed `bufferedRead` and prove a fully satisfied receive performs no deadline call. For a larger receive, make deadline installation fail before additional I/O: require the buffered bytes as a successful partial result and no byte loss. Repeat with successful installation followed by a failing `Read`. Add a dialed controlled connection carrying a prior deadline; require `SetDeadline(time.Time{})` before registry insertion and close plus invalid-socket result when clearing fails.

- [ ] **Step 2: Run tests and verify RED**

```text
go test ./internal/vm -run 'TestSocket(Read|Write)(Timeout|Deadline|Partial)|TestBlockingReadWaitsForData' -count=1
```

Expected: FAIL because stored deadlines are not applied.

- [ ] **Step 3: Implement minimal I/O behavior**

Within `readMu`/`writeMu`, acquire `deadlineMu`, briefly lock `stateMu` to validate lifecycle state, capture the connection and mode, and reserve a generation. Unlock `stateMu`, apply zero or `time.Now().Add(ioTimeout)`, then reacquire it to commit the last-installed absolute deadline only if the resource is still open and the generation remains current. Release `deadlineMu` before I/O. Close never waits for `deadlineMu`. Normalize only deadline errors:

```go
func networkErrorMessage(err error) string {
    if errors.Is(err, os.ErrDeadlineExceeded) {
        return "operation timed out"
    }
    return err.Error()
}
```

Inspect buffered bytes before deadline application. Return a fully satisfied buffer without I/O. When more data is required, install the deadline before detaching the buffer; if installation or later Read fails after buffered bytes are available, consume and return them under the partial-read contract. Preserve EOF behavior and report the real byte count on partial send failures. Before registering a dialed connection, clear all deadlines explicitly; close and return the current invalid-socket result on failure.

- [ ] **Step 4: Verify GREEN and existing lifecycle behavior**

```text
go test ./internal/vm -run 'TestSocket|TestBlockingRead|TestNetworkBuiltinsLoopbackLifecycle|TestNetSelectBufferIsSharedAcrossVMs' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```text
git add internal/vm/builtins_net.go internal/vm/builtins_net_test.go
git commit -m "feat(net): enforce read and write deadlines"
```

---

### Task 3: Listener deadlines and select restoration

**Files:**
- Modify: `internal/vm/builtins_net.go`
- Test: `internal/vm/builtins_net_test.go`

**Interfaces:**
- Consumes: listener/socket timeout state.
- Produces: `ListenerResource.acceptProbeDeadline` and `SocketResource.readProbeDeadline` absolute upper bounds.
- Produces: exact last-installed absolute deadline snapshots for rollback.
- Produces: directional live configuration with best-effort exact rollback and persistent commit only after all applications succeed.
- Produces: select cleanup that restores the latest persistent mode or reports an open-resource failure.

- [ ] **Step 1: Write failing listener and concurrency tests**

Cover: timed `accept` returning the existing invalid-socket sentinel; accepted sockets starting with `ioTimeout == 0`; setting a timeout during pending Read/Write/Accept; and clearing a deadline during pending ordinary I/O with `setblocking(true)`. Start probes with persistent timeouts both shorter and longer than the select timeout and assert the exact `min` formula. Prove that concurrent clearing or a longer timeout cannot extend the immutable probe bound, a shorter timeout may shorten it, write configuration remains independent of a read probe, and cleanup restores the latest persistent mode.

Split deadline-management failures into installation failure (no I/O attempted), restoration failure on an open resource (synchronous `net_select` error), and close during probe (skip restoration and report not ready). For both socket and listener, make readiness succeed and restoration fail; require the same candidate's byte/connection to be published before the error and available to the next call. If close wins after Accept, require the accepted connection to close and no buffer publication. Separately inject ordinary `Read` and `Accept` errors and require the current not-ready result plus continued processing of later candidates. Characterize ordinary `Read(n>0, err!=nil)` as unchanged and do not preserve its byte in this task. Use the Task 1 listener interface for ordering and loopback for visible behavior. Add doubles where read application succeeds and write application fails after mutation five seconds into a ten-second pending deadline; require rollback to the exact stored absolute instant, not `now+10s`. Cover successful rollback and repair by the next operation.

When rollback itself fails, require fail-closed behavior for sockets and listeners: `closed` set under `stateMu`, buffers detached and cleared, OS resources closed after unlock, pending Read/Write/Accept/probe released, and an `errors.Join` containing application, rollback, and close failures. The poisoned handle remains registered but all later operations fail promptly until `net_close` removes it. Add a buffered accepted connection and assert it is also closed by listener poisoning.

Run two controlled configurations concurrently and prove their persistent commits follow `deadlineMu` order while pending read/write effects occur at their individual setters. Stall each kind of deadline setter behind a controlled barrier, invoke close concurrently, and require close to mark/detach and call `Close` exactly once without waiting for the setter; release the barrier and require the stale generation to perform no commit, rollback, registration, poisoning, or second close. Race close against Read, Write, Accept, and configuration and require bounded completion without deadlock. In a multi-candidate select, make an earlier probe buffer readiness and a later deadline installation fail; require no `SelectResult`, no later probes, and retention of the earlier buffer. Repeat the later candidate with an ordinary I/O error and require continued processing instead of a synchronous error.

Seed `bufferedAccept` and prove accept performs no listener deadline call. Hold `acceptMu` for delivery, observe the connection under `stateMu`, and keep it published while clearing its own deadline without `stateMu`. On success require revalidation of `!closed && bufferedAccept == connection`, atomic detach, then registration. On clearing failure require the same ownership revalidation followed by destructive detach, connection close outside the lock, invalid-socket sentinel, and an empty buffer. Add success and failure barrier tests where listener close wins during clearing: close owns the detached connection, accept returns the sentinel, no closed connection is registered, and no connection is closed twice. Do not mutate unrelated buffered state.

- [ ] **Step 2: Run tests and verify RED**

```text
go test ./internal/vm -run 'TestListenerTimeout|TestAcceptedSocket|TestPendingNetwork|TestNetSelectRestores|TestConcurrentNetworkConfiguration' -count=1
```

Expected: FAIL because listener modes and non-stale restoration do not exist.

- [ ] **Step 3: Implement linearized configuration/restoration**

```go
type deadlineListener interface {
    net.Listener
    SetDeadline(time.Time) error
}
```

Serialize each transition with `deadlineMu`. Under `stateMu`, snapshot the exact last-successful absolute deadlines, validate the live resource, capture it, compute directional effective deadlines, and reserve a generation; then release `stateMu` before every setter. Reacquire `stateMu` after each setter and before commit. If close changed the generation, do not commit, roll back, poison, register, or close again. At probe start `t`, store `probeBound=t+selectTimeout` and install `min(probeBound, t+ioTimeout)` when persistent timeout is positive, otherwise the probe bound. When configuration runs during a read/accept probe, blocking or a longer timeout preserves that immutable upper bound and a shorter timeout uses the earlier instant. Apply socket read and write deadlines separately so a read probe never blocks write configuration. After each successful, still-current setter, record its exact absolute value. Store `ioTimeout` only after every required setter succeeds and generation revalidation passes; this store is the persistent-mode linearization point.

On application failure while the generation is still owned, retain persistent state and best-effort reapply the exact absolute snapshots without `stateMu`, preserving a probe bound. Never derive rollback from `time.Now().Add(oldTimeout)`. If rollback succeeds, return the primary error and allow later reapplication to repair unknown live state. If rollback fails and the generation is still current, poison the resource under `stateMu`, increment the generation, clear/detach buffers, unlock both mutexes, close every captured OS resource, and return all application, rollback, and close errors with `errors.Join`. If close already invalidated the generation, it owns resource cleanup and the transition performs no second close. This fail-closed path is required because a pending operation may hold its direction mutex and cannot rely on later repair.

Refactor select helpers to distinguish management from ordinary I/O errors. Installation/restoration failures use `(false, error)`; ordinary Read/Accept errors use `(false, nil)`. On a management error, stop later candidates and discard the `SelectResult`, retaining buffers from earlier probes. On ordinary errors, continue candidates, including the unchanged `n>0, err!=nil` path reserved for `feat/network-poller`. Record and install the probe with the reserve/apply/revalidate protocol before I/O. After successful readiness, acquire `deadlineMu`, publish the byte/connection under `stateMu`, and only then restore the latest persistent deadline without `stateMu`; a concurrent close invalidates the generation and owns buffer/resource cleanup. Restoration failure leaves that same buffer published when close did not win. For buffered accept delivery, hold `acceptMu`, observe but do not detach under `stateMu`, clear `connection.SetDeadline(time.Time{})` without `stateMu`, then reacquire it and detach only if the listener is open and still owns that exact connection. Close wins by detaching and closing; accept then returns the sentinel without registration or a second close.

- [ ] **Step 4: Verify GREEN under repetition and race detector**

```text
go test ./internal/vm -run 'TestListenerTimeout|TestAcceptedSocket|TestPendingNetwork|TestNetSelect|TestConcurrentNetwork' -count=20
go test -race ./internal/vm -run 'TestPendingNetwork|TestNetSelectRestores|TestConcurrentNetworkConfiguration' -count=1
```

Expected: PASS without races or stale restoration.

- [ ] **Step 5: Commit**

```text
git add internal/vm/builtins_net.go internal/vm/builtins_net_test.go
git commit -m "fix(net): preserve deadlines across select probes"
```

---

### Task 4: Public native and typed wrapper

**Files:**
- Modify: `internal/vm/builtins_net.go`
- Modify: `internal/vm/builtins_registry_test.go`
- Modify: `internal/stdlib/net.nx`
- Test: `internal/vm/builtins_net_test.go`

**Interfaces:**
- Produces: `net_settimeout(socket, timeout_ms) -> void` and `net.settimeout(sock: Socket, timeout_ms: int) -> void`.
- Changes: `net_setblocking(socket, true)` clears deadlines.
- Preserves: `net_setblocking(socket, false)` returns without inspecting the socket or mutating state.

- [ ] **Step 1: Write failing native/API tests**

Invoke the natives against the normative decision tree and assert error precedence. `net_settimeout` must check: exact arity; exact `VAL_INT` timeout; positivity/overflow; first value `VAL_OBJ`; non-nil `ObjMap` or `ObjInstance`; existing exact-integer, platform-representable `fd`; listener lookup before socket; then open state. Include typed nil values and cases failing multiple conditions, requiring the earlier error without panic. `net_setblocking` must return silent `void` unless it has exactly two arguments and argument two is exactly bool; false returns before inspecting malformed, nil, missing, unknown, closed, or out-of-range handles; only exact true performs shared extraction/lookup and clears an open resource or errors. Also require no false-branch state change from blocking and timed modes. Add `net_settimeout` to registry and contextual-handler expectations. Compile and run a Noxy source snippet importing `net`, calling `settimeout(listener, 25)`, restoring blocking, and closing it.

- [ ] **Step 2: Run tests and verify RED**

```text
go test ./internal/vm -run 'TestNetSetTimeout|TestNetSetBlocking|TestBuiltinRegistrySnapshot|TestStatefulBuiltinsUseContextualHandlers|TestNetTimeoutWrapper' -count=1
```

Expected: FAIL because the new API is absent and `net_setblocking` is unconditional.

- [ ] **Step 3: Register natives and wrapper**

Register `net_settimeout` contextually and implement the Task 4 test order exactly: arity, timeout type, safe timeout value, `VAL_OBJ`, non-nil supported object, exact/range-safe `fd`, listener-first registry lookup, then open state. Refactor `net_setblocking` in this order: return `void` unless there are exactly two arguments and argument two is exactly bool; return `void` immediately when false; only then run shared nil-safe extraction/lookup and configure zero duration for true.

```noxy
func settimeout(sock: Socket, timeout_ms: int) -> void
    net_settimeout(sock, timeout_ms)
end
```

- [ ] **Step 4: Verify GREEN and import/compiler compatibility**

```text
go test ./internal/vm -run 'TestNetSet|TestBuiltinRegistrySnapshot|TestStatefulBuiltinsUseContextualHandlers|TestNetTimeoutWrapper|TestNetworkBuiltinsLoopbackLifecycle' -count=1
go test ./internal/compiler ./internal/parser -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```text
git add internal/vm/builtins_net.go internal/vm/builtins_net_test.go internal/vm/builtins_registry_test.go internal/stdlib/net.nx
git commit -m "feat(net): add portable socket timeouts"
```

---

### Task 5: Documentation and full validation

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md`
- Modify: `CHANGELOG.md`
- Create: `.github/workflows/network-deadlines.yml`
- Include: `docs/superpowers/specs/2026-08-14-network-deadlines-design.md`
- Include: `docs/superpowers/plans/2026-08-14-network-deadlines.md`

- [ ] **Step 1: Document the exact contract**

Add “Network Deadlines” to the standard-library section. Include the precedence table, exact validation/error order, nil-safe `ObjMap`/`ObjInstance` forms, authoritative range-safe `fd` and listener-first lookup, positive/overflow limits specific to `net_settimeout`, per-operation refresh, persistent commit versus directional pending-I/O effects, shared-VM state, accept sentinel, accepted-socket non-inheritance, buffered and partial I/O, and normalized timeout error. Document publication before restoration, destructive buffered-accept removal on initialization failure, the exact probe `min` formula, absolute rollback snapshots, fail-closed rollback failure, skipped restoration after close, and the distinction between synchronous deadline-management errors and ordinary not-ready probe errors. Explicitly leave ordinary select `Read(n>0, err!=nil)` unchanged for `feat/network-poller`. Mark `setblocking(false)` as an unconditional deprecated no-op until that subproject; never describe it as non-blocking. Add concise Unreleased changelog entries.

- [ ] **Step 2: Add Windows and Unix execution coverage**

Create this workflow with the current Node 24-based major releases of the
official checkout and Go setup actions:

```yaml
name: Network deadlines

on:
  pull_request:
  push:
    branches: [develop, main]

permissions:
  contents: read

jobs:
  network-deadlines:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version-file: go.mod
          cache-dependency-path: go.sum
      - run: go test ./internal/vm -run 'Test(Socket|Listener|PendingNetwork|NetSelectRestores|ConcurrentNetwork|NetSet)' -count=1
      - run: go test ./internal/... -count=1
```

The real-loopback suite must assert that a TCP listener supports deadline configuration and that a wrapped `os.ErrDeadlineExceeded` normalizes through `errors.Is`. Controlled doubles remain platform-independent.

- [ ] **Step 3: Format and inspect the diff**

```text
gofmt -w internal/vm/resources.go internal/vm/builtins_net.go internal/vm/builtins_net_test.go internal/vm/builtins_registry_test.go
git diff --check
```

Expected: formatting succeeds and `git diff --check` prints nothing.

- [ ] **Step 4: Run required project validation**

```text
go test ./internal/...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: all internal Go tests and all non-excluded Noxy examples pass.

- [ ] **Step 5: Run final checks**

```text
go test ./...
go vet ./...
go build ./...
```

Expected: every command exits zero without newly introduced warnings.

- [ ] **Step 6: Commit**

```text
git add docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md .github/workflows/network-deadlines.yml docs/superpowers/specs/2026-08-14-network-deadlines-design.md docs/superpowers/plans/2026-08-14-network-deadlines.md
git commit -m "docs(net): specify deadline precedence"
```
