//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package vm

import (
	"errors"
	"runtime"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestUnixPollEventMapping(t *testing.T) {
	tests := []struct {
		name string
		poll uint16
		want networkEvent
	}{
		{name: "POLLIN", poll: unix.POLLIN, want: networkReadReady},
		{name: "POLLOUT", poll: unix.POLLOUT, want: networkWriteReady},
		{name: "POLLHUP", poll: unix.POLLHUP, want: networkReadReady | networkWriteReady | networkErrorReady},
		{name: "POLLERR", poll: unix.POLLERR, want: networkReadReady | networkWriteReady | networkErrorReady},
		{name: "POLLNVAL", poll: unix.POLLNVAL, want: networkReadReady | networkWriteReady | networkErrorReady},
		{name: "POLLPRI", poll: unix.POLLPRI, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeUnixPollEvents(test.poll); got != test.want {
				t.Fatalf("events=%#x, want %#x", got, test.want)
			}
		})
	}
}

func TestUnixPollInterestNeverRequestsPOLLPRI(t *testing.T) {
	got := unixPollEvents(networkReadable | networkWritable | networkErrorInterest)
	if got&uint16(unix.POLLPRI) != 0 {
		t.Fatalf("events=%#x include POLLPRI", got)
	}
	if got != uint16(unix.POLLIN)|uint16(unix.POLLOUT) {
		t.Fatalf("events=%#x, want %#x", got, uint16(unix.POLLIN)|uint16(unix.POLLOUT))
	}
	if got := unixPollEvents(networkErrorInterest); got != 0 {
		t.Fatalf("error-only events=%#x, want zero normal flags", got)
	}
}

func TestUnixReadHangupMaskConfiguration(t *testing.T) {
	switch runtime.GOOS {
	case "linux", "freebsd":
		if networkPollReadHangup == 0 {
			t.Fatal("POLLRDHUP extension mask is zero")
		}
		want := networkReadReady | networkWriteReady | networkErrorReady
		if got := normalizeUnixPollEvents(uint16(networkPollReadHangup)); got != want {
			t.Fatalf("normalized POLLRDHUP=%#x, want %#x", got, want)
		}
	default:
		if networkPollReadHangup != 0 {
			t.Fatalf("extension mask=%#x, want zero", networkPollReadHangup)
		}
	}
}

func TestUnixPollPreservesOrderAndSocketReadHangupInterest(t *testing.T) {
	originalPoll := callUnixPoll
	t.Cleanup(func() { callUnixPoll = originalPoll })
	callUnixPoll = func(pollfds []unix.PollFd, timeout int) (int, error) {
		if timeout != 37 {
			t.Fatalf("timeout=%d, want 37", timeout)
		}
		if len(pollfds) != 4 {
			t.Fatalf("descriptor count=%d, want 4", len(pollfds))
		}
		if pollfds[0].Fd != 99 || uint16(pollfds[0].Events) != uint16(unix.POLLIN) {
			t.Fatalf("wake descriptor=%+v", pollfds[0])
		}
		wantFDs := []int32{11, 12, 13}
		wantEvents := []uint16{
			uint16(unix.POLLIN) | uint16(networkPollReadHangup),
			uint16(networkPollReadHangup),
			uint16(unix.POLLIN),
		}
		for index := range wantFDs {
			got := pollfds[index+1]
			if got.Fd != wantFDs[index] || uint16(got.Events) != wantEvents[index] {
				t.Fatalf("descriptor %d=%+v, want fd=%d events=%#x", index, got, wantFDs[index], wantEvents[index])
			}
			if uint16(got.Events)&uint16(unix.POLLPRI) != 0 {
				t.Fatalf("descriptor %d events=%#x include POLLPRI", index, got.Events)
			}
		}
		pollfds[0].Revents = unix.POLLIN
		pollfds[1].Revents = unix.POLLOUT
		pollfds[2].Revents = unix.POLLERR
		pollfds[3].Revents = unix.POLLOUT
		return 4, nil
	}

	descriptors := []networkPollFD{
		{descriptor: 11, interests: networkReadable},
		{descriptor: 12, interests: networkErrorInterest},
		{descriptor: 13, interests: networkReadable | networkWritable, listener: true},
	}
	batch, err := unixNetworkWait(descriptors, 99, 37)
	if err != nil {
		t.Fatal(err)
	}
	if !batch.woke || batch.interrupted {
		t.Fatalf("batch woke=%t interrupted=%t", batch.woke, batch.interrupted)
	}
	want := []networkEvent{
		networkWriteReady,
		networkReadReady | networkWriteReady | networkErrorReady,
		0,
	}
	if len(batch.events) != len(want) {
		t.Fatalf("events=%v, want %v", batch.events, want)
	}
	for index := range want {
		if batch.events[index] != want[index] {
			t.Fatalf("event %d=%#x, want %#x", index, batch.events[index], want[index])
		}
	}
}

func TestUnixPollRejectsDescriptorsThatDoNotRoundTripThroughInt32(t *testing.T) {
	originalPoll := callUnixPoll
	t.Cleanup(func() { callUnixPoll = originalPoll })
	called := false
	callUnixPoll = func([]unix.PollFd, int) (int, error) {
		called = true
		return 0, nil
	}

	tooLarge := uintptr(1 << 31)
	for _, test := range []struct {
		name        string
		descriptors []networkPollFD
		wake        uintptr
	}{
		{name: "wake", wake: tooLarge},
		{name: "socket", descriptors: []networkPollFD{{descriptor: tooLarge, interests: networkReadable}}, wake: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			called = false
			if _, err := unixNetworkWait(test.descriptors, test.wake, 0); err == nil {
				t.Fatal("wait accepted descriptor that cannot round-trip through int32")
			}
			if called {
				t.Fatal("poll called after descriptor validation failed")
			}
		})
	}
}

func TestUnixPollEINTRReturnsInterruptedBatch(t *testing.T) {
	originalPoll := callUnixPoll
	t.Cleanup(func() { callUnixPoll = originalPoll })
	callUnixPoll = func([]unix.PollFd, int) (int, error) { return 0, unix.EINTR }
	batch, err := unixNetworkWait(nil, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !batch.interrupted || batch.woke || len(batch.events) != 0 {
		t.Fatalf("batch=%+v, want interrupted-only", batch)
	}
}

func TestUnixWakeSocketpairSignalsPoll(t *testing.T) {
	platform := systemNetworkPlatform()
	platformWake, err := platform.newWake()
	if err != nil {
		t.Fatal(err)
	}
	wake := newNetworkWake(platformWake)
	t.Cleanup(func() {
		if err := wake.Close(); err != nil {
			t.Errorf("close wake: %v", err)
		}
	})

	batch, err := platform.wait(nil, platformWake.descriptor(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if batch.woke {
		t.Fatal("fresh wake socketpair was readable")
	}
	if err := wake.Signal(); err != nil {
		t.Fatal(err)
	}
	if err := wake.Signal(); err != nil {
		t.Fatal(err)
	}
	batch, err = platform.wait(nil, platformWake.descriptor(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !batch.woke {
		t.Fatal("poll did not report the signaled wake socketpair")
	}
}

type fakeUnixWakeOps struct {
	failAt  string
	failure error
	created []int
	closed  []int
	writes  []error
}

func (fake *fakeUnixWakeOps) boundary() unixNetworkOps {
	setNonblockCalls := 0
	return unixNetworkOps{
		socketpair: func(int, int, int) ([2]int, error) {
			if fake.failAt == "socketpair" {
				return [2]int{}, fake.failure
			}
			pair := [2]int{101, 102}
			fake.created = append(fake.created, pair[:]...)
			return pair, nil
		},
		setNonblock: func(int, bool) error {
			setNonblockCalls++
			stage := "reader nonblock"
			if setNonblockCalls == 2 {
				stage = "writer nonblock"
			}
			if fake.failAt == stage {
				return fake.failure
			}
			return nil
		},
		write: func(int, []byte) (int, error) {
			if len(fake.writes) == 0 {
				return 1, nil
			}
			err := fake.writes[0]
			fake.writes = fake.writes[1:]
			if err != nil {
				return 0, err
			}
			return 1, nil
		},
		close: func(descriptor int) error {
			fake.closed = append(fake.closed, descriptor)
			return nil
		},
	}
}

func requireBalancedUnixWakeDescriptors(t *testing.T, fake *fakeUnixWakeOps) {
	t.Helper()
	if len(fake.created) != len(fake.closed) {
		t.Fatalf("created=%v closed=%v", fake.created, fake.closed)
	}
	balances := make(map[int]int, len(fake.created))
	for _, descriptor := range fake.created {
		balances[descriptor]++
	}
	for _, descriptor := range fake.closed {
		balances[descriptor]--
	}
	for descriptor, balance := range balances {
		if balance != 0 {
			t.Fatalf("descriptor %d balance=%d; created=%v closed=%v", descriptor, balance, fake.created, fake.closed)
		}
	}
}

func TestUnixWakeSetupBalancesDescriptorsAtEveryFailure(t *testing.T) {
	failure := errors.New("injected setup failure")
	for _, test := range []struct {
		stage       string
		wantCreated int
	}{
		{stage: "socketpair", wantCreated: 0},
		{stage: "reader nonblock", wantCreated: 2},
		{stage: "writer nonblock", wantCreated: 2},
	} {
		t.Run(test.stage, func(t *testing.T) {
			fake := &fakeUnixWakeOps{failAt: test.stage, failure: failure}
			wake, err := newUnixNetworkWake(fake.boundary())
			if wake != nil {
				t.Fatalf("wake=%#v, want nil", wake)
			}
			if !errors.Is(err, failure) {
				t.Fatalf("error=%v, want injected failure", err)
			}
			if len(fake.created) != test.wantCreated || len(fake.closed) != test.wantCreated {
				t.Fatalf("created=%v closed=%v, want %d each", fake.created, fake.closed, test.wantCreated)
			}
			requireBalancedUnixWakeDescriptors(t, fake)
		})
	}
}

func TestUnixWakeCloseIsIdempotentThroughCommonLifecycle(t *testing.T) {
	fake := &fakeUnixWakeOps{}
	platformWake, err := newUnixNetworkWake(fake.boundary())
	if err != nil {
		t.Fatal(err)
	}
	wake := newNetworkWake(platformWake)
	if err := wake.Close(); err != nil {
		t.Fatal(err)
	}
	if err := wake.Close(); err != nil {
		t.Fatal(err)
	}
	requireBalancedUnixWakeDescriptors(t, fake)
}

func TestUnixWakeSignalRetriesEINTRAndAcceptsWouldBlock(t *testing.T) {
	for _, test := range []struct {
		name   string
		writes []error
	}{
		{name: "EINTR retry", writes: []error{unix.EINTR, nil}},
		{name: "EAGAIN", writes: []error{unix.EAGAIN}},
		{name: "EWOULDBLOCK", writes: []error{unix.EWOULDBLOCK}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeUnixWakeOps{writes: append([]error(nil), test.writes...)}
			wake := &unixNetworkWake{writer: 102, ops: fake.boundary()}
			if err := wake.signal(); err != nil {
				t.Fatalf("signal error=%v", err)
			}
			if len(fake.writes) != 0 {
				t.Fatalf("unconsumed write results=%v", fake.writes)
			}
		})
	}
}

type unixWakeRaceTracker struct {
	mu         sync.Mutex
	closed     map[int]bool
	afterClose bool
}

func TestUnixWakeSignalCloseRace(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		ops := systemUnixNetworkOps()
		tracker := &unixWakeRaceTracker{closed: make(map[int]bool)}
		write := ops.write
		closeDescriptor := ops.close
		ops.write = func(descriptor int, payload []byte) (int, error) {
			tracker.mu.Lock()
			if tracker.closed[descriptor] {
				tracker.afterClose = true
			}
			tracker.mu.Unlock()
			return write(descriptor, payload)
		}
		ops.close = func(descriptor int) error {
			tracker.mu.Lock()
			tracker.closed[descriptor] = true
			tracker.mu.Unlock()
			return closeDescriptor(descriptor)
		}

		platformWake, err := newUnixNetworkWake(ops)
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		wake := newNetworkWake(platformWake)
		start := make(chan struct{})
		errorsOut := make(chan error, 2)
		go func() {
			<-start
			errorsOut <- wake.Signal()
		}()
		go func() {
			<-start
			errorsOut <- wake.Close()
		}()
		close(start)
		for call := 0; call < 2; call++ {
			if err := <-errorsOut; err != nil {
				t.Fatalf("iteration %d: signal/close error: %v", iteration, err)
			}
		}
		tracker.mu.Lock()
		afterClose := tracker.afterClose
		tracker.mu.Unlock()
		if afterClose {
			t.Fatalf("iteration %d: write called after descriptor close", iteration)
		}
	}
}
