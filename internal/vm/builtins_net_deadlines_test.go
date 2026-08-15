package vm

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"noxy-vm/internal/stdlib"
	"noxy-vm/internal/value"
)

type controlledDeadlineConn struct {
	net.Conn
	mu             sync.Mutex
	readDeadlines  []time.Time
	writeDeadlines []time.Time
	deadlines      []time.Time
	closeCount     int
	readFn         func([]byte) (int, error)
	writeFn        func([]byte) (int, error)
	setDeadlineFn  func(time.Time) error
	setReadFn      func(time.Time) error
	setWriteFn     func(time.Time) error
	closeFn        func() error
}

type controlledDeadlineListener struct {
	net.Listener
	mu         sync.Mutex
	deadlines  []time.Time
	closeCount int
	acceptFn   func() (net.Conn, error)
	setFn      func(time.Time) error
	closeFn    func() error
}

func (listener *controlledDeadlineListener) Accept() (net.Conn, error) {
	if listener.acceptFn != nil {
		return listener.acceptFn()
	}
	return listener.Listener.Accept()
}

func (listener *controlledDeadlineListener) SetDeadline(deadline time.Time) error {
	listener.mu.Lock()
	listener.deadlines = append(listener.deadlines, deadline)
	listener.mu.Unlock()
	if listener.setFn != nil {
		return listener.setFn(deadline)
	}
	return listener.Listener.(deadlineListener).SetDeadline(deadline)
}

func (listener *controlledDeadlineListener) Close() error {
	listener.mu.Lock()
	listener.closeCount++
	listener.mu.Unlock()
	if listener.closeFn != nil {
		return listener.closeFn()
	}
	return listener.Listener.Close()
}

func (listener *controlledDeadlineListener) closes() int {
	listener.mu.Lock()
	defer listener.mu.Unlock()
	return listener.closeCount
}

func (connection *controlledDeadlineConn) Read(buffer []byte) (int, error) {
	if connection.readFn != nil {
		return connection.readFn(buffer)
	}
	return connection.Conn.Read(buffer)
}

func (connection *controlledDeadlineConn) Write(buffer []byte) (int, error) {
	if connection.writeFn != nil {
		return connection.writeFn(buffer)
	}
	return connection.Conn.Write(buffer)
}

func (connection *controlledDeadlineConn) SetReadDeadline(deadline time.Time) error {
	connection.mu.Lock()
	connection.readDeadlines = append(connection.readDeadlines, deadline)
	connection.mu.Unlock()
	if connection.setReadFn != nil {
		return connection.setReadFn(deadline)
	}
	return connection.Conn.SetReadDeadline(deadline)
}

func (connection *controlledDeadlineConn) SetDeadline(deadline time.Time) error {
	connection.mu.Lock()
	connection.deadlines = append(connection.deadlines, deadline)
	connection.mu.Unlock()
	if connection.setDeadlineFn != nil {
		return connection.setDeadlineFn(deadline)
	}
	return connection.Conn.SetDeadline(deadline)
}

func (connection *controlledDeadlineConn) SetWriteDeadline(deadline time.Time) error {
	connection.mu.Lock()
	connection.writeDeadlines = append(connection.writeDeadlines, deadline)
	connection.mu.Unlock()
	if connection.setWriteFn != nil {
		return connection.setWriteFn(deadline)
	}
	return connection.Conn.SetWriteDeadline(deadline)
}

func (connection *controlledDeadlineConn) readDeadlineCount() int {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return len(connection.readDeadlines)
}

func (connection *controlledDeadlineConn) Close() error {
	connection.mu.Lock()
	connection.closeCount++
	connection.mu.Unlock()
	if connection.closeFn != nil {
		return connection.closeFn()
	}
	return connection.Conn.Close()
}

func (connection *controlledDeadlineConn) closes() int {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return connection.closeCount
}

func requireWakeSignals(t *testing.T, wakes ...*fakePlatformWake) {
	t.Helper()
	for index, wake := range wakes {
		wake.mu.Lock()
		signals := wake.signals
		wake.mu.Unlock()
		if signals != 1 {
			t.Fatalf("wake %d signals=%d, want 1 before close", index, signals)
		}
	}
}

func TestSocketDetachWakesAllWaitersAndClosesOnce(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	controlled := &controlledDeadlineConn{Conn: connection}
	firstRaw := &fakePlatformWake{}
	secondRaw := &fakePlatformWake{}
	first := newNetworkWake(firstRaw)
	second := newNetworkWake(secondRaw)
	resource := &SocketResource{
		connection:         controlled,
		deadlineGeneration: 7,
		pollWaiters:        map[*networkWake]struct{}{first: {}, second: {}},
	}

	closeSocket(resource)
	requireWakeSignals(t, firstRaw, secondRaw)
	resource.stateMu.Lock()
	closedAfterFirst := resource.closed
	generationAfterFirst := resource.deadlineGeneration
	connectionAfterFirst := resource.connection
	waitersAfterFirst := resource.pollWaiters
	resource.stateMu.Unlock()
	closesAfterFirst := controlled.closes()
	if !closedAfterFirst || generationAfterFirst != 8 || connectionAfterFirst != nil || waitersAfterFirst != nil {
		t.Fatalf("first close: closed=%v generation=%d connection=%v waiters=%v", closedAfterFirst, generationAfterFirst, connectionAfterFirst, waitersAfterFirst)
	}
	if closesAfterFirst != 1 {
		t.Fatalf("first close calls=%d, want 1", closesAfterFirst)
	}

	closeSocket(resource)
	requireWakeSignals(t, firstRaw, secondRaw)
	resource.stateMu.Lock()
	closedAfterSecond := resource.closed
	generationAfterSecond := resource.deadlineGeneration
	connectionAfterSecond := resource.connection
	waitersAfterSecond := resource.pollWaiters
	resource.stateMu.Unlock()
	if !closedAfterSecond || generationAfterSecond != generationAfterFirst || connectionAfterSecond != nil || waitersAfterSecond != nil {
		t.Fatalf("second close: closed=%v generation=%d connection=%v waiters=%v", closedAfterSecond, generationAfterSecond, connectionAfterSecond, waitersAfterSecond)
	}
	if controlled.closes() != closesAfterFirst {
		t.Fatalf("second close calls=%d, want unchanged %d", controlled.closes(), closesAfterFirst)
	}
}

func TestListenerDetachWakesAllWaitersAndClosesOnce(t *testing.T) {
	underlying, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	controlled := &controlledDeadlineListener{Listener: underlying}
	accepted, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	controlledAccepted := &controlledDeadlineConn{Conn: accepted}
	firstRaw := &fakePlatformWake{}
	secondRaw := &fakePlatformWake{}
	first := newNetworkWake(firstRaw)
	second := newNetworkWake(secondRaw)
	resource := &ListenerResource{
		listener:           controlled,
		bufferedAccept:     controlledAccepted,
		deadlineGeneration: 11,
		pollWaiters:        map[*networkWake]struct{}{first: {}, second: {}},
	}

	closeListener(resource)
	requireWakeSignals(t, firstRaw, secondRaw)
	resource.stateMu.Lock()
	closedAfterFirst := resource.closed
	generationAfterFirst := resource.deadlineGeneration
	listenerAfterFirst := resource.listener
	bufferedAfterFirst := resource.bufferedAccept
	waitersAfterFirst := resource.pollWaiters
	resource.stateMu.Unlock()
	listenerClosesAfterFirst := controlled.closes()
	bufferedClosesAfterFirst := controlledAccepted.closes()
	if !closedAfterFirst || generationAfterFirst != 12 || listenerAfterFirst != nil || bufferedAfterFirst != nil || waitersAfterFirst != nil {
		t.Fatalf("first close: closed=%v generation=%d listener=%v buffered=%v waiters=%v", closedAfterFirst, generationAfterFirst, listenerAfterFirst, bufferedAfterFirst, waitersAfterFirst)
	}
	if listenerClosesAfterFirst != 1 || bufferedClosesAfterFirst != 1 {
		t.Fatalf("first close calls: listener=%d accepted=%d, want 1 each", listenerClosesAfterFirst, bufferedClosesAfterFirst)
	}

	closeListener(resource)
	requireWakeSignals(t, firstRaw, secondRaw)
	resource.stateMu.Lock()
	closedAfterSecond := resource.closed
	generationAfterSecond := resource.deadlineGeneration
	listenerAfterSecond := resource.listener
	bufferedAfterSecond := resource.bufferedAccept
	waitersAfterSecond := resource.pollWaiters
	resource.stateMu.Unlock()
	if !closedAfterSecond || generationAfterSecond != generationAfterFirst || listenerAfterSecond != nil || bufferedAfterSecond != nil || waitersAfterSecond != nil {
		t.Fatalf("second close: closed=%v generation=%d listener=%v buffered=%v waiters=%v", closedAfterSecond, generationAfterSecond, listenerAfterSecond, bufferedAfterSecond, waitersAfterSecond)
	}
	if controlled.closes() != listenerClosesAfterFirst || controlledAccepted.closes() != bufferedClosesAfterFirst {
		t.Fatalf("second close calls: listener=%d accepted=%d, want unchanged %d and %d", controlled.closes(), controlledAccepted.closes(), listenerClosesAfterFirst, bufferedClosesAfterFirst)
	}
}

func TestSocketReadRollbackPoisonWakesBeforeCloseAndJoinsErrors(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	applicationFailure := errors.New("read deadline failed")
	rollbackFailure := errors.New("read rollback failed")
	signalFailure := errors.New("wake signal failed")
	closeFailure := errors.New("socket close failed")
	firstRaw := &fakePlatformWake{signalErr: signalFailure}
	secondRaw := &fakePlatformWake{}
	readCalls := 0
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setReadFn: func(time.Time) error {
			readCalls++
			if readCalls == 1 {
				return applicationFailure
			}
			return rollbackFailure
		},
		closeFn: func() error {
			requireWakeSignals(t, firstRaw, secondRaw)
			_ = connection.Close()
			return closeFailure
		},
	}
	resource := &SocketResource{
		connection: controlled,
		pollWaiters: map[*networkWake]struct{}{
			newNetworkWake(firstRaw):  {},
			newNetworkWake(secondRaw): {},
		},
	}

	err := configureSocketTimeout(resource, time.Second)
	for _, want := range []error{applicationFailure, rollbackFailure, signalFailure, closeFailure} {
		if !errors.Is(err, want) {
			t.Fatalf("configuration error=%v, want joined %v", err, want)
		}
	}
	resource.stateMu.Lock()
	closedAfterPoison := resource.closed
	generationAfterPoison := resource.deadlineGeneration
	connectionAfterPoison := resource.connection
	waitersAfterPoison := resource.pollWaiters
	resource.stateMu.Unlock()
	closesAfterPoison := controlled.closes()
	closeSocket(resource)
	requireWakeSignals(t, firstRaw, secondRaw)
	resource.stateMu.Lock()
	closedAfterClose := resource.closed
	generationAfterClose := resource.deadlineGeneration
	connectionAfterClose := resource.connection
	waitersAfterClose := resource.pollWaiters
	resource.stateMu.Unlock()
	if !closedAfterPoison || !closedAfterClose || connectionAfterPoison != nil || connectionAfterClose != nil || waitersAfterPoison != nil || waitersAfterClose != nil {
		t.Fatalf("closed before/after=%v/%v connection before/after=%v/%v waiters before/after=%v/%v", closedAfterPoison, closedAfterClose, connectionAfterPoison, connectionAfterClose, waitersAfterPoison, waitersAfterClose)
	}
	if generationAfterClose != generationAfterPoison {
		t.Fatalf("generation after close=%d, want unchanged %d", generationAfterClose, generationAfterPoison)
	}
	if closesAfterPoison != 1 || controlled.closes() != closesAfterPoison {
		t.Fatalf("close calls before/after=%d/%d, want 1/1", closesAfterPoison, controlled.closes())
	}
}

func TestSocketWriteRollbackPoisonWakesBeforeCloseAndJoinsErrors(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	applicationFailure := errors.New("write deadline failed")
	rollbackFailure := errors.New("read rollback failed")
	signalFailure := errors.New("wake signal failed")
	closeFailure := errors.New("socket close failed")
	firstRaw := &fakePlatformWake{signalErr: signalFailure}
	secondRaw := &fakePlatformWake{}
	readCalls := 0
	writeCalls := 0
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setReadFn: func(time.Time) error {
			readCalls++
			if readCalls == 2 {
				return rollbackFailure
			}
			return nil
		},
		setWriteFn: func(time.Time) error {
			writeCalls++
			if writeCalls == 1 {
				return applicationFailure
			}
			return nil
		},
		closeFn: func() error {
			requireWakeSignals(t, firstRaw, secondRaw)
			_ = connection.Close()
			return closeFailure
		},
	}
	resource := &SocketResource{
		connection: controlled,
		pollWaiters: map[*networkWake]struct{}{
			newNetworkWake(firstRaw):  {},
			newNetworkWake(secondRaw): {},
		},
	}

	err := configureSocketTimeout(resource, time.Second)
	for _, want := range []error{applicationFailure, rollbackFailure, signalFailure, closeFailure} {
		if !errors.Is(err, want) {
			t.Fatalf("configuration error=%v, want joined %v", err, want)
		}
	}
	resource.stateMu.Lock()
	closedAfterPoison := resource.closed
	generationAfterPoison := resource.deadlineGeneration
	connectionAfterPoison := resource.connection
	waitersAfterPoison := resource.pollWaiters
	resource.stateMu.Unlock()
	closesAfterPoison := controlled.closes()
	closeSocket(resource)
	requireWakeSignals(t, firstRaw, secondRaw)
	resource.stateMu.Lock()
	closedAfterClose := resource.closed
	generationAfterClose := resource.deadlineGeneration
	connectionAfterClose := resource.connection
	waitersAfterClose := resource.pollWaiters
	resource.stateMu.Unlock()
	if !closedAfterPoison || !closedAfterClose || connectionAfterPoison != nil || connectionAfterClose != nil || waitersAfterPoison != nil || waitersAfterClose != nil {
		t.Fatalf("closed before/after=%v/%v connection before/after=%v/%v waiters before/after=%v/%v", closedAfterPoison, closedAfterClose, connectionAfterPoison, connectionAfterClose, waitersAfterPoison, waitersAfterClose)
	}
	if generationAfterClose != generationAfterPoison {
		t.Fatalf("generation after close=%d, want unchanged %d", generationAfterClose, generationAfterPoison)
	}
	if closesAfterPoison != 1 || controlled.closes() != closesAfterPoison {
		t.Fatalf("close calls before/after=%d/%d, want 1/1", closesAfterPoison, controlled.closes())
	}
}

func TestListenerRollbackPoisonWakesBeforeCloseAndJoinsErrors(t *testing.T) {
	underlying, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	applicationFailure := errors.New("accept deadline failed")
	rollbackFailure := errors.New("accept rollback failed")
	signalFailure := errors.New("wake signal failed")
	closeFailure := errors.New("listener close failed")
	firstRaw := &fakePlatformWake{signalErr: signalFailure}
	secondRaw := &fakePlatformWake{}
	setterCalls := 0
	controlled := &controlledDeadlineListener{
		Listener: underlying,
		setFn: func(time.Time) error {
			setterCalls++
			if setterCalls == 1 {
				return applicationFailure
			}
			return rollbackFailure
		},
		closeFn: func() error {
			requireWakeSignals(t, firstRaw, secondRaw)
			_ = underlying.Close()
			return closeFailure
		},
	}
	accepted, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	controlledAccepted := &controlledDeadlineConn{Conn: accepted}
	resource := &ListenerResource{
		listener:       controlled,
		bufferedAccept: controlledAccepted,
		pollWaiters: map[*networkWake]struct{}{
			newNetworkWake(firstRaw):  {},
			newNetworkWake(secondRaw): {},
		},
	}

	err = configureListenerTimeout(resource, time.Second)
	for _, want := range []error{applicationFailure, rollbackFailure, signalFailure, closeFailure} {
		if !errors.Is(err, want) {
			t.Fatalf("configuration error=%v, want joined %v", err, want)
		}
	}
	resource.stateMu.Lock()
	closedAfterPoison := resource.closed
	generationAfterPoison := resource.deadlineGeneration
	listenerAfterPoison := resource.listener
	bufferedAfterPoison := resource.bufferedAccept
	waitersAfterPoison := resource.pollWaiters
	resource.stateMu.Unlock()
	listenerClosesAfterPoison := controlled.closes()
	bufferedClosesAfterPoison := controlledAccepted.closes()
	closeListener(resource)
	requireWakeSignals(t, firstRaw, secondRaw)
	resource.stateMu.Lock()
	closedAfterClose := resource.closed
	generationAfterClose := resource.deadlineGeneration
	listenerAfterClose := resource.listener
	bufferedAfterClose := resource.bufferedAccept
	waitersAfterClose := resource.pollWaiters
	resource.stateMu.Unlock()
	if !closedAfterPoison || !closedAfterClose || listenerAfterPoison != nil || listenerAfterClose != nil || bufferedAfterPoison != nil || bufferedAfterClose != nil || waitersAfterPoison != nil || waitersAfterClose != nil {
		t.Fatalf("closed before/after=%v/%v listener before/after=%v/%v buffered before/after=%v/%v waiters before/after=%v/%v", closedAfterPoison, closedAfterClose, listenerAfterPoison, listenerAfterClose, bufferedAfterPoison, bufferedAfterClose, waitersAfterPoison, waitersAfterClose)
	}
	if generationAfterClose != generationAfterPoison {
		t.Fatalf("generation after close=%d, want unchanged %d", generationAfterClose, generationAfterPoison)
	}
	if listenerClosesAfterPoison != 1 || bufferedClosesAfterPoison != 1 || controlled.closes() != listenerClosesAfterPoison || controlledAccepted.closes() != bufferedClosesAfterPoison {
		t.Fatalf("close calls before/after: listener=%d/%d accepted=%d/%d, want 1/1 each", listenerClosesAfterPoison, controlled.closes(), bufferedClosesAfterPoison, controlledAccepted.closes())
	}
}

func invokeBuiltin(t *testing.T, machine *VM, name string, args ...value.Value) (value.Value, error) {
	t.Helper()
	return requireBuiltin(t, machine, name).Invoke(machine, args)
}

func TestValidateNetworkTimeout(t *testing.T) {
	maximum := int64(math.MaxInt64) / int64(time.Millisecond)
	tests := []struct {
		name         string
		milliseconds int64
		want         time.Duration
		wantError    string
	}{
		{name: "one millisecond", milliseconds: 1, want: time.Millisecond},
		{name: "maximum", milliseconds: maximum, want: time.Duration(maximum) * time.Millisecond},
		{name: "zero", milliseconds: 0, wantError: "network timeout must be positive"},
		{name: "negative", milliseconds: -1, wantError: "network timeout must be positive"},
		{name: "overflow", milliseconds: maximum + 1, wantError: "network timeout is too large"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateNetworkTimeout(test.milliseconds)
			if test.wantError != "" {
				if err == nil || err.Error() != test.wantError {
					t.Fatalf("error=%v, want %q", err, test.wantError)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("duration=%v, error=%v, want %v", got, err, test.want)
			}
		})
	}
}

func TestNetworkTimeoutStateIsShared(t *testing.T) {
	parent := New()
	child := NewWithShared(parent.shared, parent.Config)
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	resource := &SocketResource{connection: connection}
	handle := parent.shared.Sockets.add(resource)
	t.Cleanup(func() { parent.shared.Sockets.remove(handle) })
	socket := socketValue(handle, "pipe", 0, true)

	result, err := invokeBuiltin(t, child, "net_settimeout", socket, value.NewInt(25))
	if err != nil {
		t.Fatalf("net_settimeout: %v", err)
	}
	assertBuiltinValue(t, result, value.NewNull())

	resource.stateMu.Lock()
	got := resource.ioTimeout
	resource.stateMu.Unlock()
	if got != 25*time.Millisecond {
		t.Fatalf("shared ioTimeout=%v, want 25ms", got)
	}
}

func TestNetSetTimeoutRejectsInvalidValuesBeforeSocketLookup(t *testing.T) {
	machine := New()
	malformedSocket := value.NewString("not a socket")

	tests := []struct {
		name    string
		args    []value.Value
		message string
	}{
		{name: "arity", args: []value.Value{malformedSocket}, message: "net_settimeout expects exactly 2 arguments"},
		{name: "timeout type", args: []value.Value{malformedSocket, value.NewString("1")}, message: "network timeout must be an int"},
		{name: "timeout value", args: []value.Value{malformedSocket, value.NewInt(0)}, message: "network timeout must be positive"},
		{name: "socket type", args: []value.Value{malformedSocket, value.NewInt(1)}, message: "invalid socket"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := invokeBuiltin(t, machine, "net_settimeout", test.args...)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error=%v, want substring %q", err, test.message)
			}
		})
	}
}

func TestNetSetBlockingFalseIsCompatibilityNoOp(t *testing.T) {
	machine := New()
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	resource := &SocketResource{connection: connection, ioTimeout: 40 * time.Millisecond}
	handle := machine.shared.Sockets.add(resource)
	t.Cleanup(func() { machine.shared.Sockets.remove(handle) })

	for _, socket := range []value.Value{
		socketValue(handle, "pipe", 0, true),
		value.NewString("malformed"),
		socketValue(handle+1000, "stale", 0, false),
	} {
		result, err := invokeBuiltin(t, machine, "net_setblocking", socket, value.NewBool(false))
		if err != nil {
			t.Fatalf("net_setblocking(false): %v", err)
		}
		assertBuiltinValue(t, result, value.NewNull())
	}

	resource.stateMu.Lock()
	got := resource.ioTimeout
	resource.stateMu.Unlock()
	if got != 40*time.Millisecond {
		t.Fatalf("ioTimeout=%v, want unchanged 40ms", got)
	}
}

func TestSocketReadTimeoutIsRefreshedPerOperation(t *testing.T) {
	machine := New()
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	resource := &SocketResource{connection: connection}
	handle := machine.shared.Sockets.add(resource)
	t.Cleanup(func() { machine.shared.Sockets.remove(handle) })
	socket := socketValue(handle, "pipe", 0, true)

	if _, err := invokeBuiltin(t, machine, "net_settimeout", socket, value.NewInt(30)); err != nil {
		t.Fatalf("net_settimeout: %v", err)
	}
	time.Sleep(45 * time.Millisecond)
	started := time.Now()
	result := receiveSocket(resource, 1)
	elapsed := time.Since(started)

	assertBuiltinValue(t, builtinMapField(t, result, "ok"), value.NewBool(false))
	assertBuiltinValue(t, builtinMapField(t, result, "error"), value.NewString("operation timed out"))
	if elapsed < 15*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("receive elapsed=%v, want a freshly bounded operation", elapsed)
	}
}

func TestReceiveSocketInstallsDeadlineBeforeRead(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		readFn: func([]byte) (int, error) {
			return 0, io.EOF
		},
	}
	resource := &SocketResource{connection: controlled, ioTimeout: 50 * time.Millisecond}
	result := receiveSocket(resource, 1)

	assertBuiltinValue(t, builtinMapField(t, result, "ok"), value.NewBool(true))
	if controlled.readDeadlineCount() != 1 {
		t.Fatalf("read deadline calls=%d, want 1", controlled.readDeadlineCount())
	}
}

func TestReceiveSocketFullyBufferedSkipsDeadlineSetter(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setReadFn: func(time.Time) error {
			return errors.New("unexpected deadline setter")
		},
	}
	resource := &SocketResource{
		connection:   controlled,
		bufferedRead: []byte("x"),
		ioTimeout:    50 * time.Millisecond,
	}
	result := receiveSocket(resource, 1)

	assertBuiltinValue(t, builtinMapField(t, result, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, result, "data"), value.NewBytes("x"))
	if controlled.readDeadlineCount() != 0 {
		t.Fatalf("read deadline calls=%d, want 0", controlled.readDeadlineCount())
	}
}

func TestSendSocketReportsPartialTimeout(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		writeFn: func([]byte) (int, error) {
			return 2, fmt.Errorf("wrapped: %w", os.ErrDeadlineExceeded)
		},
	}
	resource := &SocketResource{connection: controlled, ioTimeout: 50 * time.Millisecond}
	result := sendSocket(resource, "data")

	assertBuiltinValue(t, builtinMapField(t, result, "ok"), value.NewBool(false))
	assertBuiltinValue(t, builtinMapField(t, result, "count"), value.NewInt(2))
	assertBuiltinValue(t, builtinMapField(t, result, "error"), value.NewString("operation timed out"))
}

func TestNetworkErrorMessageUsesErrorsIs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", os.ErrDeadlineExceeded)
	if got := networkErrorMessage(err); got != "operation timed out" {
		t.Fatalf("message=%q, want operation timed out", got)
	}
}

func TestListenerAcceptTimeoutIsRefreshedPerOperation(t *testing.T) {
	machine := New()
	cleanupNetworkResources(t, machine)
	listener := callBuiltinWithinBound(t, machine, "net_listen", value.NewString("127.0.0.1"), value.NewInt(0))
	if _, err := invokeBuiltin(t, machine, "net_settimeout", listener, value.NewInt(30)); err != nil {
		t.Fatalf("net_settimeout: %v", err)
	}
	time.Sleep(45 * time.Millisecond)
	started := time.Now()
	accepted := callBuiltinWithinBound(t, machine, "net_accept", listener)
	elapsed := time.Since(started)

	assertBuiltinValue(t, builtinMapField(t, accepted, "open"), value.NewBool(false))
	if elapsed < 15*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("accept elapsed=%v, want a freshly bounded operation", elapsed)
	}
}

func TestCloseDoesNotWaitForBlockedDeadlineSetter(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	setterEntered := make(chan struct{})
	releaseSetter := make(chan struct{})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setReadFn: func(time.Time) error {
			close(setterEntered)
			<-releaseSetter
			return nil
		},
	}
	resource := &SocketResource{connection: controlled}
	configured := make(chan error, 1)
	go func() { configured <- configureSocketTimeout(resource, 20*time.Millisecond) }()
	select {
	case <-setterEntered:
	case <-time.After(time.Second):
		t.Fatal("deadline setter was not entered")
	}

	closed := make(chan struct{})
	go func() {
		closeSocket(resource)
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("close waited for deadline setter")
	}
	if controlled.closes() != 1 {
		t.Fatalf("close calls=%d, want 1", controlled.closes())
	}

	close(releaseSetter)
	select {
	case err := <-configured:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("configuration error=%v, want net.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("configuration did not finish after setter release")
	}
	if controlled.closes() != 1 {
		t.Fatalf("close calls after stale setter=%d, want 1", controlled.closes())
	}
}

func TestBufferedAcceptCloseWinsDuringDeadlineClear(t *testing.T) {
	for _, setterError := range []error{nil, errors.New("clear failed")} {
		name := "setter success"
		if setterError != nil {
			name = "setter failure"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			deadlineCapable := listener.(deadlineListener)
			connection, peer := net.Pipe()
			t.Cleanup(func() { _ = peer.Close() })
			setterEntered := make(chan struct{})
			releaseSetter := make(chan struct{})
			controlled := &controlledDeadlineConn{
				Conn: connection,
				setDeadlineFn: func(time.Time) error {
					close(setterEntered)
					<-releaseSetter
					return setterError
				},
			}
			resource := &ListenerResource{listener: deadlineCapable, bufferedAccept: controlled}
			t.Cleanup(func() {
				_ = listener.Close()
				_ = connection.Close()
			})
			accepted := make(chan struct {
				connection net.Conn
				err        error
			}, 1)
			go func() {
				connection, err := acceptConnection(resource)
				accepted <- struct {
					connection net.Conn
					err        error
				}{connection: connection, err: err}
			}()
			select {
			case <-setterEntered:
			case <-time.After(time.Second):
				t.Fatal("accepted connection deadline clear was not entered")
			}

			closeListener(resource)
			if controlled.closes() != 1 {
				t.Fatalf("close calls=%d, want listener close ownership", controlled.closes())
			}
			close(releaseSetter)
			select {
			case result := <-accepted:
				if result.connection != nil || result.err == nil {
					t.Fatalf("accept=(%v, %v), want nil connection and error", result.connection, result.err)
				}
			case <-time.After(time.Second):
				t.Fatal("accept did not finish")
			}
			if controlled.closes() != 1 {
				t.Fatalf("close calls after accept=%d, want 1", controlled.closes())
			}
		})
	}
}

func TestNetSelectRestoresPersistentSocketDeadline(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		readFn: func(buffer []byte) (int, error) {
			buffer[0] = 'x'
			return 1, nil
		},
	}
	resource := &SocketResource{connection: controlled, ioTimeout: 100 * time.Millisecond}
	started := time.Now()
	ready, err := selectSocket(resource, 10*time.Millisecond)
	if err != nil || !ready {
		t.Fatalf("selectSocket=(%v, %v), want ready", ready, err)
	}

	controlled.mu.Lock()
	deadlines := append([]time.Time(nil), controlled.readDeadlines...)
	controlled.mu.Unlock()
	if len(deadlines) != 2 {
		t.Fatalf("read deadline calls=%d, want probe and restoration", len(deadlines))
	}
	if deadlines[0].Before(started.Add(5*time.Millisecond)) || deadlines[0].After(started.Add(50*time.Millisecond)) {
		t.Fatalf("probe deadline=%v, want select bound", deadlines[0])
	}
	if deadlines[1].IsZero() || !deadlines[1].After(deadlines[0]) {
		t.Fatalf("restored deadline=%v, want fresh persistent deadline after %v", deadlines[1], deadlines[0])
	}
	resource.stateMu.Lock()
	buffered := string(resource.bufferedRead)
	resource.stateMu.Unlock()
	if buffered != "x" {
		t.Fatalf("buffered read=%q, want x", buffered)
	}
}

func TestNetSelectRestorationFailureKeepsReadyByte(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	restoreFailure := errors.New("restore failed")
	setterCalls := 0
	controlled := &controlledDeadlineConn{
		Conn: connection,
		readFn: func(buffer []byte) (int, error) {
			buffer[0] = 'x'
			return 1, nil
		},
		setReadFn: func(time.Time) error {
			setterCalls++
			if setterCalls == 2 {
				return restoreFailure
			}
			return nil
		},
	}
	resource := &SocketResource{connection: controlled, ioTimeout: 100 * time.Millisecond}
	ready, err := selectSocket(resource, 10*time.Millisecond)
	if ready || !errors.Is(err, restoreFailure) {
		t.Fatalf("selectSocket=(%v, %v), want restoration failure", ready, err)
	}
	resource.stateMu.Lock()
	buffered := string(resource.bufferedRead)
	resource.stateMu.Unlock()
	if buffered != "x" {
		t.Fatalf("buffered read=%q, want x after restoration failure", buffered)
	}
}

func TestSocketConfigurationRollsBackExactDeadlines(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	writeFailure := errors.New("write deadline failed")
	writeCalls := 0
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setWriteFn: func(time.Time) error {
			writeCalls++
			if writeCalls == 1 {
				return writeFailure
			}
			return nil
		},
	}
	oldRead := time.Now().Add(2 * time.Second).Round(0)
	oldWrite := time.Now().Add(3 * time.Second).Round(0)
	resource := &SocketResource{
		connection:        controlled,
		ioTimeout:         10 * time.Second,
		lastReadDeadline:  oldRead,
		lastWriteDeadline: oldWrite,
	}
	err := configureSocketTimeout(resource, 5*time.Second)
	if !errors.Is(err, writeFailure) {
		t.Fatalf("configuration error=%v, want write failure", err)
	}

	controlled.mu.Lock()
	readDeadlines := append([]time.Time(nil), controlled.readDeadlines...)
	writeDeadlines := append([]time.Time(nil), controlled.writeDeadlines...)
	controlled.mu.Unlock()
	if len(readDeadlines) != 2 || !readDeadlines[1].Equal(oldRead) {
		t.Fatalf("read rollback=%v, want exact %v", readDeadlines, oldRead)
	}
	if len(writeDeadlines) != 2 || !writeDeadlines[1].Equal(oldWrite) {
		t.Fatalf("write rollback=%v, want exact %v", writeDeadlines, oldWrite)
	}
	resource.stateMu.Lock()
	timeout := resource.ioTimeout
	resource.stateMu.Unlock()
	if timeout != 10*time.Second {
		t.Fatalf("ioTimeout=%v, want unchanged 10s", timeout)
	}
}

func TestSocketRollbackFailurePoisonsAndCloses(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	applicationFailure := errors.New("write deadline failed")
	rollbackFailure := errors.New("read rollback failed")
	readCalls := 0
	writeCalls := 0
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setReadFn: func(time.Time) error {
			readCalls++
			if readCalls == 2 {
				return rollbackFailure
			}
			return nil
		},
		setWriteFn: func(time.Time) error {
			writeCalls++
			if writeCalls == 1 {
				return applicationFailure
			}
			return nil
		},
	}
	resource := &SocketResource{
		connection:        controlled,
		ioTimeout:         10 * time.Second,
		lastReadDeadline:  time.Now().Add(2 * time.Second),
		lastWriteDeadline: time.Now().Add(3 * time.Second),
		bufferedRead:      []byte("x"),
	}
	err := configureSocketTimeout(resource, 5*time.Second)
	if !errors.Is(err, applicationFailure) || !errors.Is(err, rollbackFailure) {
		t.Fatalf("configuration error=%v, want application and rollback failures", err)
	}
	resource.stateMu.Lock()
	closed := resource.closed
	underlying := resource.connection
	buffered := len(resource.bufferedRead)
	resource.stateMu.Unlock()
	if !closed || underlying != nil || buffered != 0 {
		t.Fatalf("poisoned state: closed=%v connection=%v buffered=%d", closed, underlying, buffered)
	}
	if controlled.closes() != 1 {
		t.Fatalf("close calls=%d, want 1", controlled.closes())
	}
}

func TestListenerConfigurationRollsBackExactDeadline(t *testing.T) {
	underlying, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = underlying.Close() })
	applicationFailure := errors.New("accept deadline failed")
	setterCalls := 0
	controlled := &controlledDeadlineListener{
		Listener: underlying,
		setFn: func(time.Time) error {
			setterCalls++
			if setterCalls == 1 {
				return applicationFailure
			}
			return nil
		},
	}
	oldDeadline := time.Now().Add(2 * time.Second).Round(0)
	resource := &ListenerResource{
		listener:           controlled,
		ioTimeout:          10 * time.Second,
		lastAcceptDeadline: oldDeadline,
	}
	err = configureListenerTimeout(resource, 5*time.Second)
	if !errors.Is(err, applicationFailure) {
		t.Fatalf("configuration error=%v, want application failure", err)
	}
	controlled.mu.Lock()
	deadlines := append([]time.Time(nil), controlled.deadlines...)
	controlled.mu.Unlock()
	if len(deadlines) != 2 || !deadlines[1].Equal(oldDeadline) {
		t.Fatalf("listener rollback=%v, want exact %v", deadlines, oldDeadline)
	}
}

func TestListenerRollbackFailurePoisonsOwnedResources(t *testing.T) {
	underlying, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	applicationFailure := errors.New("accept deadline failed")
	rollbackFailure := errors.New("accept rollback failed")
	setterCalls := 0
	controlledListener := &controlledDeadlineListener{
		Listener: underlying,
		setFn: func(time.Time) error {
			setterCalls++
			if setterCalls == 1 {
				return applicationFailure
			}
			return rollbackFailure
		},
	}
	accepted, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	controlledAccepted := &controlledDeadlineConn{Conn: accepted}
	resource := &ListenerResource{
		listener:           controlledListener,
		bufferedAccept:     controlledAccepted,
		ioTimeout:          10 * time.Second,
		lastAcceptDeadline: time.Now().Add(2 * time.Second),
	}
	err = configureListenerTimeout(resource, 5*time.Second)
	if !errors.Is(err, applicationFailure) || !errors.Is(err, rollbackFailure) {
		t.Fatalf("configuration error=%v, want application and rollback failures", err)
	}
	resource.stateMu.Lock()
	closed := resource.closed
	listener := resource.listener
	buffered := resource.bufferedAccept
	resource.stateMu.Unlock()
	if !closed || listener != nil || buffered != nil {
		t.Fatalf("poisoned listener: closed=%v listener=%v buffered=%v", closed, listener, buffered)
	}
	if controlledListener.closes() != 1 || controlledAccepted.closes() != 1 {
		t.Fatalf("close calls: listener=%d accepted=%d, want 1 each", controlledListener.closes(), controlledAccepted.closes())
	}
}

func TestNetTimeoutWrapper(t *testing.T) {
	machine := NewWithConfig(VMConfig{RootPath: "."})
	content, err := stdlib.FS.ReadFile("net.nx")
	if err != nil {
		t.Fatal(err)
	}
	module, err := machine.loadResolvedModule(resolvedModule{
		Key:     moduleKey{Root: "embedded", Name: "net"},
		Name:    "net",
		Kind:    resolvedEmbeddedModule,
		Content: string(content),
	})
	if err != nil {
		t.Fatalf("load embedded net module: %v", err)
	}
	mapping := requireBuiltinMap(t, module)
	exported, exists := mapping.Get("settimeout")
	if !exists || exported.Type != value.VAL_FUNCTION {
		t.Fatalf("net.settimeout export=%v, exists=%v", exported, exists)
	}
}

func TestConnectedSocketClearsDeadlineBeforeRegistration(t *testing.T) {
	originalDial := networkDialTimeout
	t.Cleanup(func() { networkDialTimeout = originalDial })

	for _, clearErr := range []error{nil, errors.New("clear failed")} {
		name := "success"
		if clearErr != nil {
			name = "clear failure"
		}
		t.Run(name, func(t *testing.T) {
			machine := New()
			connection, peer := net.Pipe()
			t.Cleanup(func() { _ = peer.Close() })
			controlled := &controlledDeadlineConn{
				Conn: connection,
				setDeadlineFn: func(time.Time) error {
					return clearErr
				},
			}
			networkDialTimeout = func(string, string, time.Duration) (net.Conn, error) {
				return controlled, nil
			}

			result := callBuiltinWithinBound(t, machine, "net_connect", value.NewString("example.test"), value.NewInt(80))
			controlled.mu.Lock()
			deadlines := append([]time.Time(nil), controlled.deadlines...)
			controlled.mu.Unlock()
			if len(deadlines) != 1 || !deadlines[0].IsZero() {
				t.Fatalf("connection deadlines=%v, want one zero clear", deadlines)
			}
			if clearErr == nil {
				assertBuiltinValue(t, builtinMapField(t, result, "open"), value.NewBool(true))
				callBuiltinWithinBound(t, machine, "net_close", result)
				return
			}
			assertBuiltinValue(t, builtinMapField(t, result, "open"), value.NewBool(false))
			if len(machine.shared.Sockets.snapshot()) != 0 {
				t.Fatal("connection with failed deadline clear was registered")
			}
			if controlled.closes() != 1 {
				t.Fatalf("close calls=%d, want 1", controlled.closes())
			}
		})
	}
}

func TestPendingReadObservesTimeoutConfiguration(t *testing.T) {
	machine := New()
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	readEntered := make(chan struct{})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		readFn: func(buffer []byte) (int, error) {
			close(readEntered)
			return connection.Read(buffer)
		},
	}
	resource := &SocketResource{connection: controlled}
	handle := machine.shared.Sockets.add(resource)
	t.Cleanup(func() { machine.shared.Sockets.remove(handle) })
	result := make(chan value.Value, 1)
	go func() { result <- receiveSocket(resource, 1) }()
	select {
	case <-readEntered:
	case <-time.After(time.Second):
		t.Fatal("pending read did not start")
	}
	if _, err := invokeBuiltin(t, machine, "net_settimeout", socketValue(handle, "pipe", 0, true), value.NewInt(25)); err != nil {
		t.Fatalf("net_settimeout: %v", err)
	}
	select {
	case got := <-result:
		assertBuiltinValue(t, builtinMapField(t, got, "error"), value.NewString("operation timed out"))
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pending read did not observe configured timeout")
	}
}

func TestPendingReadObservesBlockingConfiguration(t *testing.T) {
	machine := New()
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	readEntered := make(chan struct{})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		readFn: func(buffer []byte) (int, error) {
			close(readEntered)
			return connection.Read(buffer)
		},
	}
	resource := &SocketResource{connection: controlled, ioTimeout: 30 * time.Millisecond}
	handle := machine.shared.Sockets.add(resource)
	t.Cleanup(func() { machine.shared.Sockets.remove(handle) })
	socket := socketValue(handle, "pipe", 0, true)
	result := make(chan value.Value, 1)
	go func() { result <- receiveSocket(resource, 1) }()
	select {
	case <-readEntered:
	case <-time.After(time.Second):
		t.Fatal("pending read did not start")
	}
	if _, err := invokeBuiltin(t, machine, "net_setblocking", socket, value.NewBool(true)); err != nil {
		t.Fatalf("net_setblocking(true): %v", err)
	}
	time.Sleep(45 * time.Millisecond)
	if _, err := peer.Write([]byte("x")); err != nil {
		t.Fatalf("write after clearing deadline: %v", err)
	}
	select {
	case got := <-result:
		assertBuiltinValue(t, builtinMapField(t, got, "data"), value.NewBytes("x"))
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pending read was not restored to blocking")
	}
}

func TestNetSetTimeoutAcceptsTypedSocketAndRejectsTypedNil(t *testing.T) {
	machine := New()
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	resource := &SocketResource{connection: connection}
	handle := machine.shared.Sockets.add(resource)
	t.Cleanup(func() { machine.shared.Sockets.remove(handle) })
	definition := &value.ObjStruct{Name: "Socket", Fields: []string{"fd", "addr", "port", "open"}}
	instance := value.NewInstance(definition)
	instance.Obj.(*value.ObjInstance).Fields["fd"] = value.NewInt(int64(handle))
	if _, err := invokeBuiltin(t, machine, "net_settimeout", instance, value.NewInt(10)); err != nil {
		t.Fatalf("typed socket: %v", err)
	}

	invalid := []value.Value{
		{Type: value.VAL_OBJ, Obj: (*value.ObjMap)(nil)},
		{Type: value.VAL_OBJ, Obj: (*value.ObjInstance)(nil)},
		value.NewMap(),
		value.NewMapWithData(map[string]value.Value{"fd": value.NewString("1")}),
	}
	for _, socket := range invalid {
		if _, err := invokeBuiltin(t, machine, "net_settimeout", socket, value.NewInt(10)); err == nil || !strings.Contains(err.Error(), "invalid socket") {
			t.Fatalf("invalid socket %#v produced error %v", socket, err)
		}
	}
}

func TestNetworkProbeDeadlineUsesOneStartInstant(t *testing.T) {
	started := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		selectTimeout time.Duration
		ioTimeout     time.Duration
		want          time.Time
	}{
		{name: "persistent shorter", selectTimeout: 10 * time.Millisecond, ioTimeout: 9 * time.Millisecond, want: started.Add(9 * time.Millisecond)},
		{name: "select shorter", selectTimeout: 9 * time.Millisecond, ioTimeout: 10 * time.Millisecond, want: started.Add(9 * time.Millisecond)},
		{name: "blocking mode", selectTimeout: 9 * time.Millisecond, ioTimeout: 0, want: started.Add(9 * time.Millisecond)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := networkProbeDeadline(started, test.selectTimeout, test.ioTimeout); !got.Equal(test.want) {
				t.Fatalf("deadline=%v, want %v", got, test.want)
			}
		})
	}
}

func TestSocketRollbackStopsWhenCloseInvalidatesGeneration(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	applicationFailure := errors.New("write deadline failed")
	readRollbackEntered := make(chan struct{})
	releaseReadRollback := make(chan struct{})
	readCalls := 0
	writeCalls := 0
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setReadFn: func(time.Time) error {
			readCalls++
			if readCalls == 2 {
				close(readRollbackEntered)
				<-releaseReadRollback
			}
			return nil
		},
		setWriteFn: func(time.Time) error {
			writeCalls++
			if writeCalls == 1 {
				return applicationFailure
			}
			return nil
		},
	}
	resource := &SocketResource{
		connection:        controlled,
		lastReadDeadline:  time.Now().Add(time.Second),
		lastWriteDeadline: time.Now().Add(2 * time.Second),
	}
	configured := make(chan error, 1)
	go func() { configured <- configureSocketTimeout(resource, 5*time.Second) }()
	select {
	case <-readRollbackEntered:
	case <-time.After(time.Second):
		t.Fatal("read rollback did not start")
	}
	closeSocket(resource)
	close(releaseReadRollback)
	select {
	case err := <-configured:
		if !errors.Is(err, net.ErrClosed) || !errors.Is(err, applicationFailure) {
			t.Fatalf("configuration error=%v, want application failure and net.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("configuration did not finish")
	}
	if writeCalls != 1 {
		t.Fatalf("write deadline calls=%d, want no write rollback after stale generation", writeCalls)
	}
}

func TestBlockedWriteIsBoundedByTimeout(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	resource := &SocketResource{connection: connection, ioTimeout: 25 * time.Millisecond}
	started := time.Now()
	result := sendSocket(resource, "x")
	elapsed := time.Since(started)
	assertBuiltinValue(t, builtinMapField(t, result, "ok"), value.NewBool(false))
	assertBuiltinValue(t, builtinMapField(t, result, "error"), value.NewString("operation timed out"))
	if elapsed < 10*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("write elapsed=%v, want bounded wait", elapsed)
	}
}

func TestPendingWriteObservesTimeoutConfiguration(t *testing.T) {
	machine := New()
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	writeEntered := make(chan struct{})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		writeFn: func(buffer []byte) (int, error) {
			close(writeEntered)
			return connection.Write(buffer)
		},
	}
	resource := &SocketResource{connection: controlled}
	handle := machine.shared.Sockets.add(resource)
	t.Cleanup(func() { machine.shared.Sockets.remove(handle) })
	result := make(chan value.Value, 1)
	go func() { result <- sendSocket(resource, "x") }()
	select {
	case <-writeEntered:
	case <-time.After(time.Second):
		t.Fatal("pending write did not start")
	}
	if _, err := invokeBuiltin(t, machine, "net_settimeout", socketValue(handle, "pipe", 0, true), value.NewInt(25)); err != nil {
		t.Fatalf("net_settimeout: %v", err)
	}
	select {
	case got := <-result:
		assertBuiltinValue(t, builtinMapField(t, got, "error"), value.NewString("operation timed out"))
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pending write did not observe configured timeout")
	}
}

func TestPendingAcceptObservesTimeoutConfiguration(t *testing.T) {
	underlying, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	acceptEntered := make(chan struct{})
	controlled := &controlledDeadlineListener{Listener: underlying}
	controlled.acceptFn = func() (net.Conn, error) {
		close(acceptEntered)
		return underlying.Accept()
	}
	resource := &ListenerResource{listener: controlled}
	t.Cleanup(func() { closeListener(resource) })
	result := make(chan error, 1)
	go func() {
		_, err := acceptConnection(resource)
		result <- err
	}()
	select {
	case <-acceptEntered:
	case <-time.After(time.Second):
		t.Fatal("pending accept did not start")
	}
	if err := configureListenerTimeout(resource, 25*time.Millisecond); err != nil {
		t.Fatalf("configure listener: %v", err)
	}
	select {
	case err := <-result:
		if err == nil || !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("accept error=%v, want deadline exceeded", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pending accept did not observe configured timeout")
	}
}

func TestAcceptedSocketDoesNotInheritListenerTimeout(t *testing.T) {
	machine := New()
	cleanupNetworkResources(t, machine)
	listener := callBuiltinWithinBound(t, machine, "net_listen", value.NewString("127.0.0.1"), value.NewInt(0))
	listenerHandle := int(builtinMapField(t, listener, "fd").AsInt)
	listenerResource, exists := machine.shared.Listeners.get(listenerHandle)
	if !exists {
		t.Fatal("listener not registered")
	}
	listenerResource.stateMu.Lock()
	address := listenerResource.listener.Addr().(*net.TCPAddr)
	listenerResource.stateMu.Unlock()
	if _, err := invokeBuiltin(t, machine, "net_settimeout", listener, value.NewInt(100)); err != nil {
		t.Fatalf("net_settimeout: %v", err)
	}
	client := callBuiltinWithinBound(t, machine, "net_connect", value.NewString("127.0.0.1"), value.NewInt(int64(address.Port)))
	accepted := callBuiltinWithinBound(t, machine, "net_accept", listener)
	t.Cleanup(func() {
		callBuiltinWithinBound(t, machine, "net_close", client)
		callBuiltinWithinBound(t, machine, "net_close", accepted)
	})
	_, acceptedResource := requireSocketResource(t, machine, accepted)
	acceptedResource.stateMu.Lock()
	timeout := acceptedResource.ioTimeout
	readDeadline := acceptedResource.lastReadDeadline
	writeDeadline := acceptedResource.lastWriteDeadline
	acceptedResource.stateMu.Unlock()
	if timeout != 0 || !readDeadline.IsZero() || !writeDeadline.IsZero() {
		t.Fatalf("accepted mode: timeout=%v read=%v write=%v, want blocking", timeout, readDeadline, writeDeadline)
	}
}

func TestPartialBufferedReadSurvivesDeadlineInstallationFailure(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setReadFn: func(time.Time) error {
			return errors.New("deadline install failed")
		},
	}
	resource := &SocketResource{connection: controlled, bufferedRead: []byte("x"), ioTimeout: 20 * time.Millisecond}
	result := receiveSocket(resource, 2)
	assertBuiltinValue(t, builtinMapField(t, result, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, result, "data"), value.NewBytes("x"))
	assertBuiltinValue(t, builtinMapField(t, result, "count"), value.NewInt(1))
}

func TestConcurrentTimeoutConfigurationsCommitInDeadlineOrder(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	readCalls := 0
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setReadFn: func(time.Time) error {
			readCalls++
			if readCalls == 1 {
				close(firstEntered)
				<-releaseFirst
			}
			return nil
		},
	}
	resource := &SocketResource{connection: controlled}
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- configureSocketTimeout(resource, 10*time.Millisecond) }()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first configuration did not start")
	}
	go func() { second <- configureSocketTimeout(resource, 20*time.Millisecond) }()
	close(releaseFirst)
	if err := <-first; err != nil {
		t.Fatalf("first configuration: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("second configuration: %v", err)
	}
	resource.stateMu.Lock()
	timeout := resource.ioTimeout
	resource.stateMu.Unlock()
	if timeout != 20*time.Millisecond {
		t.Fatalf("ioTimeout=%v, want second configuration 20ms", timeout)
	}
}

func TestListenerSelectRestorationFailureKeepsAcceptedConnection(t *testing.T) {
	underlying, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	restoreFailure := errors.New("listener restore failed")
	setterCalls := 0
	controlled := &controlledDeadlineListener{
		Listener: underlying,
		acceptFn: func() (net.Conn, error) {
			return accepted, nil
		},
		setFn: func(time.Time) error {
			setterCalls++
			if setterCalls == 2 {
				return restoreFailure
			}
			return nil
		},
	}
	resource := &ListenerResource{listener: controlled, ioTimeout: 100 * time.Millisecond}
	t.Cleanup(func() { closeListener(resource) })
	ready, err := selectListener(resource, 10*time.Millisecond)
	if ready || !errors.Is(err, restoreFailure) {
		t.Fatalf("selectListener=(%v, %v), want restoration failure", ready, err)
	}
	resource.stateMu.Lock()
	buffered := resource.bufferedAccept
	resource.stateMu.Unlock()
	if buffered != accepted {
		t.Fatalf("buffered accept=%v, want accepted connection", buffered)
	}
}

func TestNetSelectManagementFailureStopsLaterCandidatesAndKeepsEarlierBuffer(t *testing.T) {
	machine := New()
	makeControlled := func(readFn func([]byte) (int, error), setReadFn func(time.Time) error) (*SocketResource, value.Value, *controlledDeadlineConn) {
		connection, peer := net.Pipe()
		t.Cleanup(func() {
			_ = connection.Close()
			_ = peer.Close()
		})
		controlled := &controlledDeadlineConn{Conn: connection, readFn: readFn, setReadFn: setReadFn}
		resource := &SocketResource{connection: controlled}
		handle := machine.shared.Sockets.add(resource)
		t.Cleanup(func() { machine.shared.Sockets.remove(handle) })
		return resource, socketValue(handle, "pipe", 0, true), controlled
	}
	first, firstValue, _ := makeControlled(func(buffer []byte) (int, error) {
		buffer[0] = 'a'
		return 1, nil
	}, nil)
	managementFailure := errors.New("probe install failed")
	_, secondValue, _ := makeControlled(nil, func(time.Time) error { return managementFailure })
	thirdReads := 0
	_, thirdValue, _ := makeControlled(func([]byte) (int, error) {
		thirdReads++
		return 0, io.EOF
	}, nil)

	_, err := invokeBuiltin(t, machine, "net_select",
		value.NewArray([]value.Value{firstValue, secondValue, thirdValue}),
		value.NewArray(nil), value.NewArray(nil), value.NewInt(10))
	if !errors.Is(err, managementFailure) {
		t.Fatalf("net_select error=%v, want management failure", err)
	}
	first.stateMu.Lock()
	buffered := string(first.bufferedRead)
	first.stateMu.Unlock()
	if buffered != "a" {
		t.Fatalf("earlier buffer=%q, want a", buffered)
	}
	if thirdReads != 0 {
		t.Fatalf("later candidate reads=%d, want 0", thirdReads)
	}
}

func TestNetSetTimeoutUsesListenerFirstAndRejectsClosedHandle(t *testing.T) {
	machine := New()
	underlying, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	controlledListener := &controlledDeadlineListener{Listener: underlying}
	listenerResource := &ListenerResource{listener: controlledListener}
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	controlledSocket := &controlledDeadlineConn{Conn: connection}
	socketResource := &SocketResource{connection: controlledSocket}
	const handle = 77
	machine.shared.Listeners.mu.Lock()
	machine.shared.Listeners.items[handle] = listenerResource
	machine.shared.Listeners.mu.Unlock()
	machine.shared.Sockets.mu.Lock()
	machine.shared.Sockets.items[handle] = socketResource
	machine.shared.Sockets.mu.Unlock()
	t.Cleanup(func() {
		machine.shared.Listeners.remove(handle)
		machine.shared.Sockets.remove(handle)
		closeListener(listenerResource)
		closeSocket(socketResource)
	})
	socket := socketValue(handle, "ambiguous", 0, true)
	if _, err := invokeBuiltin(t, machine, "net_settimeout", socket, value.NewInt(10)); err != nil {
		t.Fatalf("listener-first configuration: %v", err)
	}
	listenerResource.stateMu.Lock()
	listenerTimeout := listenerResource.ioTimeout
	listenerResource.stateMu.Unlock()
	socketResource.stateMu.Lock()
	socketTimeout := socketResource.ioTimeout
	socketResource.stateMu.Unlock()
	if listenerTimeout != 10*time.Millisecond || socketTimeout != 0 {
		t.Fatalf("timeouts listener=%v socket=%v, want listener-first", listenerTimeout, socketTimeout)
	}

	closeListener(listenerResource)
	if _, err := invokeBuiltin(t, machine, "net_settimeout", socket, value.NewInt(10)); err == nil || !strings.Contains(err.Error(), "invalid socket") {
		t.Fatalf("closed listener error=%v, want invalid socket", err)
	}
}

func TestCloseDoesNotWaitForBlockedWriteDeadlineSetter(t *testing.T) {
	connection, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	setterEntered := make(chan struct{})
	releaseSetter := make(chan struct{})
	controlled := &controlledDeadlineConn{
		Conn: connection,
		setWriteFn: func(time.Time) error {
			close(setterEntered)
			<-releaseSetter
			return nil
		},
	}
	resource := &SocketResource{connection: controlled}
	configured := make(chan error, 1)
	go func() { configured <- configureSocketTimeout(resource, 20*time.Millisecond) }()
	select {
	case <-setterEntered:
	case <-time.After(time.Second):
		t.Fatal("write deadline setter was not entered")
	}
	closed := make(chan struct{})
	go func() {
		closeSocket(resource)
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("close waited for write deadline setter")
	}
	close(releaseSetter)
	if err := <-configured; !errors.Is(err, net.ErrClosed) {
		t.Fatalf("configuration error=%v, want net.ErrClosed", err)
	}
	if controlled.closes() != 1 {
		t.Fatalf("close calls=%d, want 1", controlled.closes())
	}
}

func TestCloseDoesNotWaitForBlockedListenerDeadlineSetter(t *testing.T) {
	underlying, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	setterEntered := make(chan struct{})
	releaseSetter := make(chan struct{})
	controlled := &controlledDeadlineListener{
		Listener: underlying,
		setFn: func(time.Time) error {
			close(setterEntered)
			<-releaseSetter
			return nil
		},
	}
	resource := &ListenerResource{listener: controlled}
	configured := make(chan error, 1)
	go func() { configured <- configureListenerTimeout(resource, 20*time.Millisecond) }()
	select {
	case <-setterEntered:
	case <-time.After(time.Second):
		t.Fatal("listener deadline setter was not entered")
	}
	closed := make(chan struct{})
	go func() {
		closeListener(resource)
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("close waited for listener deadline setter")
	}
	close(releaseSetter)
	if err := <-configured; !errors.Is(err, net.ErrClosed) {
		t.Fatalf("configuration error=%v, want net.ErrClosed", err)
	}
	if controlled.closes() != 1 {
		t.Fatalf("close calls=%d, want 1", controlled.closes())
	}
}

func TestActiveSocketProbeBoundsConcurrentConfiguration(t *testing.T) {
	tests := []struct {
		name              string
		configuredTimeout time.Duration
		wantAgainstProbe  string
		wantRestoredZero  bool
	}{
		{name: "longer timeout cannot extend", configuredTimeout: 200 * time.Millisecond, wantAgainstProbe: "equal"},
		{name: "shorter timeout may shorten", configuredTimeout: 20 * time.Millisecond, wantAgainstProbe: "before"},
		{name: "blocking cannot remove", configuredTimeout: 0, wantAgainstProbe: "equal", wantRestoredZero: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			connection, peer := net.Pipe()
			t.Cleanup(func() {
				_ = connection.Close()
				_ = peer.Close()
			})
			readEntered := make(chan struct{})
			releaseRead := make(chan struct{})
			controlled := &controlledDeadlineConn{
				Conn: connection,
				readFn: func(buffer []byte) (int, error) {
					close(readEntered)
					<-releaseRead
					buffer[0] = 'x'
					return 1, nil
				},
			}
			resource := &SocketResource{connection: controlled}
			selected := make(chan error, 1)
			go func() {
				ready, err := selectSocket(resource, 100*time.Millisecond)
				if err == nil && !ready {
					err = errors.New("probe was not ready")
				}
				selected <- err
			}()
			select {
			case <-readEntered:
			case <-time.After(time.Second):
				t.Fatal("probe read did not start")
			}
			if err := configureSocketTimeout(resource, test.configuredTimeout); err != nil {
				t.Fatalf("concurrent configuration: %v", err)
			}
			controlled.mu.Lock()
			installed := append([]time.Time(nil), controlled.readDeadlines...)
			controlled.mu.Unlock()
			if len(installed) != 2 {
				t.Fatalf("deadlines before cleanup=%v, want probe and configuration", installed)
			}
			switch test.wantAgainstProbe {
			case "equal":
				if !installed[1].Equal(installed[0]) {
					t.Fatalf("configured deadline=%v, want probe bound %v", installed[1], installed[0])
				}
			case "before":
				if !installed[1].Before(installed[0]) {
					t.Fatalf("configured deadline=%v, want before probe bound %v", installed[1], installed[0])
				}
			}
			close(releaseRead)
			if err := <-selected; err != nil {
				t.Fatalf("selectSocket: %v", err)
			}
			controlled.mu.Lock()
			restored := controlled.readDeadlines[len(controlled.readDeadlines)-1]
			controlled.mu.Unlock()
			if restored.IsZero() != test.wantRestoredZero {
				t.Fatalf("restored deadline=%v, want zero=%v", restored, test.wantRestoredZero)
			}
			if !test.wantRestoredZero && restored.Before(installed[1]) {
				t.Fatalf("restored deadline=%v, want no earlier than %v", restored, installed[1])
			}
		})
	}
}
