package vm

import (
	"errors"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

type fakePlatformWake struct {
	mu         sync.Mutex
	signals    int
	closes     int
	closed     bool
	afterClose bool
	signalErr  error
	closeErr   error
}

func (*fakePlatformWake) descriptor() uintptr { return 99 }

func (wake *fakePlatformWake) signal() error {
	wake.mu.Lock()
	defer wake.mu.Unlock()
	if wake.closed {
		wake.afterClose = true
	}
	wake.signals++
	return wake.signalErr
}

func (wake *fakePlatformWake) close() error {
	wake.mu.Lock()
	defer wake.mu.Unlock()
	wake.closed = true
	wake.closes++
	return wake.closeErr
}

type fakeNetworkPlatform struct {
	wake        *fakePlatformWake
	waits       []networkPollBatch
	timeouts    []int32
	descriptors [][]networkPollFD
	err         error
	waitFn      func([]networkPollFD, uintptr, int32) (networkPollBatch, error)
}

func (platform *fakeNetworkPlatform) boundary() networkPlatform {
	if platform.wake == nil {
		platform.wake = &fakePlatformWake{}
	}
	return networkPlatform{
		newWake: func() (platformNetworkWake, error) { return platform.wake, nil },
		wait: func(descriptors []networkPollFD, wake uintptr, timeout int32) (networkPollBatch, error) {
			platform.timeouts = append(platform.timeouts, timeout)
			platform.descriptors = append(platform.descriptors, append([]networkPollFD(nil), descriptors...))
			if platform.waitFn != nil {
				return platform.waitFn(descriptors, wake, timeout)
			}
			if platform.err != nil {
				return networkPollBatch{}, platform.err
			}
			if len(platform.waits) == 0 {
				return networkPollBatch{}, nil
			}
			batch := platform.waits[0]
			platform.waits = platform.waits[1:]
			return batch, nil
		},
	}
}

type fakeRawConn struct {
	descriptor uintptr
	controlErr error
	controlFn  func(func(uintptr)) error
}

func (raw *fakeRawConn) Control(callback func(uintptr)) error {
	if raw.controlFn != nil {
		return raw.controlFn(callback)
	}
	callback(raw.descriptor)
	return raw.controlErr
}

func (*fakeRawConn) Read(func(uintptr) bool) error  { return nil }
func (*fakeRawConn) Write(func(uintptr) bool) error { return nil }

type syscallTestConn struct {
	net.Conn
	raw       syscall.RawConn
	syscallFn func() (syscall.RawConn, error)
}

func (connection *syscallTestConn) SyscallConn() (syscall.RawConn, error) {
	if connection.syscallFn != nil {
		return connection.syscallFn()
	}
	return connection.raw, nil
}

type syscallTestListener struct {
	*net.TCPListener
	raw syscall.RawConn
}

type trackingRawConn struct {
	raw    syscall.RawConn
	active *atomic.Int32
}

func (raw *trackingRawConn) Control(callback func(uintptr)) error {
	return raw.raw.Control(func(descriptor uintptr) {
		raw.active.Add(1)
		defer raw.active.Add(-1)
		callback(descriptor)
	})
}

func (raw *trackingRawConn) Read(callback func(uintptr) bool) error {
	return raw.raw.Read(callback)
}

func (raw *trackingRawConn) Write(callback func(uintptr) bool) error {
	return raw.raw.Write(callback)
}

func TestTrackingRawConnCountsOnlyControlCallbackLifetime(t *testing.T) {
	active := &atomic.Int32{}
	var before, inside, after int32
	underlying := &fakeRawConn{controlFn: func(callback func(uintptr)) error {
		before = active.Load()
		callback(10)
		after = active.Load()
		return nil
	}}
	tracked := &trackingRawConn{raw: underlying, active: active}
	if err := tracked.Control(func(uintptr) { inside = active.Load() }); err != nil {
		t.Fatal(err)
	}
	if before != 0 || inside != 1 || after != 0 || active.Load() != 0 {
		t.Fatalf("active before=%d inside=%d after=%d final=%d, want 0,1,0,0", before, inside, after, active.Load())
	}
}

func (listener *syscallTestListener) SyscallConn() (syscall.RawConn, error) {
	return listener.raw, nil
}

func addPollTestSocket(t *testing.T, machine *VM, descriptor uintptr) (*SocketResource, value.Value) {
	t.Helper()
	connection, peer := net.Pipe()
	wrapped := &syscallTestConn{Conn: connection, raw: &fakeRawConn{descriptor: descriptor}}
	resource := &SocketResource{connection: wrapped}
	handle := machine.shared.Sockets.add(resource)
	t.Cleanup(func() {
		machine.shared.Sockets.remove(handle)
		closeSocket(resource)
		_ = peer.Close()
	})
	return resource, socketValue(handle, "poll-test", 0, true)
}

func addPollTestListener(t *testing.T, machine *VM, descriptor uintptr) (*ListenerResource, value.Value) {
	t.Helper()
	underlying, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &syscallTestListener{TCPListener: underlying, raw: &fakeRawConn{descriptor: descriptor}}
	resource := &ListenerResource{listener: wrapped}
	handle := machine.shared.Listeners.add(resource)
	t.Cleanup(func() {
		machine.shared.Listeners.remove(handle)
		closeListener(resource)
	})
	return resource, socketValue(handle, "listener-test", 0, true)
}

func pollSets(read, write, networkErrors []value.Value) [3]*value.ObjArray {
	return [3]*value.ObjArray{
		value.NewArray(read).Obj.(*value.ObjArray),
		value.NewArray(write).Obj.(*value.ObjArray),
		value.NewArray(networkErrors).Obj.(*value.ObjArray),
	}
}

func resultSet(t *testing.T, result value.Value, name string) []value.Value {
	t.Helper()
	mapping := requireBuiltinMap(t, result)
	count := requireTestMapValue(t, mapping, name+"_count").AsInt
	array := requireTestMapValue(t, mapping, name).Obj.(*value.ObjArray)
	return array.Elements[:count]
}

func requireCandidateOrder(t *testing.T, got, want []value.Value) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("candidate count=%d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index].Obj != want[index].Obj {
			t.Fatalf("candidate %d=%v, want %v", index, got[index], want[index])
		}
	}
}

func TestNetworkWakeSerializesSignalAndClose(t *testing.T) {
	raw := &fakePlatformWake{}
	wake := newNetworkWake(raw)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_ = wake.Signal()
		}()
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		_ = wake.Close()
	}()
	close(start)
	workers.Wait()
	_ = wake.Signal()
	raw.mu.Lock()
	defer raw.mu.Unlock()
	if raw.afterClose {
		t.Fatal("platform signal ran after platform close")
	}
	if raw.signals > 1 || raw.closes != 1 {
		t.Fatalf("signals=%d closes=%d", raw.signals, raw.closes)
	}
}

func TestNetworkWakeSignalFailureRemainsClosable(t *testing.T) {
	signalFailure := errors.New("wake signal failed")
	raw := &fakePlatformWake{signalErr: signalFailure}
	wake := newNetworkWake(raw)
	if err := wake.Signal(); !errors.Is(err, signalFailure) {
		t.Fatalf("signal error=%v, want %v", err, signalFailure)
	}
	if err := wake.Close(); err != nil {
		t.Fatalf("close error=%v", err)
	}
	if err := wake.Close(); err != nil {
		t.Fatalf("second close error=%v", err)
	}
	if err := wake.Signal(); err != nil {
		t.Fatalf("signal after close error=%v", err)
	}
	raw.mu.Lock()
	defer raw.mu.Unlock()
	if raw.afterClose {
		t.Fatal("platform signal ran after platform close")
	}
	if raw.signals != 1 || raw.closes != 1 {
		t.Fatalf("signals=%d closes=%d, want 1 each", raw.signals, raw.closes)
	}
}

func TestValidateNetworkPollArguments(t *testing.T) {
	empty := value.NewArray(nil)
	maximum := int64(math.MaxInt64) / int64(time.Millisecond)
	tests := []struct {
		name      string
		args      []value.Value
		want      time.Duration
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
				if err == nil || err.Error() != test.wantError {
					t.Fatalf("error=%v want=%q", err, test.wantError)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("timeout=%v error=%v want=%v", got, err, test.want)
			}
		})
	}
}

func TestSelectResultPadsTruncatesAndCountsCopiedValues(t *testing.T) {
	values := make([]value.Value, 65)
	for i := range values {
		values[i] = socketValue(i+1, "test", 0, true)
	}
	result := selectResult(values, values[:2], values[:1])
	mapping := requireBuiltinMap(t, result)
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "read_count"), value.NewInt(64))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "write_count"), value.NewInt(2))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "error_count"), value.NewInt(1))
	read := requireTestMapValue(t, mapping, "read").Obj.(*value.ObjArray)
	if len(read.Elements) != 64 {
		t.Fatalf("read length=%d", len(read.Elements))
	}
}

func TestNetworkPollerPreservesOccurrenceOrderAndRegistersEachResourceOnce(t *testing.T) {
	machine := New()
	_, first := addPollTestSocket(t, machine, 10)
	_, second := addPollTestSocket(t, machine, 20)
	platform := &fakeNetworkPlatform{waits: []networkPollBatch{{events: []networkEvent{
		networkReadReady | networkWriteReady | networkErrorReady,
		networkReadReady | networkWriteReady | networkErrorReady,
	}}}}
	poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}

	result, err := poller.Poll(machine.shared, pollSets(
		[]value.Value{first, second, first},
		[]value.Value{second, first, second},
		[]value.Value{first, first, second},
	), 0)
	if err != nil {
		t.Fatal(err)
	}
	requireCandidateOrder(t, resultSet(t, result, "read"), []value.Value{first, second, first})
	requireCandidateOrder(t, resultSet(t, result, "write"), []value.Value{second, first, second})
	requireCandidateOrder(t, resultSet(t, result, "error"), []value.Value{first, first, second})
	if len(platform.descriptors) != 1 || len(platform.descriptors[0]) != 2 {
		t.Fatalf("descriptor batches=%v, want one batch with two unique resources", platform.descriptors)
	}
}

func TestNetworkPollerLimitsEachInputSetToFirst64Values(t *testing.T) {
	machine := New()
	_, first := addPollTestSocket(t, machine, 10)
	_, excluded := addPollTestSocket(t, machine, 20)
	read := make([]value.Value, 65)
	for index := range read[:64] {
		read[index] = first
	}
	read[64] = excluded
	platform := &fakeNetworkPlatform{waits: []networkPollBatch{{events: []networkEvent{networkReadReady}}}}
	poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}

	result, err := poller.Poll(machine.shared, pollSets(read, nil, nil), 0)
	if err != nil {
		t.Fatal(err)
	}
	ready := resultSet(t, result, "read")
	if len(ready) != 64 {
		t.Fatalf("read count=%d, want 64", len(ready))
	}
	if len(platform.descriptors[0]) != 1 || platform.descriptors[0][0].descriptor != 10 {
		t.Fatalf("descriptors=%v, want only first resource", platform.descriptors[0])
	}
}

func TestNetworkPollerMapsListenerAndSocketTerminalEvents(t *testing.T) {
	t.Run("listener normal write is suppressed", func(t *testing.T) {
		machine := New()
		_, listener := addPollTestListener(t, machine, 25)
		platform := &fakeNetworkPlatform{waits: []networkPollBatch{{events: []networkEvent{networkWriteReady}}}}
		poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
		result, err := poller.Poll(machine.shared, pollSets([]value.Value{listener}, []value.Value{listener}, nil), 0)
		if err != nil {
			t.Fatal(err)
		}
		requireCandidateOrder(t, resultSet(t, result, "write"), nil)
	})

	t.Run("listener suppresses write and terminal populates requested read and error", func(t *testing.T) {
		machine := New()
		_, listener := addPollTestListener(t, machine, 30)
		platform := &fakeNetworkPlatform{waits: []networkPollBatch{{events: []networkEvent{networkErrorReady}}}}
		poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
		result, err := poller.Poll(machine.shared, pollSets([]value.Value{listener}, []value.Value{listener}, []value.Value{listener}), 0)
		if err != nil {
			t.Fatal(err)
		}
		requireCandidateOrder(t, resultSet(t, result, "read"), []value.Value{listener})
		requireCandidateOrder(t, resultSet(t, result, "write"), nil)
		requireCandidateOrder(t, resultSet(t, result, "error"), []value.Value{listener})
		if got := platform.descriptors[0][0].interests; got != networkReadable|networkErrorInterest {
			t.Fatalf("listener interests=%v, want read|error", got)
		}
	})

	t.Run("generic read does not populate error", func(t *testing.T) {
		machine := New()
		_, socket := addPollTestSocket(t, machine, 40)
		platform := &fakeNetworkPlatform{waits: []networkPollBatch{{events: []networkEvent{networkReadReady}}}}
		poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
		result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, []value.Value{socket}), 0)
		if err != nil {
			t.Fatal(err)
		}
		requireCandidateOrder(t, resultSet(t, result, "read"), []value.Value{socket})
		requireCandidateOrder(t, resultSet(t, result, "error"), nil)
	})

	t.Run("socket terminal populates every requested set", func(t *testing.T) {
		machine := New()
		_, socket := addPollTestSocket(t, machine, 50)
		platform := &fakeNetworkPlatform{waits: []networkPollBatch{{events: []networkEvent{networkErrorReady}}}}
		poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
		result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, []value.Value{socket}, []value.Value{socket}), 0)
		if err != nil {
			t.Fatal(err)
		}
		requireCandidateOrder(t, resultSet(t, result, "read"), []value.Value{socket})
		requireCandidateOrder(t, resultSet(t, result, "write"), []value.Value{socket})
		requireCandidateOrder(t, resultSet(t, result, "error"), []value.Value{socket})
	})
}

func TestNetworkPollerBackendFailureReturnsNoPartialResult(t *testing.T) {
	machine := New()
	_, socket := addPollTestSocket(t, machine, 10)
	failure := errors.New("backend failed")
	platform := &fakeNetworkPlatform{err: failure}
	poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
	result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), 0)
	if !errors.Is(err, failure) || result.Type != value.VAL_NULL {
		t.Fatalf("result=%v error=%v, want null and backend failure", result, err)
	}
}

func TestNetworkPollerEmptyCandidatesUseInjectedSleeperOnlyForPositiveTimeout(t *testing.T) {
	platform := &fakeNetworkPlatform{}
	var slept []time.Duration
	start := time.Unix(100, 0)
	poller := networkPoller{platform: platform.boundary(), now: func() time.Time { return start }, sleep: func(duration time.Duration) { slept = append(slept, duration) }}
	machine := New()
	invalid := value.NewString("not a socket")

	if _, err := poller.Poll(machine.shared, pollSets([]value.Value{invalid}, nil, nil), 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := poller.Poll(machine.shared, pollSets(nil, nil, nil), 0); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != 25*time.Millisecond || len(platform.timeouts) != 0 {
		t.Fatalf("slept=%v waits=%v", slept, platform.timeouts)
	}
}

func TestNetworkPollerPoisonClosedCandidateSleepsRemainingPositiveTimeout(t *testing.T) {
	machine := New()
	resource, socket := addPollTestSocket(t, machine, 10)
	closeSocket(resource)

	start := time.Unix(100, 0)
	times := []time.Time{start, start.Add(7 * time.Millisecond)}
	now := func() time.Time {
		current := times[0]
		if len(times) > 1 {
			times = times[1:]
		}
		return current
	}
	var slept []time.Duration
	platform := &fakeNetworkPlatform{}
	poller := networkPoller{
		platform: platform.boundary(),
		now:      now,
		sleep:    func(duration time.Duration) { slept = append(slept, duration) },
	}

	result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != 3*time.Millisecond {
		t.Fatalf("slept=%v, want literal remaining budget [3ms]", slept)
	}
	if len(platform.timeouts) != 0 || len(resultSet(t, result, "read")) != 0 {
		t.Fatalf("waits=%v result=%v, want empty result without native wait", platform.timeouts, result)
	}
}

func TestNetworkPollerZeroTimeoutCallsWaitExactlyOnce(t *testing.T) {
	machine := New()
	_, socket := addPollTestSocket(t, machine, 10)
	platform := &fakeNetworkPlatform{waits: []networkPollBatch{{interrupted: true}, {events: []networkEvent{networkReadReady}}}}
	poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
	result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.timeouts) != 1 || platform.timeouts[0] != 0 || len(resultSet(t, result, "read")) != 0 {
		t.Fatalf("timeouts=%v result=%v", platform.timeouts, result)
	}
}

func TestNetworkPollerPositiveTimeoutUsesAbsoluteDeadlineCeilingAndOneSecondChunks(t *testing.T) {
	machine := New()
	_, socket := addPollTestSocket(t, machine, 10)
	start := time.Unix(100, 0)
	times := []time.Time{start, start, start.Add(1499*time.Millisecond + 500*time.Microsecond), start.Add(1500 * time.Millisecond)}
	nextTime := func() time.Time {
		current := times[0]
		if len(times) > 1 {
			times = times[1:]
		}
		return current
	}
	platform := &fakeNetworkPlatform{}
	poller := networkPoller{platform: platform.boundary(), now: nextTime, sleep: time.Sleep}
	if _, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(platform.timeouts) != 2 || platform.timeouts[0] != 1000 || platform.timeouts[1] != 1 {
		t.Fatalf("timeouts=%v, want [1000 1]", platform.timeouts)
	}
}

func TestNetworkPollerDeadlineStartsBeforeRegistryLookup(t *testing.T) {
	machine := New()
	_, socket := addPollTestSocket(t, machine, 10)
	start := time.Unix(100, 0)
	var elapsed atomic.Int64
	firstClockRead := make(chan struct{}, 1)
	platform := &fakeNetworkPlatform{waits: []networkPollBatch{{events: []networkEvent{networkReadReady}}}}
	poller := networkPoller{
		platform: platform.boundary(),
		now: func() time.Time {
			select {
			case firstClockRead <- struct{}{}:
			default:
			}
			return start.Add(time.Duration(elapsed.Load()))
		},
		sleep: time.Sleep,
	}

	machine.shared.Listeners.mu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), 10*time.Millisecond)
		done <- err
	}()
	clockReadBeforeLookup := false
	select {
	case <-firstClockRead:
		clockReadBeforeLookup = true
	case <-time.After(time.Second):
	}
	elapsed.Store(int64(7 * time.Millisecond))
	machine.shared.Listeners.mu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !clockReadBeforeLookup {
		t.Fatal("operation clock was not read before the contended registry lookup")
	}
	if len(platform.timeouts) != 1 || platform.timeouts[0] != 3 {
		t.Fatalf("timeouts=%v, want literal remaining budget [3]", platform.timeouts)
	}
}

func TestNetworkPollerEmptyInputSleepsOnlyRemainingBudget(t *testing.T) {
	start := time.Unix(100, 0)
	times := []time.Time{start, start.Add(7 * time.Millisecond)}
	now := func() time.Time {
		current := times[0]
		if len(times) > 1 {
			times = times[1:]
		}
		return current
	}
	var slept []time.Duration
	poller := networkPoller{
		platform: (&fakeNetworkPlatform{}).boundary(),
		now:      now,
		sleep:    func(duration time.Duration) { slept = append(slept, duration) },
	}
	if _, err := poller.Poll(New().shared, pollSets(nil, nil, nil), 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] != 3*time.Millisecond {
		t.Fatalf("slept=%v, want literal remaining budget [3ms]", slept)
	}
}

func TestNetworkPollerRetriesInterruptionOnlyForPositiveTimeout(t *testing.T) {
	machine := New()
	_, socket := addPollTestSocket(t, machine, 10)
	start := time.Unix(100, 0)
	now := start
	platform := &fakeNetworkPlatform{waits: []networkPollBatch{{interrupted: true}, {events: []networkEvent{networkReadReady}}}}
	poller := networkPoller{platform: platform.boundary(), now: func() time.Time { return now }, sleep: time.Sleep}
	result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.timeouts) != 2 {
		t.Fatalf("wait count=%d, want 2", len(platform.timeouts))
	}
	requireCandidateOrder(t, resultSet(t, result, "read"), []value.Value{socket})
}

func TestNetworkPollerRetriesNonLocalSpuriousWake(t *testing.T) {
	machine := New()
	_, socket := addPollTestSocket(t, machine, 10)
	start := time.Unix(100, 0)
	platform := &fakeNetworkPlatform{waits: []networkPollBatch{{woke: true}, {events: []networkEvent{networkReadReady}}}}
	poller := networkPoller{platform: platform.boundary(), now: func() time.Time { return start }, sleep: time.Sleep}
	result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.timeouts) != 2 {
		t.Fatalf("wait count=%d, want 2", len(platform.timeouts))
	}
	requireCandidateOrder(t, resultSet(t, result, "read"), []value.Value{socket})
}

func TestNetworkPollerCloseBeforeAttachmentReturnsWithoutWaiting(t *testing.T) {
	machine := New()
	resource, socket := addPollTestSocket(t, machine, 10)
	platform := &fakeNetworkPlatform{}
	closeSocket(resource)
	poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
	result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.timeouts) != 0 || len(resultSet(t, result, "read")) != 0 {
		t.Fatalf("waits=%v result=%v", platform.timeouts, result)
	}
}

func TestNetworkPollerLocalCloseWakeReturnsPromptlyAndUnregistersWaiter(t *testing.T) {
	machine := New()
	resource, socket := addPollTestSocket(t, machine, 10)
	platform := &fakeNetworkPlatform{}
	platform.waitFn = func([]networkPollFD, uintptr, int32) (networkPollBatch, error) {
		machine.shared.Sockets.remove(int(requireTestMapFD(t, socket)))
		closeSocket(resource)
		return networkPollBatch{woke: true}, nil
	}
	poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
	result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(resultSet(t, result, "read")) != 0 || len(platform.timeouts) != 1 {
		t.Fatalf("result=%v waits=%d", result, len(platform.timeouts))
	}
	resource.stateMu.Lock()
	waiters := len(resource.pollWaiters)
	resource.stateMu.Unlock()
	if waiters != 0 {
		t.Fatalf("waiters=%d, want 0", waiters)
	}
}

func requireTestMapFD(t *testing.T, socket value.Value) int64 {
	t.Helper()
	mapping := socket.Obj.(*value.ObjMap)
	fd, _ := mapping.Get("fd")
	return fd.AsInt
}

func TestNetworkPollerWriteOnlyListenerWaitsWithoutNativeDescriptor(t *testing.T) {
	machine := New()
	resource, listener := addPollTestListener(t, machine, 30)
	start := time.Unix(100, 0)
	times := []time.Time{start, start, start.Add(5 * time.Millisecond)}
	now := func() time.Time {
		current := times[0]
		if len(times) > 1 {
			times = times[1:]
		}
		return current
	}
	platform := &fakeNetworkPlatform{}
	poller := networkPoller{platform: platform.boundary(), now: now, sleep: time.Sleep}
	result, err := poller.Poll(machine.shared, pollSets(nil, []value.Value{listener}, nil), 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.descriptors) != 1 || len(platform.descriptors[0]) != 0 || platform.timeouts[0] != 5 {
		t.Fatalf("descriptors=%v timeouts=%v", platform.descriptors, platform.timeouts)
	}
	if len(resultSet(t, result, "write")) != 0 {
		t.Fatalf("write-only listener unexpectedly ready")
	}
	resource.stateMu.Lock()
	waiters := len(resource.pollWaiters)
	resource.stateMu.Unlock()
	if waiters != 0 {
		t.Fatalf("waiters=%d, want 0", waiters)
	}
}

func TestNetworkPollerPermanentWakeFailureStillDetectsClosedWriteOnlyListenerAfterOneChunk(t *testing.T) {
	machine := New()
	resource, listener := addPollTestListener(t, machine, 30)
	start := time.Unix(100, 0)
	times := []time.Time{start, start, start.Add(time.Second), start.Add(3 * time.Second)}
	now := func() time.Time {
		current := times[0]
		if len(times) > 1 {
			times = times[1:]
		}
		return current
	}
	wakeFailure := errors.New("permanent wake signal failure")
	platform := &fakeNetworkPlatform{wake: &fakePlatformWake{signalErr: wakeFailure}}
	platform.waitFn = func(descriptors []networkPollFD, _ uintptr, _ int32) (networkPollBatch, error) {
		if len(descriptors) != 0 {
			t.Fatalf("write-only listener descriptors=%v, want wake descriptor only", descriptors)
		}
		closeListener(resource)
		return networkPollBatch{}, nil
	}
	poller := networkPoller{platform: platform.boundary(), now: now, sleep: time.Sleep}

	result, err := poller.Poll(machine.shared, pollSets(nil, []value.Value{listener}, nil), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.timeouts) != 1 || platform.timeouts[0] != 1000 {
		t.Fatalf("native timeouts=%v, want one safety chunk [1000]", platform.timeouts)
	}
	platform.wake.mu.Lock()
	signals := platform.wake.signals
	platform.wake.mu.Unlock()
	if signals != 1 {
		t.Fatalf("wake signal attempts=%d, want one permanent failure", signals)
	}
	if len(resultSet(t, result, "write")) != 0 {
		t.Fatalf("closed write-only listener unexpectedly ready: %v", result)
	}
}

func TestNetworkPollerErrorOnlyListenerRemainsPollable(t *testing.T) {
	machine := New()
	_, listener := addPollTestListener(t, machine, 30)
	platform := &fakeNetworkPlatform{waits: []networkPollBatch{{events: []networkEvent{networkErrorReady}}}}
	poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
	result, err := poller.Poll(machine.shared, pollSets(nil, nil, []value.Value{listener}), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(platform.descriptors[0]) != 1 || platform.descriptors[0][0].interests != networkErrorInterest {
		t.Fatalf("descriptors=%v", platform.descriptors[0])
	}
	requireCandidateOrder(t, resultSet(t, result, "error"), []value.Value{listener})
}

func TestNetworkPollerWakeCloseFailureReturnsNoPartialResult(t *testing.T) {
	machine := New()
	_, socket := addPollTestSocket(t, machine, 10)
	closeFailure := errors.New("wake close failed")
	platform := &fakeNetworkPlatform{
		wake:  &fakePlatformWake{closeErr: closeFailure},
		waits: []networkPollBatch{{events: []networkEvent{networkReadReady}}},
	}
	poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
	result, err := poller.Poll(machine.shared, pollSets([]value.Value{socket}, nil, nil), 0)
	if !errors.Is(err, closeFailure) || result.Type != value.VAL_NULL {
		t.Fatalf("result=%v error=%v, want null and close failure", result, err)
	}
}

func TestNetworkDescriptorsRemainControlledDuringWait(t *testing.T) {
	machine := New()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	active := &atomic.Int32{}
	values := make([]value.Value, 0, 2)
	for index := 0; index < 2; index++ {
		client, dialErr := net.Dial("tcp", listener.Addr().String())
		if dialErr != nil {
			t.Fatal(dialErr)
		}
		server, acceptErr := listener.Accept()
		if acceptErr != nil {
			t.Fatal(acceptErr)
		}
		t.Cleanup(func() {
			_ = client.Close()
			_ = server.Close()
		})
		syscallConnection := client.(syscall.Conn)
		raw, rawErr := syscallConnection.SyscallConn()
		if rawErr != nil {
			t.Fatal(rawErr)
		}
		wrapped := &syscallTestConn{Conn: client, raw: &trackingRawConn{raw: raw, active: active}}
		resource := &SocketResource{connection: wrapped}
		handle := machine.shared.Sockets.add(resource)
		t.Cleanup(func() {
			machine.shared.Sockets.remove(handle)
			closeSocket(resource)
		})
		values = append(values, socketValue(handle, "loopback", 0, true))
	}
	platform := &fakeNetworkPlatform{}
	platform.waitFn = func(descriptors []networkPollFD, _ uintptr, _ int32) (networkPollBatch, error) {
		if got := active.Load(); got != 2 {
			t.Fatalf("active RawConn.Control callbacks=%d, want 2", got)
		}
		return networkPollBatch{events: []networkEvent{networkReadReady, networkReadReady}}, nil
	}
	poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
	if _, err := poller.Poll(machine.shared, pollSets(values, nil, nil), 0); err != nil {
		t.Fatal(err)
	}
}

func TestNetworkControlCloseClassification(t *testing.T) {
	closedFailure := errors.New("descriptor closed during acquisition")

	t.Run("SyscallConn close of attributed registration is local", func(t *testing.T) {
		machine := New()
		connection, peer := net.Pipe()
		t.Cleanup(func() { _ = peer.Close() })
		resource := &SocketResource{}
		handle := machine.shared.Sockets.add(resource)
		wrapped := &syscallTestConn{Conn: connection}
		resource.connection = wrapped
		wrapped.syscallFn = func() (syscall.RawConn, error) {
			machine.shared.Sockets.remove(handle)
			closeSocket(resource)
			return nil, closedFailure
		}
		platform := &fakeNetworkPlatform{}
		poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
		result, err := poller.Poll(machine.shared, pollSets([]value.Value{socketValue(handle, "local", 0, true)}, nil, nil), time.Second)
		if err != nil || len(resultSet(t, result, "read")) != 0 || len(platform.timeouts) != 0 {
			t.Fatalf("result=%v error=%v waits=%v", result, err, platform.timeouts)
		}
	})

	t.Run("SyscallConn error on unchanged registration is synchronous", func(t *testing.T) {
		machine := New()
		connection, peer := net.Pipe()
		t.Cleanup(func() { _ = peer.Close() })
		wrapped := &syscallTestConn{Conn: connection, syscallFn: func() (syscall.RawConn, error) { return nil, closedFailure }}
		resource := &SocketResource{connection: wrapped}
		handle := machine.shared.Sockets.add(resource)
		t.Cleanup(func() {
			machine.shared.Sockets.remove(handle)
			closeSocket(resource)
		})
		platform := &fakeNetworkPlatform{}
		poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
		result, err := poller.Poll(machine.shared, pollSets([]value.Value{socketValue(handle, "open", 0, true)}, nil, nil), time.Second)
		if !errors.Is(err, closedFailure) || result.Type != value.VAL_NULL || len(platform.timeouts) != 0 {
			t.Fatalf("result=%v error=%v waits=%v", result, err, platform.timeouts)
		}
	})

	for _, closeIndex := range []int{0, 1} {
		t.Run("Control close at nested boundary "+string(rune('A'+closeIndex)), func(t *testing.T) {
			machine := New()
			resources := make([]*SocketResource, 2)
			handles := make([]int, 2)
			values := make([]value.Value, 2)
			for index := range resources {
				connection, peer := net.Pipe()
				t.Cleanup(func() { _ = peer.Close() })
				raw := &fakeRawConn{descriptor: uintptr(index + 10)}
				wrapped := &syscallTestConn{Conn: connection, raw: raw}
				resources[index] = &SocketResource{connection: wrapped}
				handles[index] = machine.shared.Sockets.add(resources[index])
				values[index] = socketValue(handles[index], "control", 0, true)
			}
			t.Cleanup(func() {
				for index, resource := range resources {
					machine.shared.Sockets.remove(handles[index])
					closeSocket(resource)
				}
			})
			target := resources[closeIndex].connection.(*syscallTestConn).raw.(*fakeRawConn)
			target.controlFn = func(func(uintptr)) error {
				machine.shared.Sockets.remove(handles[closeIndex])
				closeSocket(resources[closeIndex])
				return closedFailure
			}
			platform := &fakeNetworkPlatform{}
			poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
			result, err := poller.Poll(machine.shared, pollSets(values, nil, nil), time.Second)
			if err != nil || result.Type == value.VAL_NULL || len(platform.timeouts) != 0 {
				t.Fatalf("result=%v error=%v waits=%v", result, err, platform.timeouts)
			}
		})
	}

	t.Run("unrelated close cannot suppress SyscallConn error", func(t *testing.T) {
		machine := New()
		first, firstValue := addPollTestSocket(t, machine, 10)
		connection, peer := net.Pipe()
		t.Cleanup(func() { _ = peer.Close() })
		second := &SocketResource{}
		secondHandle := machine.shared.Sockets.add(second)
		wrapped := &syscallTestConn{Conn: connection}
		second.connection = wrapped
		wrapped.syscallFn = func() (syscall.RawConn, error) {
			firstHandle, _ := networkSocketDescriptor(firstValue)
			machine.shared.Sockets.remove(firstHandle)
			closeSocket(first)
			return nil, closedFailure
		}
		t.Cleanup(func() {
			machine.shared.Sockets.remove(secondHandle)
			closeSocket(second)
		})
		platform := &fakeNetworkPlatform{}
		poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
		result, err := poller.Poll(machine.shared, pollSets([]value.Value{firstValue, socketValue(secondHandle, "second", 0, true)}, nil, nil), time.Second)
		if !errors.Is(err, closedFailure) || result.Type != value.VAL_NULL {
			t.Fatalf("result=%v error=%v", result, err)
		}
	})

	t.Run("deeper Control error wins over unrelated outer close", func(t *testing.T) {
		machine := New()
		first, firstValue := addPollTestSocket(t, machine, 10)
		second, secondValue := addPollTestSocket(t, machine, 20)
		outerFailure := errors.New("first outer control failure")
		deeperFailure := errors.New("second deeper control failure")
		firstRaw := first.connection.(*syscallTestConn).raw.(*fakeRawConn)
		firstRaw.controlErr = outerFailure
		secondRaw := second.connection.(*syscallTestConn).raw.(*fakeRawConn)
		secondRaw.controlFn = func(func(uintptr)) error {
			firstHandle, _ := networkSocketDescriptor(firstValue)
			machine.shared.Sockets.remove(firstHandle)
			closeSocket(first)
			return deeperFailure
		}
		platform := &fakeNetworkPlatform{}
		poller := networkPoller{platform: platform.boundary(), now: time.Now, sleep: time.Sleep}
		result, err := poller.Poll(machine.shared, pollSets([]value.Value{firstValue, secondValue}, nil, nil), time.Second)
		var acquisitionFailure *networkAcquisitionError
		if !errors.Is(err, deeperFailure) || errors.Is(err, outerFailure) || !errors.As(err, &acquisitionFailure) ||
			acquisitionFailure.registration.socket != second || acquisitionFailure.stage != networkAcquireControl ||
			result.Type != value.VAL_NULL || len(platform.timeouts) != 0 {
			t.Fatalf("result=%v error=%v waits=%v", result, err, platform.timeouts)
		}
	})
}
