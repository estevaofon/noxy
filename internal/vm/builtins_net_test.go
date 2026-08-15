package vm

import (
	"errors"
	"math"
	"net"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

func callBuiltinWithinBound(t *testing.T, machine *VM, name string, args ...value.Value) value.Value {
	t.Helper()
	native := requireBuiltin(t, machine, name)
	type invocationResult struct {
		value value.Value
		err   error
	}
	result := make(chan invocationResult, 1)
	go func() {
		value, err := native.Invoke(machine, args)
		result <- invocationResult{value: value, err: err}
	}()
	select {
	case invocation := <-result:
		if invocation.err != nil {
			t.Fatalf("%s: %v", name, invocation.err)
		}
		return invocation.value
	case <-time.After(statefulBuiltinTimeout):
		t.Fatalf("%s did not complete within %s", name, statefulBuiltinTimeout)
		return value.NewNull()
	}
}

func interpretVMSourceWithinBound(t *testing.T, machine *VM, source string) error {
	t.Helper()
	code := compileVMSource(t, source)
	result := make(chan error, 1)
	go func() {
		result <- machine.Interpret(code)
	}()
	select {
	case err := <-result:
		return err
	case <-time.After(statefulBuiltinTimeout):
		t.Fatalf("network program did not complete within %s", statefulBuiltinTimeout)
		return nil
	}
}

func recordDeferredListenerResource(machine *VM, captured **ListenerResource) {
	machine.DefineNative("record_listener_resource", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		socket, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull()
		}
		handle, ok := socket.Get("fd")
		if !ok {
			return value.NewNull()
		}
		*captured, _ = machine.shared.Listeners.get(int(handle.AsInt))
		return value.NewNull()
	})
}

func requireDeferredListenerClosed(t *testing.T, machine *VM, resource *ListenerResource) {
	t.Helper()
	if got := len(machine.shared.Listeners.snapshot()); got != 0 {
		t.Fatalf("listeners=%d, want 0", got)
	}
	if got := len(machine.shared.Sockets.snapshot()); got != 0 {
		t.Fatalf("sockets=%d, want 0", got)
	}
	if resource == nil {
		t.Fatal("listener resource was not recorded")
	}
	resource.stateMu.Lock()
	closed := resource.closed
	resource.stateMu.Unlock()
	if !closed {
		t.Fatal("listener resource remains open")
	}
}

func requireSocketResource(t *testing.T, machine *VM, socket value.Value) (int, *SocketResource) {
	t.Helper()
	handle := int(builtinMapField(t, socket, "fd").AsInt)
	resource, exists := machine.shared.Sockets.get(handle)
	if !exists {
		t.Fatalf("socket descriptor %d is not registered", handle)
	}
	return handle, resource
}

func requireDeferredSocketsClosed(t *testing.T, machine *VM, resources map[int]*SocketResource) {
	t.Helper()
	registered := machine.shared.Sockets.snapshot()
	for handle, resource := range resources {
		if _, exists := registered[handle]; exists {
			t.Errorf("socket descriptor %d remains registered", handle)
		}
		resource.stateMu.Lock()
		closed := resource.closed
		resource.stateMu.Unlock()
		if !closed {
			t.Errorf("socket descriptor %d remains open", handle)
		}
	}
}

func cleanupNetworkResources(t *testing.T, machine *VM) {
	t.Helper()
	t.Cleanup(func() {
		for handle, listener := range machine.shared.Listeners.snapshot() {
			machine.shared.Listeners.remove(handle)
			closeListener(listener)
		}
		for handle, socket := range machine.shared.Sockets.snapshot() {
			machine.shared.Sockets.remove(handle)
			closeSocket(socket)
		}
	})
}

func TestDeferredSocketCleanupClosesListenerOnEveryExit(t *testing.T) {
	tests := []struct {
		name      string
		suffix    string
		wantError bool
	}{
		{"normal", "", false},
		{"runtime error", "\nlet zero: int = 0\nprint(1 / zero)", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machine := New()
			cleanupNetworkResources(t, machine)
			var resource *ListenerResource
			recordDeferredListenerResource(machine, &resource)
			source := `use net
let listener: net.Socket = net.listen("127.0.0.1", 0)
record_listener_resource(listener)
defer net.socket_close(listener)` + test.suffix

			err := interpretVMSourceWithinBound(t, machine, source)
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v, wantError=%v", err, test.wantError)
			}
			requireDeferredListenerClosed(t, machine, resource)
		})
	}
}

func TestDeferredSocketResourceCleanupContinuesAfterFailure(t *testing.T) {
	machine := New()
	cleanupNetworkResources(t, machine)
	var resource *ListenerResource
	recordDeferredListenerResource(machine, &resource)
	sentinel := errors.New("sentinel cleanup failure")
	defineCleanupFailureNative(machine, sentinel)
	source := `use net
let listener: net.Socket = net.listen("127.0.0.1", 0)
record_listener_resource(listener)
defer net.socket_close(listener)
defer cleanup_fail()`

	err := interpretVMSourceWithinBound(t, machine, source)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error=%v, want sentinel cleanup failure", err)
	}
	requireDeferredListenerClosed(t, machine, resource)
}

func TestDeferredConnectedSocketCleanupClosesEverySocketOnEveryExit(t *testing.T) {
	tests := []struct {
		name      string
		suffix    string
		sentinel  error
		wantError bool
	}{
		{name: "normal"},
		{name: "runtime error", suffix: "\nlet zero: int = 0\nprint(1 / zero)", wantError: true},
		{name: "newer cleanup failure", suffix: "\ndefer cleanup_fail()", sentinel: errors.New("sentinel cleanup failure"), wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machine := New()
			cleanupNetworkResources(t, machine)
			listener, client, accepted := setupAcceptedLoopback(t, machine)
			clientHandle, clientResource := requireSocketResource(t, machine, client)
			acceptedHandle, acceptedResource := requireSocketResource(t, machine, accepted)

			callBuiltinWithinBound(t, machine, "net_close", listener)
			if got := len(machine.shared.Listeners.snapshot()); got != 0 {
				t.Fatalf("listeners after explicit close=%d, want 0", got)
			}

			machine.DefineNative("deferred_client_socket", func([]value.Value) value.Value { return client })
			machine.DefineNative("deferred_accepted_socket", func([]value.Value) value.Value { return accepted })
			if test.sentinel != nil {
				defineCleanupFailureNative(machine, test.sentinel)
			}
			source := `use net
let client: net.Socket = deferred_client_socket()
let accepted: net.Socket = deferred_accepted_socket()
defer net.socket_close(client)
defer net.socket_close(accepted)` + test.suffix

			err := interpretVMSourceWithinBound(t, machine, source)
			if test.sentinel != nil {
				if !errors.Is(err, test.sentinel) {
					t.Fatalf("error=%v, want sentinel cleanup failure", err)
				}
			} else if (err != nil) != test.wantError {
				t.Fatalf("error=%v, wantError=%v", err, test.wantError)
			}
			requireDeferredSocketsClosed(t, machine, map[int]*SocketResource{
				clientHandle:   clientResource,
				acceptedHandle: acceptedResource,
			})
		})
	}
}

func builtinMapField(t *testing.T, object value.Value, field string) value.Value {
	t.Helper()
	mapped := requireBuiltinMap(t, object)
	return requireTestMapValue(t, mapped, field)
}

func setupAcceptedLoopback(t *testing.T, machine *VM) (value.Value, value.Value, value.Value) {
	t.Helper()
	listener := callBuiltinWithinBound(t, machine, "net_listen", value.NewString("127.0.0.1"), value.NewInt(0))
	assertBuiltinValue(t, builtinMapField(t, listener, "open"), value.NewBool(true))
	listenerFD := int(builtinMapField(t, listener, "fd").AsInt)
	listenerResource, exists := machine.shared.Listeners.get(listenerFD)
	if !exists {
		t.Fatalf("listener descriptor %d is not registered", listenerFD)
	}
	listenerResource.stateMu.Lock()
	registeredListener := listenerResource.listener
	listenerResource.stateMu.Unlock()
	tcpAddress, ok := registeredListener.Addr().(*net.TCPAddr)
	if !ok || tcpAddress.Port == 0 {
		t.Fatalf("listener address = %#v, want OS-assigned TCP port", registeredListener.Addr())
	}

	client := callBuiltinWithinBound(t, machine, "net_connect", value.NewString("127.0.0.1"), value.NewInt(int64(tcpAddress.Port)))
	assertBuiltinValue(t, builtinMapField(t, client, "open"), value.NewBool(true))
	listenerReady := callBuiltinWithinBound(t, machine, "net_select",
		value.NewArray([]value.Value{listener}), value.NewArray(nil), value.NewArray(nil), value.NewInt(250))
	assertBuiltinValue(t, builtinMapField(t, listenerReady, "read_count"), value.NewInt(1))
	server := callBuiltinWithinBound(t, machine, "net_accept", listener)
	assertBuiltinValue(t, builtinMapField(t, server, "open"), value.NewBool(true))

	t.Cleanup(func() {
		callBuiltinWithinBound(t, machine, "net_close", client)
		callBuiltinWithinBound(t, machine, "net_close", server)
		callBuiltinWithinBound(t, machine, "net_close", value.NewInt(int64(listenerFD)))
	})
	return listener, client, server
}

func requireValidSelectResult(t *testing.T, result value.Value) {
	t.Helper()
	mapping := requireBuiltinMap(t, result)
	for _, field := range []string{"read", "write", "error"} {
		item := requireTestMapValue(t, mapping, field)
		if _, ok := item.Obj.(*value.ObjArray); item.Type != value.VAL_OBJ || !ok {
			t.Fatalf("select %s=%v, want array", field, item)
		}
	}
	for _, field := range []string{"read_count", "write_count", "error_count"} {
		item := requireTestMapValue(t, mapping, field)
		if item.Type != value.VAL_INT {
			t.Fatalf("select %s=%v, want int", field, item)
		}
	}
}

func TestNetSelectDoesNotConsumeSocketData(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	child := NewWithShared(machine.shared, machine.Config)
	_, resource := requireSocketResource(t, machine, server)
	resource.stateMu.Lock()
	underlying := resource.connection
	readCalls := 0
	resource.connection = &controlledDeadlineConn{
		Conn: underlying,
		readFn: func(buffer []byte) (int, error) {
			readCalls++
			return underlying.Read(buffer)
		},
	}
	resource.stateMu.Unlock()

	sent := callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("xy"))
	assertBuiltinValue(t, builtinMapField(t, sent, "ok"), value.NewBool(true))
	for attempt := 0; attempt < 2; attempt++ {
		selected := callBuiltinWithinBound(t, machine, "net_select",
			value.NewArray([]value.Value{server}), value.NewArray(nil), value.NewArray(nil), value.NewInt(0))
		assertBuiltinValue(t, builtinMapField(t, selected, "read_count"), value.NewInt(1))
	}
	if readCalls != 0 {
		t.Fatalf("socket reads during net_select=%d, want 0", readCalls)
	}
	received := callBuiltinWithinBound(t, child, "net_recv", server, value.NewInt(2))
	assertBuiltinValue(t, builtinMapField(t, received, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, received, "count"), value.NewInt(2))
	assertBuiltinValue(t, builtinMapField(t, received, "data"), value.NewBytes("xy"))
}

func TestNetSelectDoesNotAcceptPendingConnection(t *testing.T) {
	machine := New()
	cleanupNetworkResources(t, machine)
	listener := callBuiltinWithinBound(t, machine, "net_listen", value.NewString("127.0.0.1"), value.NewInt(0))
	listenerFD := int(builtinMapField(t, listener, "fd").AsInt)
	listenerResource, exists := machine.shared.Listeners.get(listenerFD)
	if !exists {
		t.Fatalf("listener descriptor %d is not registered", listenerFD)
	}
	listenerResource.stateMu.Lock()
	underlying := listenerResource.listener
	acceptCalls := 0
	listenerResource.listener = &controlledDeadlineListener{
		Listener: underlying,
		acceptFn: func() (net.Conn, error) {
			acceptCalls++
			return underlying.Accept()
		},
	}
	address := underlying.Addr().(*net.TCPAddr)
	listenerResource.stateMu.Unlock()
	client := callBuiltinWithinBound(t, machine, "net_connect", value.NewString("127.0.0.1"), value.NewInt(int64(address.Port)))

	for attempt := 0; attempt < 2; attempt++ {
		selected := callBuiltinWithinBound(t, machine, "net_select",
			value.NewArray([]value.Value{listener}), value.NewArray(nil), value.NewArray(nil), value.NewInt(0))
		assertBuiltinValue(t, builtinMapField(t, selected, "read_count"), value.NewInt(1))
	}
	if acceptCalls != 0 {
		t.Fatalf("listener accepts during net_select=%d, want 0", acceptCalls)
	}
	accepted := callBuiltinWithinBound(t, machine, "net_accept", listener)
	assertBuiltinValue(t, builtinMapField(t, accepted, "open"), value.NewBool(true))
	if acceptCalls != 1 {
		t.Fatalf("listener accepts after net_accept=%d, want exactly 1", acceptCalls)
	}
	sent := callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("ok"))
	assertBuiltinValue(t, builtinMapField(t, sent, "count"), value.NewInt(2))
	received := callBuiltinWithinBound(t, machine, "net_recv", accepted, value.NewInt(2))
	assertBuiltinValue(t, builtinMapField(t, received, "data"), value.NewBytes("ok"))
}

func TestNetSelectValidatesArgumentsSynchronously(t *testing.T) {
	machine := New()
	empty := value.NewArray(nil)
	maximum := int64(math.MaxInt64) / int64(time.Millisecond)
	tests := []struct {
		name string
		args []value.Value
	}{
		{"wrong arity", []value.Value{empty}},
		{"wrong read set", []value.Value{value.NewNull(), empty, empty, value.NewInt(0)}},
		{"wrong write set", []value.Value{empty, value.NewNull(), empty, value.NewInt(0)}},
		{"wrong error set", []value.Value{empty, empty, value.NewNull(), value.NewInt(0)}},
		{"wrong timeout type", []value.Value{empty, empty, empty, value.NewString("0")}},
		{"negative timeout", []value.Value{empty, empty, empty, value.NewInt(-1)}},
		{"overflow timeout", []value.Value{empty, empty, empty, value.NewInt(maximum + 1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := invokeBuiltin(t, machine, "net_select", test.args...)
			if err == nil {
				t.Fatalf("net_select result=%v, want synchronous error", result)
			}
			if result.Type != value.VAL_NULL {
				t.Fatalf("net_select result=%v, want null on error", result)
			}
		})
	}
}

func TestNetworkBuiltinsLoopbackLifecycle(t *testing.T) {
	machine := New()
	defer func() {
		for handle, listener := range machine.shared.Listeners.snapshot() {
			machine.shared.Listeners.remove(handle)
			closeListener(listener)
		}
		for handle, socket := range machine.shared.Sockets.snapshot() {
			machine.shared.Sockets.remove(handle)
			closeSocket(socket)
		}
	}()

	listener := callBuiltinWithinBound(t, machine, "net_listen", value.NewString("127.0.0.1"), value.NewInt(0))
	assertBuiltinValue(t, builtinMapField(t, listener, "open"), value.NewBool(true))
	listenerFD := int(builtinMapField(t, listener, "fd").AsInt)
	listenerResource, exists := machine.shared.Listeners.get(listenerFD)
	if !exists {
		t.Fatalf("listener descriptor %d is not registered", listenerFD)
	}
	listenerResource.stateMu.Lock()
	registeredListener := listenerResource.listener
	listenerResource.stateMu.Unlock()
	tcpAddress, ok := registeredListener.Addr().(*net.TCPAddr)
	if !ok || tcpAddress.Port == 0 {
		t.Fatalf("listener address = %#v, want OS-assigned TCP port", registeredListener.Addr())
	}

	client := callBuiltinWithinBound(t, machine, "net_connect", value.NewString("127.0.0.1"), value.NewInt(int64(tcpAddress.Port)))
	assertBuiltinValue(t, builtinMapField(t, client, "open"), value.NewBool(true))

	listenerReady := callBuiltinWithinBound(t, machine, "net_select",
		value.NewArray([]value.Value{listener}), value.NewArray(nil), value.NewArray(nil), value.NewInt(250),
	)
	assertBuiltinValue(t, builtinMapField(t, listenerReady, "read_count"), value.NewInt(1))
	server := callBuiltinWithinBound(t, machine, "net_accept", listener)
	assertBuiltinValue(t, builtinMapField(t, server, "open"), value.NewBool(true))
	assertBuiltinValue(t, callBuiltinWithinBound(t, machine, "net_setblocking", server, value.NewBool(false)), value.NewNull())

	clientSend := callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("ping"))
	assertBuiltinValue(t, builtinMapField(t, clientSend, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, clientSend, "count"), value.NewInt(4))
	serverReady := callBuiltinWithinBound(t, machine, "net_select",
		value.NewArray([]value.Value{server}), value.NewArray(nil), value.NewArray(nil), value.NewInt(250),
	)
	assertBuiltinValue(t, builtinMapField(t, serverReady, "read_count"), value.NewInt(1))
	serverReceive := callBuiltinWithinBound(t, machine, "net_recv", server, value.NewInt(4))
	assertBuiltinValue(t, builtinMapField(t, serverReceive, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, serverReceive, "count"), value.NewInt(4))
	assertBuiltinValue(t, builtinMapField(t, serverReceive, "data"), value.NewBytes("ping"))

	serverSend := callBuiltinWithinBound(t, machine, "net_send", server, value.NewBytes("pong"))
	assertBuiltinValue(t, builtinMapField(t, serverSend, "ok"), value.NewBool(true))
	clientReady := callBuiltinWithinBound(t, machine, "net_select",
		value.NewArray([]value.Value{client}), value.NewArray(nil), value.NewArray(nil), value.NewInt(250),
	)
	assertBuiltinValue(t, builtinMapField(t, clientReady, "read_count"), value.NewInt(1))
	clientReceive := callBuiltinWithinBound(t, machine, "net_recv", client, value.NewInt(4))
	assertBuiltinValue(t, builtinMapField(t, clientReceive, "data"), value.NewBytes("pong"))

	assertBuiltinValue(t, callBuiltinWithinBound(t, machine, "net_close", client), value.NewNull())
	assertBuiltinValue(t, callBuiltinWithinBound(t, machine, "net_close", server), value.NewNull())
	assertBuiltinValue(t, callBuiltinWithinBound(t, machine, "net_close", value.NewInt(int64(listenerFD))), value.NewNull())
	listenerCount := len(machine.shared.Listeners.snapshot())
	connectionCount := len(machine.shared.Sockets.snapshot())
	if listenerCount != 0 || connectionCount != 0 {
		t.Fatalf("network state after close: %d listeners, %d connections", listenerCount, connectionCount)
	}
}
