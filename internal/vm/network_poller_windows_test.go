//go:build windows

package vm

import (
	"errors"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"noxy-vm/internal/value"
)

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
	if got&pollPRI != 0 {
		t.Fatalf("events=%#x include POLLPRI", got)
	}
	if got != pollRDNORM|pollWRNORM {
		t.Fatalf("events=%#x", got)
	}
}

func TestWindowsPollNormalizesTerminalEvents(t *testing.T) {
	got := normalizeWindowsPollEvents(pollHUP | pollERR)
	want := networkReadReady | networkWriteReady | networkErrorReady
	if got != want {
		t.Fatalf("events=%#x want=%#x", got, want)
	}
}

func TestWindowsPollEventMapping(t *testing.T) {
	tests := []struct {
		name string
		poll int16
		want networkEvent
	}{
		{name: "POLLRDNORM", poll: pollRDNORM, want: networkReadReady},
		{name: "POLLWRNORM", poll: pollWRNORM, want: networkWriteReady},
		{name: "POLLHUP", poll: pollHUP, want: networkReadReady | networkWriteReady | networkErrorReady},
		{name: "POLLERR", poll: pollERR, want: networkReadReady | networkWriteReady | networkErrorReady},
		{name: "POLLNVAL", poll: pollNVAL, want: networkReadReady | networkWriteReady | networkErrorReady},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeWindowsPollEvents(test.poll); got != test.want {
				t.Fatalf("events=%#x want=%#x", got, test.want)
			}
		})
	}
}

func TestWSAPollPreservesDescriptorOrderAndListenerInvariant(t *testing.T) {
	originalWSAPoll := callWSAPoll
	originalLastError := callWSAGetLastError
	t.Cleanup(func() {
		callWSAPoll = originalWSAPoll
		callWSAGetLastError = originalLastError
	})

	callWSAPoll = func(first *wsaPollFD, count uint32, timeout int32) (uintptr, uintptr, error) {
		if timeout != 37 {
			t.Fatalf("timeout=%d, want 37", timeout)
		}
		pollfds := unsafe.Slice(first, int(count))
		if len(pollfds) != 4 {
			t.Fatalf("descriptor count=%d, want 4", len(pollfds))
		}
		if pollfds[0].FD != 99 || pollfds[0].Events != pollRDNORM {
			t.Fatalf("wake descriptor=%+v", pollfds[0])
		}
		wantEvents := []int16{pollRDNORM, 0, pollRDNORM}
		for index, want := range wantEvents {
			if got := pollfds[index+1].Events; got != want {
				t.Fatalf("descriptor %d events=%#x, want %#x", index, got, want)
			}
		}
		pollfds[1].Revents = pollWRNORM
		pollfds[2].Revents = pollERR
		pollfds[3].Revents = pollWRNORM
		return 3, 0, syscall.Errno(1234)
	}
	callWSAGetLastError = func() (uintptr, uintptr, error) {
		t.Fatal("WSAGetLastError called after successful WSAPoll")
		return 0, 0, nil
	}

	descriptors := []networkPollFD{
		{descriptor: 11, interests: networkReadable},
		{descriptor: 12, interests: networkErrorInterest},
		{descriptor: 13, interests: networkReadable | networkWritable, listener: true},
	}
	batch, err := windowsNetworkWait(descriptors, 99, 37)
	if err != nil {
		t.Fatal(err)
	}
	if batch.interrupted {
		t.Fatal("Windows poll unexpectedly marked interrupted")
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

func TestWSAPollUsesExplicitWSAError(t *testing.T) {
	originalWSAPoll := callWSAPoll
	originalLastError := callWSAGetLastError
	t.Cleanup(func() {
		callWSAPoll = originalWSAPoll
		callWSAGetLastError = originalLastError
	})

	callWSAPoll = func(*wsaPollFD, uint32, int32) (uintptr, uintptr, error) {
		return ^uintptr(0), 0, syscall.Errno(5)
	}
	callWSAGetLastError = func() (uintptr, uintptr, error) {
		return uintptr(windows.WSAECONNREFUSED), 0, syscall.Errno(6)
	}

	_, err := windowsNetworkWait(nil, 99, 0)
	if !errors.Is(err, windows.WSAECONNREFUSED) {
		t.Fatalf("error=%v, want %v", err, windows.WSAECONNREFUSED)
	}
	if errors.Is(err, syscall.Errno(5)) || errors.Is(err, syscall.Errno(6)) {
		t.Fatalf("error=%v came from LazyProc.Call last-error", err)
	}
}

func TestWindowsWakeDescriptorAndSignal(t *testing.T) {
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

	if _, err := platform.wait(nil, platformWake.descriptor(), 0); err != nil {
		t.Fatalf("zero-time WSAPoll rejected wake descriptor: %v", err)
	}
	if err := wake.Signal(); err != nil {
		t.Fatal(err)
	}
	if err := wake.Signal(); err != nil {
		t.Fatal(err)
	}
	batch, err := platform.wait(nil, platformWake.descriptor(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !batch.woke {
		t.Fatal("WSAPoll did not report the signaled wake descriptor")
	}
}

type windowsWakeRaceTracker struct {
	mu         sync.Mutex
	closed     map[windows.Handle]bool
	afterClose bool
}

func TestWindowsWakeSignalCloseRace(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		ops := systemWindowsNetworkOps()
		tracker := &windowsWakeRaceTracker{closed: make(map[windows.Handle]bool)}
		sendto := ops.sendto
		closeSocket := ops.closeSocket
		ops.sendto = func(handle windows.Handle, payload []byte, flags int, address windows.Sockaddr) error {
			tracker.mu.Lock()
			if tracker.closed[handle] {
				tracker.afterClose = true
			}
			tracker.mu.Unlock()
			return sendto(handle, payload, flags, address)
		}
		ops.closeSocket = func(handle windows.Handle) error {
			tracker.mu.Lock()
			tracker.closed[handle] = true
			tracker.mu.Unlock()
			return closeSocket(handle)
		}

		platformWake, err := newWindowsNetworkWake(ops)
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
			t.Fatalf("iteration %d: Winsock send called after handle close", iteration)
		}
	}
}

type fakeWindowsWakeOps struct {
	failAt  string
	failure error
	created []windows.Handle
	closed  []windows.Handle
}

func requireBalancedWindowsWakeHandles(t *testing.T, created, closed []windows.Handle) {
	t.Helper()
	open := make(map[windows.Handle]int, len(created))
	for _, handle := range created {
		open[handle]++
	}
	for _, handle := range closed {
		open[handle]--
	}
	for handle, count := range open {
		if count != 0 {
			t.Fatalf("handle %d balance=%d; created=%v closed=%v", handle, count, created, closed)
		}
	}
	if len(created) != len(closed) {
		t.Fatalf("created=%v closed=%v", created, closed)
	}
}

func (fake *fakeWindowsWakeOps) boundary() windowsNetworkOps {
	socketCalls := 0
	return windowsNetworkOps{
		socket: func(int, int, int) (windows.Handle, error) {
			socketCalls++
			stage := "reader socket"
			if socketCalls == 2 {
				stage = "writer socket"
			}
			if fake.failAt == stage {
				return windows.InvalidHandle, fake.failure
			}
			handle := windows.Handle(100 + socketCalls)
			fake.created = append(fake.created, handle)
			return handle, nil
		},
		bind: func(windows.Handle, windows.Sockaddr) error {
			if fake.failAt == "bind" {
				return fake.failure
			}
			return nil
		},
		getsockname: func(windows.Handle) (windows.Sockaddr, error) {
			if fake.failAt == "getsockname" {
				return nil, fake.failure
			}
			return &windows.SockaddrInet4{Port: 4321, Addr: [4]byte{127, 0, 0, 1}}, nil
		},
		setNonblock: func(handle windows.Handle, _ bool) error {
			stage := "reader nonblock"
			if len(fake.created) == 2 && handle == fake.created[1] {
				stage = "writer nonblock"
			}
			if fake.failAt == stage {
				return fake.failure
			}
			return nil
		},
		sendto: func(windows.Handle, []byte, int, windows.Sockaddr) error { return nil },
		closeSocket: func(handle windows.Handle) error {
			fake.closed = append(fake.closed, handle)
			return nil
		},
	}
}

func TestWindowsWakeSetupBalancesHandlesAtEveryFailure(t *testing.T) {
	failure := errors.New("injected setup failure")
	tests := []struct {
		stage       string
		wantCreated int
	}{
		{stage: "reader socket", wantCreated: 0},
		{stage: "bind", wantCreated: 1},
		{stage: "getsockname", wantCreated: 1},
		{stage: "writer socket", wantCreated: 1},
		{stage: "reader nonblock", wantCreated: 2},
		{stage: "writer nonblock", wantCreated: 2},
	}
	for _, test := range tests {
		t.Run(test.stage, func(t *testing.T) {
			fake := &fakeWindowsWakeOps{failAt: test.stage, failure: failure}
			wake, err := newWindowsNetworkWake(fake.boundary())
			if wake != nil {
				t.Fatalf("wake=%#v, want nil", wake)
			}
			if !errors.Is(err, failure) {
				t.Fatalf("error=%v, want injected failure", err)
			}
			if len(fake.created) != test.wantCreated || len(fake.closed) != test.wantCreated {
				t.Fatalf("created=%v closed=%v, want %d each", fake.created, fake.closed, test.wantCreated)
			}
			requireBalancedWindowsWakeHandles(t, fake.created, fake.closed)
		})
	}
}

func TestWindowsWakeCloseBalancesSuccessfulSetup(t *testing.T) {
	fake := &fakeWindowsWakeOps{}
	wake, err := newWindowsNetworkWake(fake.boundary())
	if err != nil {
		t.Fatal(err)
	}
	if err := wake.close(); err != nil {
		t.Fatal(err)
	}
	if len(fake.created) != 2 || len(fake.closed) != 2 {
		t.Fatalf("created=%v closed=%v, want two each", fake.created, fake.closed)
	}
	requireBalancedWindowsWakeHandles(t, fake.created, fake.closed)
}

func TestWindowsWakeTreatsWouldBlockAsAlreadySignaled(t *testing.T) {
	fake := &fakeWindowsWakeOps{}
	ops := fake.boundary()
	ops.sendto = func(windows.Handle, []byte, int, windows.Sockaddr) error {
		return windows.WSAEWOULDBLOCK
	}
	wake, err := newWindowsNetworkWake(ops)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = wake.close() })
	if err := wake.signal(); err != nil {
		t.Fatalf("signal error=%v, want nil", err)
	}
}

func TestNetworkPollIntegrationWSAPollResetCopiesTerminalEventAndPreservesPendingError(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)

	clientHandle := int(builtinMapField(t, client, "fd").AsInt)
	clientResource, exists := machine.shared.Sockets.get(clientHandle)
	if !exists {
		t.Fatalf("client descriptor %d is not registered", clientHandle)
	}
	clientResource.stateMu.Lock()
	peer, ok := clientResource.connection.(*net.TCPConn)
	clientResource.stateMu.Unlock()
	if !ok {
		t.Fatalf("peer connection=%T, want *net.TCPConn", clientResource.connection)
	}
	if err := peer.SetLinger(0); err != nil {
		t.Fatal(err)
	}
	if err := peer.Close(); err != nil {
		t.Fatal(err)
	}

	selected := callBuiltinWithinBound(t, machine, "net_select",
		value.NewArray([]value.Value{server}),
		value.NewArray(nil),
		value.NewArray([]value.Value{server}),
		value.NewInt(1000))
	requireNetworkPollIntegrationSet(t, selected, "read", server)
	requireNetworkPollIntegrationSet(t, selected, "error", server)

	selected = callBuiltinWithinBound(t, machine, "net_select",
		value.NewArray([]value.Value{server}),
		value.NewArray([]value.Value{server}),
		value.NewArray([]value.Value{server}),
		value.NewInt(0))
	requireNetworkPollIntegrationSet(t, selected, "read", server)
	requireNetworkPollIntegrationSet(t, selected, "write", server)
	requireNetworkPollIntegrationSet(t, selected, "error", server)

	received := callBuiltinWithinBound(t, machine, "net_recv", server, value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, received, "ok"), value.NewBool(false))
	message := builtinMapField(t, received, "error").String()
	if message == "" || strings.Contains(strings.ToLower(message), "timeout") {
		t.Fatalf("net_recv error=%q, want non-timeout connection error", message)
	}
}
