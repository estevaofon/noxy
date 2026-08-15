# Portable Network Poller Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Noxy's sequential consuming `net_select` probes with a portable, non-consuming, globally timed read/write/error multiplexer on Windows and supported Unix systems.

**Architecture:** A common poller resolves and deduplicates Noxy resources, owns timeout/revalidation/result semantics, and retains every descriptor inside nested `syscall.RawConn.Control` callbacks. Build-tagged Windows and Unix backends provide the native wait and an operation-local wake handle; resource close uses a two-phase detach that signals every registered poll before closing the OS handle.

**Tech Stack:** Go 1.24 language level, Go toolchain 1.24.11+, `net`, `syscall.RawConn`, `golang.org/x/sys/unix`, `golang.org/x/sys/windows`, Winsock `WSAPoll`, Go tests/race detector, Noxy integration examples.

## Global Constraints

- Preserve `net.poll(read: Socket[64], write: Socket[64], err: Socket[64], timeout: int) -> SelectResult` and its six result fields.
- `timeout < 0` is an error, `timeout == 0` is immediate, and positive timeout bounds the complete call.
- Polling must never read, peek, accept, call `SO_ERROR`, or change a persistent deadline.
- Consider only the first 64 elements of each input and make every count equal the number of copied output values.
- Preserve order and duplicate occurrences independently in read, write, and error outputs.
- Keep `net.setblocking(sock, false)` as a deprecated no-op.
- Keep `deadlineGeneration` exclusive to deadline transitions; do not add `lifetimeGeneration`.
- Preserve the lock order `deadlineMu -> stateMu`; signal poll waiters and close OS resources outside both locks.
- Attribute every `SyscallConn`/`RawConn.Control` failure to the exact registration and acquisition stage before deciding whether concurrent local close suppresses it.
- Keep listener write-only registrations subscribed to local-close wake-up without placing their descriptors in the native poll set.
- Cap each native blocking chunk at one second so a failed wake signal cannot hold `RawConn.Control` references—and therefore resource close—for an unbounded interval.
- Treat Windows and Linux as runtime-verified. Treat AIX, Darwin, DragonFly BSD, FreeBSD, NetBSD, OpenBSD, and Solaris as compile-supported until their integration tests run on those systems.
- Use strict red-green-refactor for each behavior and leave unrelated worktree changes untouched.

---

## Planned File Structure

### New files

- `internal/vm/network_poller.go` — public argument contract, common event model, wake lifecycle, candidate aggregation, nested descriptor retention, timeout loop, revalidation, and result construction.
- `internal/vm/network_poller_test.go` — platform-independent contract, ordering, timeout, descriptor-lifetime, close-classification, and fake-backend tests.
- `internal/vm/network_poller_windows.go` — Winsock wake sockets, `WSAPoll` ABI, and Windows event normalization.
- `internal/vm/network_poller_windows_test.go` — Windows ABI, wake, immediate, listener, and error-path tests.
- `internal/vm/network_poller_linux_test.go` — Linux `POLLRDHUP`, reset/pending-error, and half-close integration tests.
- `internal/vm/network_poller_unix.go` — Unix socketpair wake and `unix.Poll` backend.
- `internal/vm/network_poller_unix_test.go` — Unix wake, immediate, descriptor, and event tests.
- `internal/vm/network_poller_rdhup.go` — Linux/FreeBSD `POLLRDHUP` terminal mask.
- `internal/vm/network_poller_no_rdhup.go` — zero terminal-extension mask for other compile-supported Unix targets.
- `internal/vm/network_poller_unsupported.go` — stable unsupported-platform backend.
- `internal/vm/network_poller_integration_test.go` — real TCP non-consumption, read/write/EOF, global timeout, deadline independence, and concurrent-close tests.
- `noxy_examples/network_poller.nx` — public Noxy poll lifecycle demonstration.

### Modified files

- `internal/vm/resources.go` — resource waiter sets and two-phase detach payloads.
- `internal/vm/builtins_net.go` — centralized detach, simplified receive/accept/deadlines, removal of consuming probes, and new builtin dispatch.
- `internal/vm/builtins_net_test.go` — replace shared-buffer assertions with non-consuming readiness assertions.
- `internal/vm/builtins_net_deadlines_test.go` — remove obsolete probe/deadline-restoration cases and retain persistent-I/O guarantees.
- `internal/vm/architecture_test.go` — forbid reintroduction of consuming poll buffers/probe helpers.
- `go.mod`, `go.sum` — promote `golang.org/x/sys` to a direct dependency.
- `.github/workflows/network-deadlines.yml` — Windows/Linux runtime matrix and Unix cross-build matrix.
- `docs/NOXY_LANGUAGE_SPEC.md`, `CHANGELOG.md` — final public semantics and compatibility notes.

---

### Task 1: Establish the pure public contract and fixed result shape

**Files:**
- Create: `internal/vm/network_poller.go`
- Create: `internal/vm/network_poller_test.go`
- Modify: `internal/vm/builtins_net.go:362-381,947-994`

**Interfaces:**
- Produces: `networkInterest`, `networkEvent`, `validateNetworkPollArguments`, `boundedNetworkCandidates`, and `selectResult(read, write, errors []value.Value)`.
- Preserves: the existing `SelectResult` map fields and 64-element arrays.

- [ ] **Step 1: Write failing validation and result tests**

Add table-driven tests with literal expectations:

```go
func TestValidateNetworkPollArguments(t *testing.T) {
	empty := value.NewArray(nil)
	maximum := int64(math.MaxInt64) / int64(time.Millisecond)
	tests := []struct {
		name string
		args []value.Value
		want time.Duration
		wantError string
	}{
		{"zero", []value.Value{empty, empty, empty, value.NewInt(0)}, 0, ""},
		{"positive", []value.Value{empty, empty, empty, value.NewInt(25)}, 25 * time.Millisecond, ""},
		{"negative", []value.Value{empty, empty, empty, value.NewInt(-1)}, 0, "network poll timeout must be non-negative"},
		{"overflow", []value.Value{empty, empty, empty, value.NewInt(maximum + 1)}, 0, "network poll timeout is too large"},
		{"wrong arity", []value.Value{empty}, 0, "net_select expects exactly 4 arguments"},
		{"wrong set", []value.Value{value.NewNull(), empty, empty, value.NewInt(0)}, 0, "net_select read, write, and error arguments must be arrays"},
		{"wrong timeout", []value.Value{empty, empty, empty, value.NewString("0")}, 0, "network poll timeout must be an int"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got, err := validateNetworkPollArguments(test.args)
			if test.wantError != "" {
				if err == nil || err.Error() != test.wantError { t.Fatalf("error=%v want=%q", err, test.wantError) }
				return
			}
			if err != nil || got != test.want { t.Fatalf("timeout=%v error=%v want=%v", got, err, test.want) }
		})
	}
}

func TestSelectResultPadsTruncatesAndCountsCopiedValues(t *testing.T) {
	values := make([]value.Value, 65)
	for i := range values { values[i] = socketValue(i+1, "test", 0, true) }
	result := selectResult(values, values[:2], values[:1])
	mapping := requireBuiltinMap(t, result)
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "read_count"), value.NewInt(64))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "write_count"), value.NewInt(2))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "error_count"), value.NewInt(1))
	read := requireTestMapValue(t, mapping, "read").Obj.(*value.ObjArray)
	if len(read.Elements) != 64 { t.Fatalf("read length=%d", len(read.Elements)) }
}
```

- [ ] **Step 2: Run RED and confirm the missing contract**

Run:

```powershell
go test ./internal/vm -run 'TestValidateNetworkPollArguments|TestSelectResultPadsTruncates' -count=1
```

Expected: build failure because the new helper/signature does not exist.

- [ ] **Step 3: Add the common event types and exact argument validation**

Create the beginning of `network_poller.go`:

```go
package vm

import (
	"fmt"
	"math"
	"time"

	"noxy-vm/internal/value"
)

const networkSetCapacity = 64

type networkInterest uint8
const (
	networkReadable networkInterest = 1 << iota
	networkWritable
	networkErrorInterest
)

type networkEvent uint8
const (
	networkReadReady networkEvent = 1 << iota
	networkWriteReady
	networkErrorReady
)

func validateNetworkPollArguments(args []value.Value) ([3]*value.ObjArray, time.Duration, error) {
	var sets [3]*value.ObjArray
	if len(args) != 4 {
		return sets, 0, fmt.Errorf("net_select expects exactly 4 arguments")
	}
	for index := 0; index < 3; index++ {
		array, ok := args[index].Obj.(*value.ObjArray)
		if args[index].Type != value.VAL_OBJ || !ok || array == nil {
			return sets, 0, fmt.Errorf("net_select read, write, and error arguments must be arrays")
		}
		sets[index] = array
	}
	if args[3].Type != value.VAL_INT {
		return sets, 0, fmt.Errorf("network poll timeout must be an int")
	}
	milliseconds := args[3].AsInt
	if milliseconds < 0 {
		return sets, 0, fmt.Errorf("network poll timeout must be non-negative")
	}
	maximum := int64(math.MaxInt64) / int64(time.Millisecond)
	if milliseconds > maximum {
		return sets, 0, fmt.Errorf("network poll timeout is too large")
	}
	return sets, time.Duration(milliseconds) * time.Millisecond, nil
}

func boundedNetworkCandidates(array *value.ObjArray) []value.Value {
	if len(array.Elements) <= networkSetCapacity { return array.Elements }
	return array.Elements[:networkSetCapacity]
}
```

- [ ] **Step 4: Generalize `selectResult` without changing its shape**

Move it into `network_poller.go` and implement a literal 64-slot builder:

```go
func fixedNetworkSet(ready []value.Value) value.Value {
	elements := make([]value.Value, networkSetCapacity)
	for i := range elements { elements[i] = value.NewNull() }
	copy(elements, ready[:min(len(ready), networkSetCapacity)])
	return value.NewArray(elements)
}

func selectResult(read, write, errors []value.Value) value.Value {
	read = read[:min(len(read), networkSetCapacity)]
	write = write[:min(len(write), networkSetCapacity)]
	errors = errors[:min(len(errors), networkSetCapacity)]
	return value.NewMapWithData(map[string]value.Value{
		"read": fixedNetworkSet(read), "read_count": value.NewInt(int64(len(read))),
		"write": fixedNetworkSet(write), "write_count": value.NewInt(int64(len(write))),
		"error": fixedNetworkSet(errors), "error_count": value.NewInt(int64(len(errors))),
	})
}
```

Temporarily change the old builtin return to `selectResult(readyRead, nil, nil)` so the package remains green until Task 6 replaces its body.

- [ ] **Step 5: Run GREEN and the existing network tests**

```powershell
go test ./internal/vm -run 'TestValidateNetworkPollArguments|TestSelectResultPadsTruncates|TestNetworkBuiltinsLoopbackLifecycle' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the pure contract**

```powershell
git add internal/vm/network_poller.go internal/vm/network_poller_test.go internal/vm/builtins_net.go
git commit -m "feat(net): define network poll contract"
```

---

### Task 2: Add synchronized wake lifecycle and two-phase resource detach

**Files:**
- Modify: `internal/vm/network_poller.go`
- Modify: `internal/vm/network_poller_test.go`
- Modify: `internal/vm/resources.go:96-126`
- Modify: `internal/vm/builtins_net.go:78-241,723-760`
- Modify: `internal/vm/builtins_net_deadlines_test.go`

**Interfaces:**
- Produces: `platformNetworkWake`, `networkWake`, `detachSocketLocked`, `detachListenerLocked`, `finishSocketDetach`, and `finishListenerDetach`.
- Preserves: idempotent `closeSocket`/`closeListener`, deadline lock order, and joined rollback/close errors.

- [ ] **Step 1: Write failing wake serialization tests**

Use a real fake whose methods detect calls after close rather than asserting on a mock invocation:

```go
type fakePlatformWake struct {
	mu sync.Mutex
	signals int
	closes int
	closed bool
	afterClose bool
	signalErr error
}
func (*fakePlatformWake) descriptor() uintptr { return 99 }
func (wake *fakePlatformWake) signal() error {
	wake.mu.Lock(); defer wake.mu.Unlock()
	if wake.closed { wake.afterClose = true }
	wake.signals++
	return wake.signalErr
}
func (wake *fakePlatformWake) close() error {
	wake.mu.Lock(); defer wake.mu.Unlock()
	wake.closed = true
	wake.closes++
	return nil
}

func TestNetworkWakeSerializesSignalAndClose(t *testing.T) {
	raw := &fakePlatformWake{}
	wake := newNetworkWake(raw)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); <-start; _ = wake.Signal() }()
	}
	workers.Add(1)
	go func() { defer workers.Done(); <-start; _ = wake.Close() }()
	close(start)
	workers.Wait()
	_ = wake.Signal()
	raw.mu.Lock(); defer raw.mu.Unlock()
	if raw.afterClose { t.Fatal("platform signal ran after platform close") }
	if raw.signals > 1 || raw.closes != 1 { t.Fatalf("signals=%d closes=%d", raw.signals, raw.closes) }
}
```

Add `TestNetworkWakeSignalFailureRemainsClosable`: make `signalErr` non-nil, require `Signal` to return it without transitioning to `signaled`, then require `Close` to close the platform exactly once and every later `Signal` to be a no-op. Platform-specific tasks add retry behavior for interruptible writes; the common state machine must not hide a real signaling failure.

- [ ] **Step 2: Write failing resource broadcast/detach tests**

Register two distinct wake objects in one `SocketResource`, call `closeSocket`, and require both to be signaled, the connection to be closed, the waiter map cleared, and a second close to do nothing. Repeat for `ListenerResource`.

Cover the three existing fail-closed transitions independently:

1. initial socket `SetReadDeadline` failure plus failed read rollback (`builtins_net.go:122` before refactoring);
2. socket `SetWriteDeadline` failure plus at least one failed read/write rollback (`builtins_net.go:170`);
3. listener `SetDeadline` failure plus failed rollback (`builtins_net.go:226`).

For each, require detach, broadcast to every waiter before the controlled `Close` begins, exactly one underlying close, cleared waiter ownership, and `errors.Join` containing application, rollback, signal, and close failures that were injected for that case.

Run RED:

```powershell
go test ./internal/vm -run 'TestNetworkWake|Test(Socket|Listener)DetachWakes|TestSocketRollbackFailurePoisons' -count=1
```

Expected: build failure because wake and detach APIs do not exist.

- [ ] **Step 3: Implement the common wake state machine**

Add to `network_poller.go`:

```go
type platformNetworkWake interface {
	descriptor() uintptr
	signal() error
	close() error
}

type networkWakeState uint8
const (
	networkWakeOpen networkWakeState = iota
	networkWakeSignaled
	networkWakeClosed
)

type networkWake struct {
	mu sync.Mutex
	state networkWakeState
	platform platformNetworkWake
}

func newNetworkWake(platform platformNetworkWake) *networkWake {
	return &networkWake{platform: platform}
}

func (wake *networkWake) Signal() error {
	wake.mu.Lock()
	defer wake.mu.Unlock()
	if wake.state != networkWakeOpen { return nil }
	if err := wake.platform.signal(); err != nil { return err }
	wake.state = networkWakeSignaled
	return nil
}

func (wake *networkWake) Close() error {
	wake.mu.Lock()
	defer wake.mu.Unlock()
	if wake.state == networkWakeClosed { return nil }
	wake.state = networkWakeClosed
	return wake.platform.close()
}
```

`descriptor()` is read only while the operation owns the wake and before `Close`; expose it through a small locked helper if the race detector reports an access conflict.

- [ ] **Step 4: Add waiter ownership and locked detach payloads**

Extend both network resources with `pollWaiters map[*networkWake]struct{}`. Add payloads in `resources.go`:

```go
type detachedSocket struct { connection net.Conn; waiters []*networkWake }
type detachedListener struct { listener deadlineListener; buffered net.Conn; waiters []*networkWake }

func takeNetworkWaiters(waiters map[*networkWake]struct{}) []*networkWake {
	result := make([]*networkWake, 0, len(waiters))
	for waiter := range waiters { result = append(result, waiter) }
	return result
}
```

Implement `detachSocketLocked` and `detachListenerLocked` with the exact rule: caller already holds `stateMu`; set `closed`, increment `deadlineGeneration`, detach handles/buffers, snapshot and nil the waiter map, and return `(payload, true)`. Return `(_, false)` when already closed.

Implement finish functions that call every `Signal`, then close detached resources, and return `errors.Join` of signal/close failures. They never acquire `stateMu` or `deadlineMu`.

- [ ] **Step 5: Route normal close and every poison branch through detach**

Replace direct `closed = true` transitions in all three rollback-failure branches and in `closeSocket`/`closeListener`. For a poison path already holding `deadlineMu` and `stateMu`:

```go
detached, _ := detachSocketLocked(resource)
resource.stateMu.Unlock()
resource.deadlineMu.Unlock()
detachErr := finishSocketDetach(detached)
return errors.Join(applicationErr, rollbackErr, detachErr)
```

Use the listener equivalent and preserve buffered accepted-connection cleanup until Task 6 removes that field. Normal close ignores the returned finish error, matching current public behavior. The poller itself must surface its operation-local wake cleanup error synchronously and return no partial `SelectResult`; Task 3 tests that rule.

- [ ] **Step 6: Run focused GREEN and race tests**

```powershell
go test ./internal/vm -run 'TestNetworkWake|Test(Socket|Listener)DetachWakes|Test(SocketRead|SocketWrite|Listener)RollbackPoison|TestCloseDoes' -count=20
go test -race ./internal/vm -run 'TestNetworkWake|Test(Socket|Listener)DetachWakes|TestCloseDoes' -count=1
```

Expected: PASS with no post-close platform call, double close, deadlock, or race.

- [ ] **Step 7: Commit lifecycle ownership**

```powershell
git add internal/vm/network_poller.go internal/vm/network_poller_test.go internal/vm/resources.go internal/vm/builtins_net.go internal/vm/builtins_net_deadlines_test.go
git commit -m "refactor(net): centralize poll-aware resource close"
```

---

### Task 3: Implement candidate aggregation, stable descriptors, and global timing

**Files:**
- Modify: `internal/vm/network_poller.go`
- Modify: `internal/vm/network_poller_test.go`

**Interfaces:**
- Consumes: `networkWake`, shared listener/socket registries, and `networkSocketDescriptor`.
- Produces: `networkPlatform`, `networkPoller`, `networkRegistration`, `networkPollFD`, `networkPollBatch`, and `(*networkPoller).Poll`.

- [ ] **Step 1: Write failing fake-backend contract tests**

Define a fake platform that records one descriptor array and scripted wait outcomes. Cover these separate behaviors:

- read/write/error occurrences preserve order and duplicates while one resource is registered once;
- only the first 64 input values participate;
- a listener's normal write event is suppressed;
- listener terminal event populates requested read and error;
- generic read does not populate error;
- terminal/error/invalid events can populate all requested socket sets;
- backend failure returns an error instead of a partial result;
- no valid candidates call the injected sleeper for positive timeout and return immediately for zero;
- timeout zero calls `wait` once with zero;
- positive timeouts use one absolute deadline, ceil a sub-millisecond remainder, and cap every native chunk at one second;
- scripted Unix-style interruption retries only a positive timeout;
- a signaled local-close wake returns promptly with the closed resource omitted;
- a listener requested only for write is registered for wake-up but contributes no native descriptor, waits for the positive timeout, and remains absent from write output;
- a listener requested for error contributes a native descriptor with zero normal event flags so terminal flags remain observable;
- closing unrelated registration A cannot suppress a `SyscallConn` or `Control` error attributed to still-open registration B;
- operation-local wake close failure returns a synchronous error and no partial `SelectResult`.

The fake returns events indexed to registrations, not candidates:

```go
type networkPollBatch struct {
	events []networkEvent
	woke bool
	interrupted bool
}

type fakeNetworkPlatform struct {
	wake platformNetworkWake
	waits []networkPollBatch
	timeouts []int32
	descriptors [][]networkPollFD
	err error
}
```

- [ ] **Step 2: Write a failing nested-`Control` lifetime test**

Wrap two real loopback TCP connections with a `syscall.Conn` wrapper whose `RawConn.Control` increments an atomic active count only for the callback duration. In the fake platform's `wait`, require `active == 2`. This test must fail if descriptor numbers are copied before entering all callbacks.

Also script close at `SyscallConn` acquisition and at each nested `Control` boundary. Revalidation must classify a detached resource as local close only when it is the registration attributed by the acquisition error; the same error on an unchanged open resource must be returned synchronously. Add the adversarial case where A closes while B's `Control` returns an invariant error: B's error must win and no result is returned.

Run RED:

```powershell
go test ./internal/vm -run 'TestNetworkPoller|TestNetworkDescriptorsRemainControlled|TestNetworkControlCloseClassification' -count=1
```

- [ ] **Step 3: Define the backend boundary and registration model**

Add these exact common types:

```go
type networkPollFD struct {
	descriptor uintptr
	interests networkInterest
	listener bool
}

type networkPollBatch struct {
	events []networkEvent
	woke bool
	interrupted bool
}

type networkPlatform struct {
	newWake func() (platformNetworkWake, error)
	wait func([]networkPollFD, uintptr, int32) (networkPollBatch, error)
}

type networkPoller struct {
	platform networkPlatform
	now func() time.Time
	sleep func(time.Duration)
}

type networkRegistration struct {
	handle int
	requested networkInterest
	nativeInterests networkInterest
	pollable bool
	listener *ListenerResource
	socket *SocketResource
	attached any
	raw syscall.RawConn
}

type networkOccurrence struct {
	candidate value.Value
	registration *networkRegistration
}

type networkAcquisitionStage uint8
const (
	networkAcquireSyscallConn networkAcquisitionStage = iota
	networkAcquireControl
)

type networkAcquisitionError struct {
	registration *networkRegistration
	stage networkAcquisitionStage
	callbackEntered bool
	err error
}

func (failure *networkAcquisitionError) Error() string { return failure.err.Error() }
func (failure *networkAcquisitionError) Unwrap() error { return failure.err }
```

Use three occurrence slices plus an ordered unique-registration slice. Group by authoritative integer handle after listener-first lookup. Invalid candidate extraction is skipped. Attach one wake to each unique open resource under its `stateMu`, capture the exact connection/listener, then obtain `SyscallConn` from the captured value. Wrap each `SyscallConn` failure immediately in `networkAcquisitionError` with that registration and stage.

For sockets, `nativeInterests` equals the union of requested interests and `pollable=true`, including error-only sockets. For listeners, remove writable from `nativeInterests`; set `pollable=true` only when read or error was requested. Thus a write-only listener stays in the waiter set but never enters `poll`/`WSAPoll`, while an error-only listener remains pollable with zero requested normal-event bits.

- [ ] **Step 4: Retain every descriptor inside nested callbacks**

Build a separate `pollRegistrations` slice containing only `pollable` registrations. Implement recursion over that slice, with the native wait only at the innermost callback:

```go
func withNetworkDescriptors(registrations []*networkRegistration, pollfds []networkPollFD, index int, wait func([]networkPollFD) (networkPollBatch, error)) (batch networkPollBatch, failure *networkAcquisitionError, err error) {
	if index == len(registrations) {
		batch, err = wait(pollfds)
		return batch, nil, err
	}
	registration := registrations[index]
	callbackEntered := false
	controlErr := registration.raw.Control(func(fd uintptr) {
		callbackEntered = true
		pollfds[index] = networkPollFD{descriptor: fd, interests: registration.nativeInterests, listener: registration.listener != nil}
		batch, failure, err = withNetworkDescriptors(registrations, pollfds, index+1, wait)
	})
	if failure != nil || err != nil { return batch, failure, err }
	if controlErr != nil {
		return batch, &networkAcquisitionError{
			registration: registration,
			stage: networkAcquireControl,
			callbackEntered: callbackEntered,
			err: controlErr,
		}, nil
	}
	return batch, nil, nil
}
```

Never replace a deeper failure with an outer `Control` error. On `networkAcquisitionError`, revalidate only `failure.registration`: suppress it as local close only if that exact registry identity/attachment is detached or closed; otherwise return the wrapped error synchronously even if some unrelated registration changed concurrently.

Do not execute another native wait after any local-close wake or acquisition failure. Always unregister from every resource, then call synchronized `wake.Close`, before returning. Join backend/acquisition/cleanup errors and return no `SelectResult` whenever any non-local operation or wake-close error remains.

- [ ] **Step 5: Implement remaining-time calculation**

Use one deadline from `poller.now()` and this conversion:

```go
func networkPollMilliseconds(remaining time.Duration) int32 {
	if remaining <= 0 { return 0 }
	if remaining > time.Second { remaining = time.Second }
	milliseconds := remaining / time.Millisecond
	if remaining%time.Millisecond != 0 { milliseconds++ }
	return int32(milliseconds)
}
```

Using quotient and remainder avoids overflowing `time.Duration` while rounding a near-maximum positive duration upward. The one-second safety chunk is below the signed 32-bit platform limit and guarantees that a failed wake signal releases nested `RawConn.Control` references within a bounded interval; it does not restart or extend the absolute operation deadline.

For timeout zero with pollable registrations, invoke the backend exactly once. For positive timeout, repeat only for an interrupted result, a completed safety chunk, or a non-local spurious wake while `poller.now().Before(deadline)`. Empty inputs with no registered resources call the injected sleeper only for a positive duration. A write-only listener is not an empty input: wait on the operation wake descriptor so local close remains observable, but never pass the listener descriptor to the backend.

- [ ] **Step 6: Revalidate and reconstruct public outputs**

Under the relevant `stateMu`, require registry identity, `!closed`, and the exact captured attachment. Drop a locally closed registration. As a defensive invariant, remove listener write again during reconstruction; terminal/error keeps listener read and error. Scan each occurrence list in input order to call `selectResult`.

- [ ] **Step 7: Run GREEN, stress, and race checks**

```powershell
go test ./internal/vm -run 'TestNetworkPoller|TestNetworkDescriptorsRemainControlled|TestNetworkControlCloseClassification' -count=20
go test -race ./internal/vm -run 'TestNetworkPoller|TestNetworkDescriptorsRemainControlled|TestNetworkControlCloseClassification' -count=1
```

Expected: PASS; fake wait sees all raw controls active, timeouts never restart, and waiter maps are empty after every return.

- [ ] **Step 8: Commit the common multiplexer**

```powershell
git add internal/vm/network_poller.go internal/vm/network_poller_test.go
git commit -m "feat(net): add common readiness multiplexer"
```

---

### Task 4: Implement and verify the Windows `WSAPoll` backend

**Files:**
- Create: `internal/vm/network_poller_windows.go`
- Create: `internal/vm/network_poller_windows_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `networkPlatform`, `networkPollFD`, `networkPollBatch`, `platformNetworkWake`.
- Produces on Windows: `systemNetworkPlatform() networkPlatform`.

- [ ] **Step 1: Write failing ABI and event-mask tests**

Create a Windows-tagged test file (`//go:build windows`) that requires:

```go
func TestWSAPollFDLayout(t *testing.T) {
	var descriptor wsaPollFD
	if unsafe.Offsetof(descriptor.Events) != unsafe.Sizeof(uintptr(0)) {
		t.Fatalf("events offset=%d", unsafe.Offsetof(descriptor.Events))
	}
	if unsafe.Offsetof(descriptor.Revents) != unsafe.Sizeof(uintptr(0))+2 {
		t.Fatalf("revents offset=%d", unsafe.Offsetof(descriptor.Revents))
	}
}

func TestWindowsPollInterestNeverRequestsPOLLPRI(t *testing.T) {
	got := windowsPollEvents(networkReadable | networkWritable | networkErrorInterest)
	if got&pollPRI != 0 { t.Fatalf("events=%#x include POLLPRI", got) }
	if got != pollRDNORM|pollWRNORM { t.Fatalf("events=%#x", got) }
}

func TestWindowsPollNormalizesTerminalEvents(t *testing.T) {
	got := normalizeWindowsPollEvents(pollHUP | pollERR)
	want := networkReadReady | networkWriteReady | networkErrorReady
	if got != want { t.Fatalf("events=%#x want=%#x", got, want) }
}
```

Add a table for `POLLRDNORM`, `POLLWRNORM`, `POLLHUP`, `POLLERR`, and `POLLNVAL`. Do not include `POLLRDBAND` or `POLLPRI` in the public mapping.

- [ ] **Step 2: Write failing wake lifecycle integration tests**

Create the real platform wake, assert its descriptor is accepted by a zero-time `WSAPoll`, signal it twice through the common `networkWake`, and require the wait to return `woke=true`. Race `Signal` and `Close` 100 times and require no Winsock call after handle close or panic.

Put socket creation and close behind a `windowsNetworkOps` value owned by the backend. In a deterministic fake, count every successfully created handle and every close, inject failure after each setup stage, and require exact balance. Keep the real race test for behavior, but do not use process-wide handle counts as a leak oracle.

Add an injectable `callWSAPoll`/`callWSAGetLastError` pair and require a scripted `SOCKET_ERROR` to return the exact WSA error rather than the `LazyProc.Call` last-error value.

Run RED:

```powershell
go test ./internal/vm -run 'TestWSAPoll|TestWindowsPoll|TestWindowsWake' -count=1
```

Expected: build failure because the Windows backend does not exist.

- [ ] **Step 3: Define the Winsock ABI and explicit error retrieval**

Implement the Windows file with:

```go
//go:build windows

package vm

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type wsaPollFD struct {
	FD uintptr
	Events int16
	Revents int16
}

const (
	pollRDNORM int16 = 0x0100
	pollWRNORM int16 = 0x0010
	pollERR int16 = 0x0001
	pollHUP int16 = 0x0002
	pollNVAL int16 = 0x0004
	pollPRI int16 = 0x0400
	socketError int32 = -1
)

var (
	ws2 = windows.NewLazySystemDLL("Ws2_32.dll")
	procWSAPoll = ws2.NewProc("WSAPoll")
	procWSAGetLastError = ws2.NewProc("WSAGetLastError")
)
```

Wrap both procedure calls in package variables for deterministic error-path tests. Detect failure with `int32(result) == socketError`; call `WSAGetLastError` explicitly and return `syscall.Errno(code)`.

- [ ] **Step 4: Implement operation-local non-blocking UDP wake sockets**

Define the exact injectable operation boundary:

```go
type windowsNetworkOps struct {
	socket func(int, int, int) (windows.Handle, error)
	bind func(windows.Handle, windows.Sockaddr) error
	getsockname func(windows.Handle) (windows.Sockaddr, error)
	setNonblock func(windows.Handle, bool) error
	sendto func(windows.Handle, []byte, int, windows.Sockaddr) error
	closeSocket func(windows.Handle) error
}
```

The production `setNonblock` wrapper converts `windows.Handle` to `syscall.Handle` and calls `syscall.SetNonblock`. Create a reader with `windows.Socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP)`, bind to `127.0.0.1:0`, obtain its `*windows.SockaddrInet4` through `windows.Getsockname`, and create a writer socket. Set both handles non-blocking. On any setup failure, close every handle already created through the injected operation and return `errors.Join`.

Implement the platform wake:

```go
type windowsNetworkWake struct {
	reader windows.Handle
	writer windows.Handle
	address *windows.SockaddrInet4
	ops windowsNetworkOps
}

func (wake *windowsNetworkWake) descriptor() uintptr { return uintptr(wake.reader) }
func (wake *windowsNetworkWake) signal() error {
	err := wake.ops.sendto(wake.writer, []byte{1}, 0, wake.address)
	if errors.Is(err, windows.WSAEWOULDBLOCK) { return nil }
	return err
}
func (wake *windowsNetworkWake) close() error {
	return errors.Join(wake.ops.closeSocket(wake.writer), wake.ops.closeSocket(wake.reader))
}
```

The common `networkWake` guarantees at most one successful signal and serializes close.

- [ ] **Step 5: Implement one `WSAPoll` call and normalize events**

Prepend the wake reader with `pollRDNORM`, convert every common descriptor without narrowing, request only `pollRDNORM` and/or `pollWRNORM`, and allow zero requested flags for error-only interests. The common layer has already excluded listener write-only registrations; assert that such a listener never reaches this function with `pollWRNORM`. After the call:

- wake `pollRDNORM|pollHUP|pollERR|pollNVAL` sets `batch.woke=true`;
- `pollRDNORM` maps to read;
- `pollWRNORM` maps to write;
- `pollHUP|pollERR|pollNVAL` maps to read, write, and error;
- listener write filtering remains a defensive common-layer invariant before and after the backend;
- result event order remains aligned with the input descriptors after removing the prepended wake entry.

Return `systemNetworkPlatform()` with `interrupted=false` for every Windows result; do not invent a `WSAEINTR` retry.

Add a Windows loopback reset test: obtain the peer `*net.TCPConn`, call `SetLinger(0)`, close it to generate RST, and poll the other socket in read/write/error with a one-second global timeout. Require prompt readiness including `error_count == 1`, then call `net_recv` and require a non-timeout connection error. This proves `WSAPoll` observation did not consume or clear the pending error.

- [ ] **Step 6: Promote `x/sys` and run Windows GREEN/race tests**

Move `golang.org/x/sys v0.40.0` from the indirect block to the direct `require` block through:

```powershell
go mod tidy
go test ./internal/vm -run 'TestWSAPoll|TestWindowsPoll|TestWindowsWake' -count=20
go test -race ./internal/vm -run 'TestWSAPoll|TestWindowsPoll|TestWindowsWake' -count=1
```

Expected: PASS on Windows without `WSAEINVAL`, blocked signal, or handle race.

- [ ] **Step 7: Commit the Windows backend**

```powershell
git add internal/vm/network_poller_windows.go internal/vm/network_poller_windows_test.go go.mod go.sum
git commit -m "feat(net): add Windows WSAPoll backend"
```

---

### Task 5: Implement and cross-verify the Unix `poll` backend

**Files:**
- Create: `internal/vm/network_poller_unix.go`
- Create: `internal/vm/network_poller_unix_test.go`
- Create: `internal/vm/network_poller_linux_test.go`
- Create: `internal/vm/network_poller_rdhup.go`
- Create: `internal/vm/network_poller_no_rdhup.go`
- Create: `internal/vm/network_poller_unsupported.go`

**Interfaces:**
- Consumes: the common backend types from Task 3.
- Produces on compile-supported Unix: `systemNetworkPlatform() networkPlatform` using `unix.Poll`.
- Produces elsewhere: a stable unsupported-platform backend with the same symbol.

- [ ] **Step 1: Write failing Unix-tagged backend tests**

Create tests under the exact supported tag:

```go
//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris
```

Test literal normalization for `POLLIN`, `POLLOUT`, `POLLHUP`, `POLLERR`, and `POLLNVAL`; verify that requested interests never include `POLLPRI`. On Linux/FreeBSD, a separate tagged test requires `POLLRDHUP` both in the requested native mask for every pollable socket—including error-only—and normalized as read, write, and error. It must not be requested for listeners. On other Unix targets, require the extension mask to be zero.

Use the real socketpair wake to prove a zero-time wait is empty before signal and `woke=true` after signal. Race common `Signal`/`Close` and assert idempotent cleanup. Put `Socketpair`, `Write`, and `Close` behind a `unixNetworkOps` value owned by the backend; inject failure at every setup stage and require the number of successfully created descriptors to equal the number closed, rather than using a noisy process-wide FD count.

Run RED on Linux CI or an available Linux environment:

```bash
go test ./internal/vm -run 'TestUnixPoll|TestUnixWake|TestUnixReadHangup' -count=1
```

Expected: build failure because the Unix backend does not exist.

- [ ] **Step 2: Implement the non-blocking Unix socketpair wake**

In `network_poller_unix.go`, define injected operations and create `unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)`, call `unix.SetNonblock` on both descriptors, and close both on partial failure:

```go
type unixNetworkOps struct {
	socketpair func(int, int, int) ([2]int, error)
	setNonblock func(int, bool) error
	write func(int, []byte) (int, error)
	close func(int) error
}

type unixNetworkWake struct {
	reader, writer int
	ops unixNetworkOps
}
func (wake *unixNetworkWake) descriptor() uintptr { return uintptr(wake.reader) }
func (wake *unixNetworkWake) signal() error {
	for {
		_, err := wake.ops.write(wake.writer, []byte{1})
		if errors.Is(err, unix.EINTR) { continue }
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) { return nil }
		return err
	}
}
func (wake *unixNetworkWake) close() error {
	return errors.Join(wake.ops.close(wake.writer), wake.ops.close(wake.reader))
}
```

The common lifecycle ensures the same numeric descriptor is never signaled after close.

- [ ] **Step 3: Implement `unix.Poll` with exact descriptor checks**

Reject a raw descriptor that cannot round-trip through `int32` before building `unix.PollFd`. Prepend the wake reader with `POLLIN`. Map interests to `POLLIN` and `POLLOUT`; error-only registrations use zero normal requested events because `POLLERR`, `POLLHUP`, and `POLLNVAL` are returned independently. For every non-listener pollable socket, OR `networkPollReadHangup` into `PollFd.Events`, including error-only sockets; `POLLRDHUP` is not one of the unconditional result flags and must be explicitly requested on Linux/FreeBSD.

On `errors.Is(err, unix.EINTR)`, return `networkPollBatch{interrupted:true}` with no error. Normalize:

```go
if revents&unix.POLLIN != 0 { event |= networkReadReady }
if revents&unix.POLLOUT != 0 { event |= networkWriteReady }
if revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL|networkPollReadHangup) != 0 {
	event |= networkReadReady | networkWriteReady | networkErrorReady
}
```

Never request or publish `POLLPRI`.

- [ ] **Step 4: Split the terminal extension by build tag**

`network_poller_rdhup.go`:

```go
//go:build linux || freebsd
package vm
import "golang.org/x/sys/unix"
const networkPollReadHangup int16 = unix.POLLRDHUP
```

`network_poller_no_rdhup.go`:

```go
//go:build aix || darwin || dragonfly || netbsd || openbsd || solaris
package vm
const networkPollReadHangup int16 = 0
```

Do not reference `POLLRDHUP` from the common Unix file, because it is not defined uniformly.

In `network_poller_linux_test.go`, add two real TCP tests. First, call `CloseWrite` on one peer and require the other endpoint's raw poll result to include `POLLRDHUP`, with public read/error outputs populated when requested. Second, call `SetLinger(0)` and close a peer to generate RST; require public read/write/error readiness within one second, followed by `net_recv` returning a non-timeout connection error. The receive assertion proves poll did not clear the pending socket error.

- [ ] **Step 5: Add the unsupported backend**

Use the complement build tag:

```go
//go:build !windows && !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris
```

Return a platform whose wake factory and wait both return `fmt.Errorf("network polling is not supported on this platform")`. This keeps builds explicit instead of restoring a consuming fallback.

- [ ] **Step 6: Run Linux GREEN and cross-build every advertised target**

On Linux:

```bash
go test ./internal/vm -run 'TestUnixPoll|TestUnixWake|TestUnixReadHangup|TestLinuxNetworkPoll' -count=20
go test -race ./internal/vm -run 'TestUnixPoll|TestUnixWake|TestUnixReadHangup|TestLinuxNetworkPoll' -count=1
```

From PowerShell or CI, compile production code for the full matrix:

```powershell
$targets = @(
  @{OS='linux'; Arch='amd64'}, @{OS='darwin'; Arch='amd64'},
  @{OS='dragonfly'; Arch='amd64'}, @{OS='freebsd'; Arch='amd64'},
  @{OS='netbsd'; Arch='amd64'}, @{OS='openbsd'; Arch='amd64'},
  @{OS='solaris'; Arch='amd64'}, @{OS='aix'; Arch='ppc64'}
)
$crossBuildFailure = $null
try {
  foreach ($target in $targets) {
    $env:GOOS=$target.OS; $env:GOARCH=$target.Arch; go build ./...
    if ($LASTEXITCODE -ne 0) { throw "cross-build failed for $($target.OS)/$($target.Arch)" }
  }
} catch {
  $crossBuildFailure = $_
} finally {
  Remove-Item Env:GOOS -ErrorAction SilentlyContinue
  Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
  go build ./...
  if ($LASTEXITCODE -ne 0) { throw "native build failed after cross-build matrix" }
}
if ($null -ne $crossBuildFailure) { throw $crossBuildFailure }
```

Expected: all builds succeed. Runtime claims remain limited to Windows/Linux.

- [ ] **Step 7: Commit the Unix and fallback backends**

```powershell
git add internal/vm/network_poller_unix.go internal/vm/network_poller_unix_test.go internal/vm/network_poller_linux_test.go internal/vm/network_poller_rdhup.go internal/vm/network_poller_no_rdhup.go internal/vm/network_poller_unsupported.go
git commit -m "feat(net): add Unix poll backend"
```

---

### Task 6: Wire the builtin and remove every consuming probe

**Files:**
- Modify: `internal/vm/builtins_net.go`
- Modify: `internal/vm/resources.go`
- Modify: `internal/vm/builtins_net_test.go`
- Modify: `internal/vm/builtins_net_deadlines_test.go`
- Modify: `internal/vm/architecture_test.go`

**Interfaces:**
- Consumes: `systemNetworkPlatform()` and `(*networkPoller).Poll`.
- Removes: `beginSocketProbe`, `finishSocketProbe`, `selectSocket`, `beginListenerProbe`, `finishListenerProbe`, `selectListener`, probe deadlines, and consumer buffers.
- Preserves: ordinary `net_recv`, `net_accept`, persistent timeout behavior, and fail-closed rollback semantics.

- [ ] **Step 1: Replace buffer-oriented tests with failing non-consumption tests**

In `builtins_net_test.go`, replace `TestNetSelectBufferIsSharedAcrossVMs` with two real loopback tests:

1. Send `"xy"`, poll the accepted socket twice with `timeout=0`, assert both calls report it readable, then call `net_recv(..., 2)` from a VM sharing the same runtime and require exactly `"xy"`.
2. Establish a pending client connection, poll the listener twice with `timeout=0`, require it readable both times, then accept exactly once and prove the accepted connection works.

Add direct builtin validation tests for wrong arity, wrong set types, wrong timeout type, negative timeout, and overflow. Require a synchronous error and no partial `SelectResult`.

- [ ] **Step 2: Add failing deadline-independence and close-wakeup tests**

In `builtins_net_deadlines_test.go`:

- snapshot a controlled socket's last read/write deadlines, call `net_select`, and require no setter invocation and no deadline change;
- start a positive-timeout poll on one socket and one listener, close each from another goroutine, and require prompt completion without reporting the locally closed handle;
- force each of the three existing rollback-poison paths—initial socket read application/rollback failure, socket write application/rollback failure, and listener application/rollback failure—and require an already blocked poll to wake before the controlled connection/listener `Close` returns;
- inject a permanent wake signal failure while a poll holds nested `RawConn.Control` references; require the close path to finish within two safety chunks, proving the one-second native chunk cap provides bounded fallback progress;
- replace `TestReceiveSocketFullyBufferedSkipsDeadlineSetter` with `TestReceiveSocketZeroSizeSkipsDeadlineSetter`.

Run RED:

```powershell
go test ./internal/vm -run 'TestNetSelectDoesNotConsume|TestNetSelectDoesNotAccept|TestNetSelectDoesNotMutateDeadline|TestNetSelectCloseWakes|TestNetSelectValidates' -count=1
```

Expected: the repeated poll or deadline assertions fail against the old consuming implementation.

- [ ] **Step 3: Install one production poller and dispatch `net_select` to it**

Add an immutable package-level poller initialized from the platform factory:

```go
var defaultNetworkPoller = networkPoller{
	platform: systemNetworkPlatform(),
	now:      time.Now,
	sleep:    time.Sleep,
}
```

Replace the complete `net_select` body with `defaultNetworkPoller.Poll(context.VM, args)`; `Poll` performs the strict validation from Task 1 before resolving any resource. Do not retain the old one-millisecond minimum or sequential candidate loop.

- [ ] **Step 4: Delete consumer buffers and probe deadline machinery**

Remove from `SocketResource`:

```text
bufferedRead
readProbeDeadline
```

Remove from `ListenerResource`:

```text
bufferedAccept
acceptProbeDeadline
```

Delete all six probe helpers and every branch that publishes or consumes their buffers. Simplify the ordinary-I/O deadline helper to:

```go
func effectiveNetworkDeadline(now time.Time, timeout time.Duration) time.Time {
	if timeout > 0 { return now.Add(timeout) }
	return time.Time{}
}
```

Update `acceptConnection`, `receiveSocket`, and their rollback tests to use only persistent I/O configuration. Keep the zero-sized receive fast path and preserve `io.EOF` as a successful zero-byte `NetResult` under the existing receive contract.

- [ ] **Step 5: Add an architecture guard against regression**

Extend `architecture_test.go` with an AST/source guard that fails if `ListenerResource` or `SocketResource` regains any of these fields:

```text
bufferedAccept, bufferedRead, acceptProbeDeadline, readProbeDeadline
```

Also forbid these function declarations:

```text
beginListenerProbe, finishListenerProbe, selectListener,
beginSocketProbe, finishSocketProbe, selectSocket
```

The guard makes a future consuming fallback an explicit reviewed design change.

- [ ] **Step 6: Remove obsolete probe tests by name, not coverage**

Delete the old cases whose contract no longer exists:

```text
TestBufferedAcceptCloseWinsDuringDeadlineClear
TestNetSelectRestoresPersistentSocketDeadline
TestNetSelectRestorationFailureKeepsReadyByte
TestNetworkProbeDeadlineUsesOneStartInstant
TestPartialBufferedReadSurvivesDeadlineInstallationFailure
TestListenerSelectRestorationFailureKeepsAcceptedConnection
TestNetSelectManagementFailureStopsLaterCandidatesAndKeepsEarlierBuffer
TestActiveSocketProbeBoundsConcurrentConfiguration
```

Retain their still-relevant close, rollback, persistent timeout, and synchronization assertions in the new non-consuming tests from Step 2.

- [ ] **Step 7: Run focused GREEN and race tests**

```powershell
go test ./internal/vm -run 'TestNetSelect|TestNetworkPoller|TestNetworkWake|Test(Socket|Listener).*(Deadline|Rollback|Detach)|TestReceiveSocketZeroSize|TestNetworkArchitecture' -count=1
go test -race ./internal/vm -run 'TestNetSelect|TestNetworkPoller|TestNetworkWake|Test(Socket|Listener)Detach' -count=1
```

Expected: PASS with no reads, accepts, deadline mutations, leaked waiters, deadlocks, or races.

- [ ] **Step 8: Commit the builtin cutover**

```powershell
git add internal/vm/builtins_net.go internal/vm/resources.go internal/vm/builtins_net_test.go internal/vm/builtins_net_deadlines_test.go internal/vm/architecture_test.go
git commit -m "feat(net): replace consuming select with readiness poll"
```

---

### Task 7: Prove the public semantics with real TCP integration tests

**Files:**
- Create: `internal/vm/network_poller_integration_test.go`
- Modify: `internal/vm/builtins_net_test.go`

**Interfaces:**
- Exercises: the registered builtin, real shared resources, OS readiness, EOF/hangup, data plus terminal state, and close wakeups.
- Separates: portable guarantees from platform-specific observations.

- [ ] **Step 1: Add the real-loopback readiness matrix**

Use `setupAcceptedLoopback` and the public builtin helpers. Add independent tests for:

- repeated read polls preserve exact unread bytes;
- repeated listener polls preserve the pending accept;
- a connected client requested in the write set is reported writable;
- the same socket occurring twice in one set is returned twice in the same order;
- the same socket requested in read/write/error is independently copied to every matching output;
- candidates after index 63 are ignored and all result arrays remain exactly 64 elements;
- a zero-time empty poll returns within a conservative 100 ms test bound;
- a positive empty poll does not return materially early.

- [ ] **Step 2: Test one global timeout instead of per-candidate delay**

Create several connected but idle sockets, request all in the read set, and poll for 80 ms. Require elapsed time to remain near one timeout (for example 60-400 ms under CI), not `N * timeout`. Repeat with three empty sets to prove the no-descriptor path obeys the same global bound.

- [ ] **Step 3: Specify EOF, half-close, hangup, and coexisting data**

Add named tests with explicit assertions:

- after an orderly peer close, a socket requested only for read is reported readable and the following `net_recv` returns the existing EOF-shaped successful zero-byte result;
- after TCP half-close where supported by the test helper, read is reported; error is asserted only when the backend exposes an explicit terminal bit;
- if the peer writes `"tail"` and then closes, read readiness remains present and `net_recv` returns `"tail"` before the later EOF result;
- when a terminal/error event is observed by a platform-specific backend test, it populates every requested read/write/error occurrence;
- ordinary readable data alone never populates the error set.

Do not make a cross-platform integration assertion that graceful EOF must appear in the error set: that depends on an explicit `HUP`/`RDHUP` indication, while EOF-as-readable is the portable guarantee.

The Windows test from Task 4 and Linux test from Task 5 provide deterministic pending-error coverage with TCP RST. Both must require the socket in the public error output and then observe the still-pending connection error through `net_recv`; this is not satisfied by merely injecting a fake `POLLERR` mask. Other compile-supported Unix targets keep the portable EOF assertions until they have runtime runners.

- [ ] **Step 4: Exercise concurrent close and concurrent polls**

Start a long poll, close a requested socket/listener, and require the call to wake promptly with the locally closed resource omitted. Start several polls over the same open socket, send one byte, require every poll to observe readiness, then consume the byte once. Repeat these tests under `-race` and at least 20 iterations.

- [ ] **Step 5: Run the integration suite on the current OS**

```powershell
go test ./internal/vm -run 'TestNetworkPollIntegration|TestNetSelectDoesNot' -count=20
go test -race ./internal/vm -run 'TestNetworkPollIntegration|TestConcurrentNetSelect' -count=1
```

Expected: PASS without flaky ordering assumptions or per-candidate timeout multiplication.

- [ ] **Step 6: Commit integration coverage**

```powershell
git add internal/vm/network_poller_integration_test.go internal/vm/builtins_net_test.go
git commit -m "test(net): cover portable poll semantics"
```

---

### Task 8: Document the contract, add a Noxy example, and harden CI

**Files:**
- Create: `noxy_examples/network_poller.nx`
- Modify: `docs/NOXY_LANGUAGE_SPEC.md`
- Modify: `CHANGELOG.md`
- Modify: `.github/workflows/network-deadlines.yml`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add the executable Noxy example**

Create a bounded loopback example using `net.listen`, `net.connect`, `net.accept`, `net.socket_set`, and `net.poll`. It must:

- put the listener in the read set and show `timeout=0` before and after the connection;
- poll the connected socket in the write set;
- send `"poll-data"`, poll the server socket twice in the read set without receiving between calls, then receive and assert the exact bytes/count;
- close every socket on success and every explicit failure branch;
- use a short positive timeout and assertions so the integration runner never waits indefinitely.

Follow the repository's fixed-array syntax (`set[0] = socket`) and avoid `setblocking(false)`, because readiness no longer depends on that deprecated no-op.

- [ ] **Step 2: Replace the transitional language-spec text**

In the networking section of `NOXY_LANGUAGE_SPEC.md`, document all of the following as normative behavior:

- three independent 64-entry read/write/error input and output sets;
- first-64 truncation, stable order, duplicate occurrences, and copied-value counts;
- strict timeout semantics (`<0` error, `0` immediate, `>0` one global wall-clock bound);
- no `Read`, `Accept`, peek, `SO_ERROR`, or persistent deadline mutation;
- ordinary data is read-only readiness; EOF is always readable; explicit hangup/error/invalid events satisfy every requested set;
- listener pending accept is normal read only; listener terminal state satisfies requested read/error and never write;
- data may coexist with EOF/hangup/error and must remain consumable after poll;
- local concurrent close wakes a blocked poll and omits the detached resource;
- OOB/urgent data is outside this API;
- Windows/Linux runtime verification, listed Unix compile support, and unsupported-platform error behavior;
- `net.setblocking(sock, false)` remains a deprecated no-op.

Remove the earlier statement that `net_select` is sequential or consumer-buffered.

- [ ] **Step 3: Record the user-visible change and dependency ownership**

Add a `CHANGELOG.md` entry for non-consuming readiness, the three sets, immediate/global timeout semantics, portable EOF/hangup behavior, and concurrent-close wakeup. Confirm that `golang.org/x/sys` remains a direct dependency after the Windows and Unix backends are present, then run:

```powershell
go mod tidy
```

Inspect `go.mod`/`go.sum` and keep only dependency changes caused by the new direct Unix/Windows backends.

- [ ] **Step 4: Expand the existing network workflow**

Keep the workflow filename but rename its displayed job scope from deadlines-only to network semantics. Its runtime matrix must run the focused poll/deadline tests on `ubuntu-latest` and `windows-latest`, including `go test -race` on both if the hosted Windows toolchain supports it; otherwise retain race on Linux and document the Windows limitation in the workflow comment.

Add a cross-build job for:

```text
linux/amd64, darwin/amd64, dragonfly/amd64, freebsd/amd64,
netbsd/amd64, openbsd/amd64, solaris/amd64, aix/ppc64
```

Cross-build production packages with `go build ./...`; do not claim runtime verification for those targets.

- [ ] **Step 5: Run docs/example/workflow verification**

```powershell
go run cmd/noxy/main.go noxy_examples/network_poller.nx
go test ./internal/vm -run 'TestNetwork|TestNetSelect' -count=1
go build ./...
```

Expected: the example exits successfully and focused Go tests pass.

- [ ] **Step 6: Commit documentation and CI**

```powershell
git add noxy_examples/network_poller.nx docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md .github/workflows/network-deadlines.yml go.mod go.sum
git commit -m "docs(net): specify portable poll readiness"
```

---

### Task 9: Perform final cross-platform verification and review the diff

**Files:**
- Verify all files changed by Tasks 1-8.

- [ ] **Step 1: Format and prove no generated drift**

```powershell
gofmt -w internal/vm/network_poller*.go internal/vm/resources.go internal/vm/builtins_net.go internal/vm/builtins_net_test.go internal/vm/builtins_net_deadlines_test.go internal/vm/architecture_test.go
git diff --check
```

- [ ] **Step 2: Run the repository-required gates**

```powershell
go test ./internal/...
go test ./...
go vet ./...
go build ./...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: every command exits zero. If the integration runner reports a pre-existing excluded or environment-only failure, preserve its full output and distinguish it from this change instead of weakening coverage.

- [ ] **Step 3: Run concurrency and repetition gates**

```powershell
go test -race ./internal/vm -run 'TestNetwork|TestNetSelect|TestConcurrentNetSelect' -count=1
go test ./internal/vm -run 'TestNetworkPollIntegration|TestNetSelectCloseWakes' -count=50
```

Expected: PASS with no races or intermittent timeouts.

- [ ] **Step 4: Cross-build all compile-supported targets**

Use the Task 5 matrix and restore `GOOS`/`GOARCH` afterward. Run a final native `go build ./...` after restoration to ensure the current environment was not left cross-configured.

- [ ] **Step 5: Audit the implementation against the spec**

```powershell
rg -n 'buffered(Read|Accept)|ProbeDeadline|begin(Socket|Listener)Probe|select(Socket|Listener)|SO_ERROR|MSG_PEEK|POLLPRI|POLLRDBAND' internal/vm
git diff --stat HEAD~8..HEAD
git status --short
```

Expected: the first search finds only intentional negative architecture tests/comments, the diff contains no unrelated files, and the worktree is clean.

- [ ] **Step 6: Request an independent code review**

Use `superpowers:requesting-code-review` against the implementation range. Require the reviewer to check the approved spec specifically for descriptor lifetime, wake/close serialization, EOF versus explicit hangup, listener write suppression, duplicate ordering, global timeout math, and Windows ABI correctness. Address technically validated findings with `superpowers:receiving-code-review`, rerun the affected gates, and only then report completion.
