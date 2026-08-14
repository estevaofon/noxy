# Network Poller Design

## Purpose

Replace the current sequential, consuming `net_select` probes with a real
socket-readiness multiplexer. The implementation resolves roadmap points 5–8
from PR #17 while preserving the public `net.poll` result shape and the
deadline behavior delivered by PR #20.

Go's `net` package and runtime poller provide the behavioral reference:
network operations may be used concurrently, deadlines apply to pending and
future I/O, close wakes blocked operations, and readiness means that the next
operation can finish without waiting rather than that it must succeed.

## Goals

- Detect socket and listener readiness without calling `Read` or `Accept`.
- Make `timeout_ms == 0` a genuinely immediate poll.
- Apply one timeout to the complete multiplexing operation rather than once per
  candidate.
- Implement read, write, and error result sets.
- Provide explicit Windows and Unix backends behind one common contract.
- Preserve safe concurrent close, shared runtime ownership, and persistent
  network deadlines.
- Define EOF, hangup, pending-error, duplicate, and overlapping-event behavior.

## Non-goals

- Do not change the `net.poll` Noxy signature or `SelectResult` fields.
- Do not add UDP sockets, asynchronous connect, edge-triggered polling, or a
  persistent event-loop API.
- Do not make poll plus subsequent I/O atomic.
- Do not implement true non-blocking mode for `net.setblocking(sock, false)`;
  that deprecated branch remains a no-op.
- Do not expose platform event constants to Noxy.
- Do not treat an expired persistent I/O deadline as operating-system socket
  readiness.

## Public Contract

The API remains:

```noxy
func poll(read: Socket[64], write: Socket[64], err: Socket[64], timeout: int) -> SelectResult
```

The native validates exactly four arguments, requires three arrays and an
exact integer timeout, and rejects invalid timeout conversion before touching
network resources. A negative timeout is invalid. Zero performs an immediate
poll. A positive value is the maximum duration in milliseconds for the whole
call. Values whose millisecond conversion exceeds Go's `time.Duration` range
are invalid.

Each result array remains 64 elements long and is padded with `null`. Each
count is the number of non-null entries in its corresponding result array.
Invalid, malformed, unknown, or already-closed candidate values are ignored,
matching the existing candidate behavior. A valid resource may occur in more
than one input set and may be reported in more than one output set.

An empty union of valid candidates returns immediately for timeout zero. For a
positive timeout it waits until the global timeout expires, then returns empty
sets.

Polling never:

- reads or peeks at application bytes;
- accepts a connection;
- calls `getsockopt(SO_ERROR)` or otherwise clears a pending error;
- calls any `SetDeadline` method;
- changes the resource's persistent timeout mode.

Readiness is an advisory snapshot. Another Noxy routine or Go goroutine may
perform I/O or close the resource before the caller acts on the result.

## Event Semantics

The common layer normalizes platform events as follows:

| Native condition | Connected socket `read` | Connected socket `write` | `error` |
|---|---:|---:|---:|
| Normal data readable | yes | no | no |
| Normal data writable | no | yes | no |
| Hangup/EOF | yes | yes | yes |
| Pending socket error | yes | yes | yes |
| Invalid native descriptor | yes | yes | yes |
| Priority/OOB exceptional condition | no | no | yes |

For a listener, normal read readiness means an `Accept` can complete without
waiting. A listener is never returned as normally writable. Listener error,
hangup, or invalid-descriptor events are returned only in the error set, even
if the listener was also supplied in read or write.

Hangup and error wake requested read and write interests because the next
operation will complete immediately, even if it completes with EOF or an
error. They also appear in the public error set when that resource was supplied
there. The error set therefore represents terminal, error, invalid-descriptor,
or exceptional readiness; it is not the narrow Berkeley `exceptfds` contract.

EOF keeps the existing Noxy receive result:

```text
NetResult{ok: true, count: 0, data: "", error: ""}
```

Readable data and hangup may coexist. In that case the socket can appear in
both read and error, and `net_recv` returns available bytes before a later
receive observes EOF. The poller does not consume either the bytes or the
pending terminal/error condition.

## Candidate and Result Ordering

The common layer resolves candidates using the current authoritative `fd`
field rules and listener-first registry lookup. It creates one native
registration per unique resource and combines all requested interests.

For result construction, each input set is scanned in its original order.
Every ready occurrence is copied to the corresponding output, including
duplicate occurrences. Consequently:

- native registration is deduplicated for efficiency;
- public order and multiplicity remain compatible with the existing loop;
- one socket may occur in read, write, and error simultaneously;
- no result count can exceed 64.

## Common Poller Boundary

`internal/vm/network_poller.go` owns the platform-independent model:

```go
type networkInterest uint8

const (
	networkReadable networkInterest = 1 << iota
	networkWritable
	networkExceptional
)

type networkEvent uint8

const (
	networkReadReady networkEvent = 1 << iota
	networkWriteReady
	networkErrorReady
)
```

An internal registration contains the Noxy handle, exact resource pointer,
exact listener or connection pointer, raw connection, requested interests, and
resource kind. Platform code receives only raw socket descriptors and interest
masks and returns normalized native flags. It does not access VM registries or
Noxy values.

The builtin flow is:

1. Validate arguments and timeout.
2. Resolve input occurrences and build unique registrations.
3. Create one operation-local wake-up object.
4. Register that wake-up with every unique resource under its `stateMu`.
5. Acquire stable raw-descriptor references and invoke one platform wait.
6. Release raw references and unregister the wake-up.
7. Revalidate every reported registration.
8. Reconstruct read, write, and error outputs in input order.

Backend failure is synchronous and returns no partial `SelectResult`.

## Descriptor Lifetime

`syscall.RawConn.Control` guarantees that its descriptor remains valid only
during its callback. The implementation therefore performs the native wait
inside nested `Control` callbacks: each callback retains one descriptor while
the next is acquired, and the innermost callback invokes the backend with all
descriptors stable. The operation-local wake socket or pipe remains owned and
open until the wait and cleanup finish.

Copying descriptor numbers outside `Control` and relying only on post-wait
revalidation is forbidden because the operating system could reuse a closed
descriptor number for another socket before polling starts.

The resources do not need a separate lifetime generation. Their `closed`
transition is monotonic, the stored connection/listener is never replaced, and
Noxy registry handles are allocated monotonically. Revalidation requires all
of the following:

- the registry handle still resolves to the exact resource pointer;
- `closed` is false;
- the exact connection or listener pointer is still attached;
- the reported registration belongs to the current poll operation.

## Concurrent Close and Wake-up

Each poll call owns a distinct wake-up object and registers it in a waiter set
on every observed resource. Signaling is idempotent and non-blocking. This
provides broadcast behavior when multiple poll calls observe the same socket.

Every path that permanently closes or poisons a listener or socket uses one
central detach transition:

1. Lock `stateMu`.
2. If already closed, return without another close.
3. Mark `closed = true` and detach the OS connection/listener and obsolete
   probe buffers.
4. Snapshot and clear the poll waiter set.
5. Unlock `stateMu`.
6. Signal every waiter.
7. Close detached OS resources outside the lock.

Signaling precedes the OS close because close may wait for outstanding
`RawConn.Control` references. The wake event causes the native wait to return,
the callbacks to release those references, and close to finish. A local close
that wakes a poll is omitted from all result sets after revalidation; the poll
returns promptly rather than waiting out the remaining timeout.

Deadline configuration remains independent. It neither invalidates poll
registrations nor wakes a poll. Existing `deadlineGeneration` continues to
belong only to deadline transitions.

## Global Timeout

The common layer records one monotonic start instant and, for positive values,
one absolute end instant. Every backend retry or timeout chunk is computed from
the remaining duration. No path restarts the original timeout.

Windows accepts a signed 32-bit millisecond timeout, so longer durations are
split into bounded chunks. The same common chunking rule is used on Unix for
consistent behavior. Unix `EINTR` and the equivalent Windows interruption are
handled as follows:

- for a positive timeout, retry with the remaining duration;
- for timeout zero, return an empty result immediately;
- once the absolute end instant is reached, return an empty result.

An unrelated spurious wake or platform wake event may cause the common layer
to revalidate and retry with the remaining duration. A wake caused by local
close returns promptly after omitting the closed resource.

## Platform Backends

### Unix

`internal/vm/network_poller_unix.go` uses `golang.org/x/sys/unix.Poll` and an
operation-local non-blocking pipe or socketpair for wake-up. Explicit build
tags cover only Unix targets where `unix.Poll` and the chosen wake primitive
are available. The backend maps `POLLIN`, `POLLOUT`, `POLLPRI`, `POLLHUP`,
`POLLERR`, and `POLLNVAL` into common flags.

The initial target list is AIX, Darwin, DragonFly BSD, FreeBSD, Linux, NetBSD,
OpenBSD, and Solaris. Any target that lacks a required primitive uses the
unsupported backend until it has a tested implementation.

### Windows

`internal/vm/network_poller_windows.go` defines the ABI-compatible
`WSAPOLLFD` structure and calls `WSAPoll` from `Ws2_32.dll`. The maximum union
is 192 unique candidate occurrences before deduplication, well within the
`ULONG` count accepted by `WSAPoll`.

The wake-up is an operation-local UDP loopback reader/writer pair because
`WSAPoll` can wait only on Winsock sockets. Signaling writes at most one
datagram per operation. The backend maps `POLLRDNORM`, `POLLWRNORM`,
`POLLRDBAND`, `POLLHUP`, `POLLERR`, and `POLLNVAL` into common flags.

The historical `WSAPoll` asynchronous-connect caveat does not affect this
feature: Noxy's current `net_connect` is synchronous and only successfully
connected sockets are registered.

### Unsupported targets

`internal/vm/network_poller_unsupported.go` compiles everywhere not covered by
the Windows or supported-Unix tags and returns a stable "network polling is not
supported on this platform" error. This is preferable to a sequential or
consuming fallback with different semantics.

## Interaction with Existing Deadlines

The new poller never takes `readMu`, `writeMu`, `acceptMu`, or `deadlineMu` and
never modifies a deadline. `net.settimeout` continues to control only
`net_recv`, `net_send`, and `net_accept`. `net.setblocking(sock, true)` still
clears persistent I/O deadlines. The deprecated false branch remains a no-op.

The old probe-specific state becomes obsolete and is removed:

- `SocketResource.bufferedRead`;
- `ListenerResource.bufferedAccept`;
- `SocketResource.readProbeDeadline`;
- `ListenerResource.acceptProbeDeadline`;
- `beginSocketProbe`, `finishSocketProbe`, and `selectSocket`;
- `beginListenerProbe`, `finishListenerProbe`, and `selectListener`.

Receive and accept no longer consult poller buffers. Persistent deadline
application and rollback retain their existing fail-closed behavior, but every
fail-closed path must use the centralized detach-and-wake transition.

## Error Handling

- Invalid public argument shape or timeout produces a synchronous native error.
- Invalid candidate elements are ignored.
- Failure to obtain `SyscallConn` for a registered production TCP resource is
  a synchronous backend error.
- Platform poll failure is synchronous and yields no partial result.
- A resource closed concurrently is omitted, not reported as a platform error.
- Native error and hangup flags remain observable; the poller does not inspect
  or clear their underlying socket error.
- Wake-up saturation is prevented by idempotent one-signal state.

## Testing Strategy

Development follows red-green-refactor. Tests are divided by responsibility.

### Common unit tests

A fake backend verifies:

- timeout zero is passed as zero and returns immediately;
- one positive global timeout is not multiplied by candidate count;
- interrupted and chunked waits use only the remaining duration;
- negative, wrong-type, and overflowing timeouts fail before polling;
- read, write, error, hangup, invalid, and exceptional masks normalize exactly;
- listener write readiness is suppressed;
- order and duplicate occurrences are preserved;
- one resource can occur in all three outputs;
- malformed, unknown, and closed candidates are ignored;
- backend error returns no partial result;
- empty-set timing behavior.

### Lifetime and concurrency tests

Controlled TCP resources and barriers verify:

- descriptors remain inside `RawConn.Control` during the backend wait;
- close signals every poll watching the resource;
- close does not deadlock behind descriptor references;
- a closed resource is omitted after wake-up;
- unrelated deadline configuration does not invalidate or wake poll;
- concurrent polls and close are race-free;
- poisoning through deadline rollback failure also wakes pollers.

### Real loopback integration

Platform tests use real TCP listeners and connections to verify:

- listener accept readiness without accepting;
- readable data remains fully available after repeated polls;
- timeout zero with no data is immediate;
- write readiness is populated;
- graceful half-close/EOF behavior;
- data plus hangup coexistence;
- reset/pending-error reporting where the platform makes it deterministic;
- global timeout with multiple non-ready sockets;
- concurrent close unblocks a positive-timeout poll.

Tests that require native descriptors do not use `net.Pipe`. Existing deadline
tests based on consuming probes are removed or rewritten around the preserved
persistent-deadline contract.

### Platform verification

- Run unit, integration, race, vet, and Noxy example suites on Windows.
- Run unit, integration, and race suites on Linux CI.
- Cross-build the affected packages for every supported Unix build tag.
- Keep platform normalization tests independent of host-only event timing.

## Documentation

`docs/NOXY_LANGUAGE_SPEC.md` will replace the consuming-probe deadline section
with the public contract above. It will explicitly document timeout modes,
global timing, all three sets, duplicate/order behavior, EOF, hangup, pending
errors, data coexistence, local close, readiness races, and deadline
independence.

`CHANGELOG.md` will record the non-consuming poller, true zero-time polling,
global timeout, write/error sets, and platform-specific backends. An executable
Noxy example will exercise immediate timeout, listener readiness, write
readiness, data preservation, and EOF.

## Compatibility

Valid existing programs keep the same signature and result structure. Read
polling no longer consumes and buffers a byte or accepts and buffers a
connection. Timeout zero becomes immediate, positive timeout becomes global,
and write/error fields become meaningful. Programs that accidentally depended
on one millisecond minimum delay or sequential per-candidate waiting receive
the intended corrected behavior.

The implementation changes no language syntax, bytecode, compiler types,
handle format, persistent timeout API, or detached/supervised task behavior.
