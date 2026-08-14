# Network Deadlines Design

## Goal and Scope

Resolve point 4 from PR #17 through a portable, additive deadline API for TCP
listeners and connected sockets. This subproject supports indefinite blocking
and positive read/write/accept timeouts. True non-blocking I/O remains part of
`feat/network-poller`, where Windows and Unix readiness implementations will
be introduced together with corrected `net_select` semantics.

This separation is required because Go's portable `net.Conn` API implements
deadlines but does not expose a portable non-blocking operation. An expired
deadline is not a non-blocking probe: it fails even when I/O is already ready.

## Public API

The additive public function follows the module's existing naming style:

```noxy
net.settimeout(sock, timeout_ms)
```

Its native entry point is `net_settimeout(Socket, int) -> void`. It configures
one positive timeout for reads, writes, or listener `accept`. A timeout must be
greater than zero and small enough to convert safely to `time.Duration`.

Validation is normative:

- `net_settimeout` validates in this exact order: exactly two arguments;
  argument two is exactly `VAL_INT`; timeout is positive and within the safe
  duration bound; argument one has `Type == VAL_OBJ`; its object is a non-nil
  `*ObjMap` or non-nil `*ObjInstance`; its `fd` field exists, is exactly
  `VAL_INT`, and round-trips through platform `int` without a value change;
  the descriptor resolves to a listener first or connected socket second; and
  the resource is open under `stateMu`. Every violation is a synchronous
  runtime error and later checks do not run after an earlier failure;
- `net_setblocking` returns silently unless it has exactly two arguments and
  its second argument is exactly `bool`;
- `net_setblocking(..., false)` returns before inspecting its first argument;
- only `net_setblocking(..., true)` performs the same socket extraction,
  descriptor, lookup, and open-state checks before clearing the timeout, with
  failures producing a synchronous runtime error.

The accepted socket representations are the existing non-nil `ObjMap` returned
by network natives and non-nil `ObjInstance` used by typed Noxy values. Typed
nil pointers are rejected before method or field access. In both forms, `fd`
is the only authoritative field. User-mutable `open`, `addr`, and `port` fields
are ignored. A descriptor outside the platform `int` range is rejected before
lookup, so narrowing can never wrap to a valid handle. Lookup remains listener
then socket, matching current `net_select`; odd/even handle allocation makes
ordinary descriptors unambiguous.

The existing function remains callable:

```noxy
net.setblocking(sock, blocking)
```

`setblocking(sock, true)` clears an active timeout and restores indefinite
blocking. `setblocking(sock, false)` remains the exact compatibility no-op and
is deprecated: it does not inspect or validate the socket, and it neither
clears nor replaces an active timeout. This behavior is explicit so the
runtime does not pretend that an expired deadline is true non-blocking I/O.
The later network-poller subproject will give the `false` branch real
readiness semantics without another API change.

Separate read/write timeout setters are excluded: the existing network module
is intentionally small, and current HTTP consumers use one timeout value.

## Modes and Precedence

Every listener and connected socket starts in indefinite blocking mode. The
persistent state has two supported modes:

| Successful call | Resulting mode |
|---|---|
| `settimeout(sock, n)`, `n > 0` | timed; each I/O operation gets a fresh `n` millisecond deadline |
| `setblocking(sock, true)` | blocking; read, write, and accept deadlines are cleared |
| `setblocking(sock, false)` | unchanged; deprecated compatibility behavior |

Between supported configuration calls, the most recently linearized call
wins. A rejected configuration never changes the prior persistent mode.

`net_select` keeps its current call-local probe timeout in this subproject. Its
absolute read or accept deadline is an upper bound while the probe is active:
no configuration may remove or extend it. A concurrent positive timeout may
shorten the live probe deadline, while `setblocking(true)` or a longer timeout
changes persistent state immediately but leaves the probe bound intact. For a
socket, write configuration is applied independently even while a read probe
is active. When the probe finishes, it removes the active-probe marker and
restores the latest persistent read or accept mode, not the mode captured at
probe start. `net_select` never mutates persistent configuration.

## Runtime Model and Linearization

An internal `deadlineListener` interface extends `net.Listener` with
`SetDeadline(time.Time) error`. `net_listen` validates this capability before
registering a TCP listener; an implementation without it is closed and
reported through the existing `Socket{open:false}` listen result.

`ListenerResource` stores the `deadlineListener`, `ioTimeout`, the active
accept-probe bound, the last successfully installed absolute accept deadline,
a deadline-transition mutex, and a deadline generation. `SocketResource`
stores the equivalent transition mutex and generation plus `ioTimeout`, the
active read-probe bound, and the last successfully installed absolute read and
write deadlines. Zero `ioTimeout` means indefinite blocking; only validated
positive values mean timed mode. The absolute fields are exact rollback
snapshots, not values reconstructed from the relative timeout.

Public timeout milliseconds remain `int64` until validation against:

```go
timeoutMilliseconds > 0 &&
timeoutMilliseconds <= int64(math.MaxInt64)/int64(time.Millisecond)
```

No conversion through platform-sized `int` is permitted on the new
`net_settimeout` path. The existing narrowing and timeout coercion inside
`net_select` belong to `feat/network-poller` and are deliberately unchanged.

Deadline transitions are serialized by a dedicated `deadlineMu`, never by
`stateMu`. No call to `SetReadDeadline`, `SetWriteDeadline`, `SetDeadline`, or
`Close` occurs while `stateMu` is held. This is required because the interface
contracts permit concurrent calls but do not promise that a setter returns
promptly. Consequently, `net_close` never acquires `deadlineMu`: under
`stateMu` it marks the resource closed, increments the deadline generation,
detaches owned buffers and OS resources, then invokes `Close` after unlocking.
It can therefore close the resource even when a deadline setter is stalled.

The lock order is directional mutex (`readMu`, `writeMu`, or `acceptMu`), when
applicable, then `deadlineMu`, then the short-lived `stateMu`. Configuration
starts at `deadlineMu`; close uses only `stateMu`. Code never acquires
`deadlineMu` while holding `stateMu`.

Every deadline transition follows a reserve/apply/revalidate protocol:

1. acquire `deadlineMu` and, under `stateMu`, validate the live resource,
   snapshot the exact state needed for rollback, reserve the next generation,
   and capture the underlying connection or listener;
2. release `stateMu` and invoke the required setters while retaining only
   `deadlineMu`;
3. reacquire `stateMu` after each setter and before commit. Commit only if the
   resource remains open and the reserved generation is still current;
4. if close changed the generation, perform no persistent commit, rollback,
   registration, or second close: close owns the detached resource lifecycle.

Before each `Accept`, `Read`, or `Write`, the operation acquires its existing
direction mutex and uses that protocol to install a fresh deadline derived
from the current persistent mode. The direction mutex remains held across its
I/O, but `deadlineMu`, `stateMu`, and registry locks do not. Every successful,
still-current setter records its exact absolute value in the corresponding
last-installed field.

Configuration uses the same protocol and applies read and write deadlines
separately. It stores the new persistent mode only after all required calls
succeed and the generation revalidation confirms that close did not win. Go
deadlines affect pending and future I/O, so changing the mode can shorten,
extend, or clear an operation already waiting, except that it cannot remove or
extend an active select bound.

The persistent-mode linearization point is the `ioTimeout` store after all
required setters succeed and the reserved generation is revalidated.
Operations beginning after that store observe the new mode. Effects on
already-pending read and write operations linearize at their individual setter
calls and are not atomic across directions. A pending read may therefore
observe a new deadline before a pending write; this is an explicit property of
the underlying Go API, not a shared-state race.

`SetReadDeadline`, `SetWriteDeadline`, and listener `SetDeadline` are not
assumed to be transactional. Before configuration, the runtime snapshots the
last successfully installed absolute deadlines. If an application step fails,
configuration keeps the previous persistent mode and makes a best-effort
rollback to those exact snapshots, including any active probe bound. It never
reconstructs rollback as `time.Now().Add(oldTimeout)`, which could extend a
pending operation. The configuration returns a synchronous error; if rollback
also fails, both failures are preserved with `errors.Join`.

A setter that returns an error may already have changed OS state, so the live
deadline for that direction is considered unknown until a rollback, later
operation, successful configuration, or probe cleanup installs a deadline
successfully. Only persistent state is guaranteed unchanged. Each successful
reapplication refreshes the last-installed absolute field and repairs the
unknown live state.

If any rollback step fails, later repair cannot be guaranteed because a
pending operation may still hold its direction mutex indefinitely. If the
transition still owns its generation, the runtime therefore fails closed:
while holding `stateMu` it marks the resource closed, increments the
generation, detaches and clears buffered state, and captures the underlying
connection, listener, and any buffered accepted connection. After releasing
`stateMu` and `deadlineMu`, it closes the captured resources to unblock pending
I/O. If ordinary close already invalidated the generation, that close owns the
lifecycle and the transition does not poison or close anything again. The
synchronous error joins the original application failure, every rollback
failure, and any close failure with `errors.Join`. A poisoned resource remains
invalid in its shared registry until an ordinary `net_close` removes the
handle; subsequent operations observe `closed` and cannot start new I/O.

At probe start time `t`, the runtime computes the immutable upper bound and
initial effective deadline as:

```text
probeBound = t + selectTimeout
effective = min(probeBound, t + ioTimeout), when ioTimeout > 0
effective = probeBound, otherwise
```

The marker always stores `probeBound`, even when the initial effective
deadline is earlier. Configuration during the probe recomputes its directional
effective value against that original bound. Probe cleanup removes the marker
and installs zero or `time.Now().Add(latestTimeout)`.

If initial installation fails, the probe clears its marker, performs no
`Read` or `Accept`, and returns a synchronous error from `net_select`. If the
resource closes during the probe, cleanup skips restoration and reports the
candidate as not ready. If restoration fails while the resource remains open,
`net_select` returns a synchronous error rather than a partial `SelectResult`;
the next operation attempts repair before I/O.

After a probe returns ordinary success (`Read` with `n > 0 && err == nil`, or
`Accept` with a non-nil connection and `err == nil`), it locks `stateMu` and
checks close state. If still open, it publishes the consumed byte or connection into
`bufferedRead` or `bufferedAccept` before attempting deadline restoration. A
restoration failure therefore returns a synchronous error while retaining the
same candidate's readiness buffer for a later call. If close won, an accepted
connection is closed and the candidate is not ready; bytes need not be
retained after their owning socket has closed.

Only deadline installation or restoration failures introduced by this feature
are synchronous `net_select` errors. On such a management error,
`net_select` stops processing later candidates and returns no `SelectResult`.
Read bytes or accepted connections buffered by earlier successful probes
remain buffered. Ordinary `Read` or `Accept` errors retain current behavior:
that candidate is not ready and later candidates are still processed. This
subproject does not otherwise change consumer-buffered or multiplexing
semantics.

The preservation guarantee is limited to deadline installation and
restoration failures introduced by this feature. The current behavior for an
ordinary select `Read` returning `n > 0` together with a non-nil error remains
unchanged, even though a general `io.Reader` consumer may process those bytes.
Correcting that polling behavior belongs to `feat/network-poller`.

## Buffered Read and Accept Ordering

Buffered readiness is examined before installing an operation deadline.

- A `bufferedAccept` fully satisfies `net_accept`, so no listener deadline is
  installed. `acceptMu` remains held for the entire delivery. Under `stateMu`,
  `net_accept` observes the connection but leaves it published, then clears the
  connection's own deadline without holding `stateMu`. It reacquires `stateMu`
  and detaches only if the listener is still open and
  `bufferedAccept == connection`. On success, the detached connection is owned
  locally and registered. On clearing failure, the still-owned connection is
  destructively detached and closed outside the lock. If listener close won,
  it already detached and owns closing the connection; `net_accept` neither
  registers nor closes it again and returns the existing invalid-socket
  sentinel. A closed connection is never left inside the buffer.
- A `bufferedRead` that fully satisfies `net_recv` is returned without a socket
  read or deadline installation.
- When additional reading is required, the deadline is installed before the
  shared buffer is detached or mutated. If installation fails and buffered
  bytes are already deliverable, those bytes are consumed and returned as a
  successful partial receive under the existing partial-read contract. With
  no buffered bytes, the deadline failure is returned as `NetResult{ok:false}`.
- A later `Read` error after buffered bytes were detached also returns those
  bytes as a successful partial receive.

These rules prevent deadline-management failures from silently discarding a
byte or accepted connection previously consumed by `net_select`.

Configuration belongs to shared resources, so parent and child VMs observe the
same mode. Registry locks are never held during network I/O.

## Accept and Socket Inheritance

A listener timeout bounds only `net_accept`. On expiry, `net_accept` preserves
its existing return shape and yields `Socket(-1, "", 0, false)`; it cannot
return a `NetResult` error string without a breaking API change.

Every connection returned by `Accept` or `DialTimeout` has
`SetDeadline(time.Time{})` applied before registration, then starts with zero
absolute snapshots and indefinite blocking mode. An accepted connection does
not inherit the listener timeout. If clearing the deadline fails, the
connection is closed and the existing invalid-socket result is returned; a
close failure cannot be represented by the current `Socket` shape and is not
exposed. Callers configure a successfully returned socket explicitly when
required.

## Results and Errors

The existing `Socket` and `NetResult` shapes remain unchanged.

- A read or write deadline expiry is recognized with
  `errors.Is(err, os.ErrDeadlineExceeded)` and reported as
  `error="operation timed out"`; platform-specific error text is never used
  for classification.
- EOF remains `ok=true`, `count=0`, `error=""`.
- A receive that transfers bytes before an error remains
  `ok=true`, returns those bytes and their count, and leaves `error` empty.
- A send that transfers bytes before an error returns `ok=false`, the actual
  transferred count, and the normalized or underlying error. This lets the
  caller resume at the correct offset without changing the result shape.
- Other I/O failures retain their underlying error string.
- Deadline-application failures during `recv` or `send` use `NetResult` with
  `ok=false`; during `accept` they use the existing closed-socket sentinel.

`net_settimeout` and the effective `net_setblocking(true)` configuration path
produce synchronous runtime errors for malformed sockets, unknown or closed
handles, deadline-application or rollback failure, non-positive timeouts, and
overflow. The deprecated `net_setblocking(false)` preserves the prior native
behavior exactly: it returns `void` without validating or changing anything.

## Compatibility

- Existing sockets remain blocking by default.
- Existing `setblocking` calls retain their name, arguments, and `void` return
  type. The valid `true` branch now restores blocking; malformed calls retain
  the old silent return behavior because no effective branch can be selected.
- `setblocking(false)`, including calls carrying stale or invalid socket
  handles, remains an unconditional no-op and is now documented as deprecated.
- `settimeout` is additive and uses the same `Socket` type.
- `net_recv`, `net_send`, `net_accept`, and `net_select` retain their result
  shapes.
- `net_select` remains sequential and consumer-buffered until
  `feat/network-poller`.

## Testing

Tests use real loopback TCP connections plus controlled `net.Conn` and
`net.Listener` implementations where deterministic pending operations or
deadline failures are required. Every goroutine has a bounded harness wait.
The loopback and public deadline tests run in CI on both Windows and Unix;
controlled contract tests run on every supported job.

The test matrix proves:

- default reads wait for later data;
- positive read timeout waits for a bounded interval and returns the stable
  timeout result;
- positive write timeout bounds a blocked write and reports its actual partial
  count;
- listener timeout bounds `accept` and returns the existing invalid-socket
  sentinel;
- accepted sockets start blocking rather than inheriting the listener mode;
- `setblocking(true)` clears a timeout for subsequent and pending I/O;
- `setblocking(false)` performs no validation and leaves both blocking and
  timed modes unchanged, including for stale handles;
- the latest supported configuration call wins across related VMs;
- configuration during pending read, write, and accept is observed;
- persistent mode commits only after all directional setters succeed, while
  already-pending directional effects may occur one setter at a time;
- two concurrent configurations commit in `deadlineMu` order without exposing
  a partially committed persistent mode;
- an active `net_select` deadline cannot be removed or extended by concurrent
  configuration, while a shorter timeout may shorten it;
- a probe starting with a shorter persistent timeout uses that timeout, while
  a longer persistent timeout remains capped by the select bound;
- write configuration proceeds independently during an active read probe;
- `net_select` restores the latest blocking or timed mode after readiness or
  timeout, skips restoration after close, and reports installation or live
  restoration failures synchronously;
- `1`, the maximum safe timeout, `0`, negative values, and overflow are handled
  without narrowing through `int`;
- malformed, unknown, and closed handles fail as specified;
- deadline errors are normalized with `errors.Is`, independent of operating
  system text;
- configuration doubles simulate partial OS mutation, primary failure,
  exact successful absolute-deadline rollback and recovery on the next
  operation without claiming live atomicity;
- rollback failure poisons and closes listeners or sockets, unblocks pending
  read/write/accept/probe operations, clears buffers, and cannot deadlock;
- concurrent close with read, write, accept, and configuration terminates
  without deadlock;
- a deadline setter stalled behind a controlled barrier cannot prevent
  `net_close` from marking, detaching, and closing its resource exactly once;
- a later deadline-management failure yields no `SelectResult`, stops later
  probes, and preserves data or connections buffered by earlier successful
  candidates, while ordinary I/O errors remain not-ready candidates;
- a restoration failure after successful readiness preserves the same
  candidate's byte or accepted connection before returning the management
  error;
- fully buffered operations skip deadline installation, and partial buffered
  reads preserve bytes when installation or subsequent I/O fails;
- concurrent buffered-accept deadline clearing and listener close has one
  lifecycle owner on both setter success and failure: no closed connection is
  registered and no connection is closed twice;
- connected and accepted sockets explicitly clear any inherited deadline
  before registration, including controlled connections that start timed;
- typed nil socket objects, invalid `fd` types/ranges, and listener-first
  lookup are handled without panic;
- ordinary select `Read(n>0, err!=nil)` behavior remains characterized but
  unchanged for the later poller subproject;
- deadline-application failures and partial I/O preserve their contracts;
- wrapped deadline errors normalize correctly through `errors.Is`;
- real TCP listeners implement the required deadline interface on Windows and
  Unix CI jobs;
- existing lifecycle, buffering, and concurrent-select tests remain green.

The project validation is:

```text
go test ./internal/...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```
