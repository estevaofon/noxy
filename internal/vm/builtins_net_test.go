package vm

import (
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

func TestNetSelectBufferIsSharedAcrossVMs(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	child := NewWithShared(machine.shared, machine.Config)

	sent := callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("x"))
	assertBuiltinValue(t, builtinMapField(t, sent, "ok"), value.NewBool(true))
	selected := callBuiltinWithinBound(t, machine, "net_select",
		value.NewArray([]value.Value{server}), value.NewArray(nil), value.NewArray(nil), value.NewInt(250))
	assertBuiltinValue(t, builtinMapField(t, selected, "read_count"), value.NewInt(1))
	received := callBuiltinWithinBound(t, child, "net_recv", server, value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, received, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, received, "count"), value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, received, "data"), value.NewBytes("x"))
}

func TestConcurrentNetSelect(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	child := NewWithShared(machine.shared, machine.Config)

	sent := callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("x"))
	assertBuiltinValue(t, builtinMapField(t, sent, "ok"), value.NewBool(true))
	start := make(chan struct{})
	done := make(chan value.Value, 2)
	for _, current := range []*VM{machine, child} {
		current := current
		go func() {
			<-start
			done <- callBuiltinWithinBound(t, current, "net_select",
				value.NewArray([]value.Value{server}), value.NewArray(nil), value.NewArray(nil), value.NewInt(5))
		}()
	}
	close(start)
	for i := 0; i < 2; i++ {
		select {
		case result := <-done:
			requireValidSelectResult(t, result)
		case <-time.After(statefulBuiltinTimeout):
			t.Fatal("concurrent net_select did not complete")
		}
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
