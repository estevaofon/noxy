package vm

import (
	"net"
	"testing"

	"noxy-vm/internal/value"
)

func callBuiltinWithinBound(t *testing.T, machine *VM, name string, args ...value.Value) value.Value {
	t.Helper()
	native := requireBuiltin(t, machine, name)
	result := make(chan value.Value, 1)
	go func() {
		result <- native.Fn(args)
	}()
	return awaitBuiltinResult(t, result, name)
}

func builtinMapField(t *testing.T, object value.Value, field string) value.Value {
	t.Helper()
	mapped := requireBuiltinMap(t, object)
	got, ok := mapped.Data[field]
	if !ok {
		t.Fatalf("map does not contain field %q", field)
	}
	return got
}

func TestNetworkBuiltinsLoopbackLifecycle(t *testing.T) {
	machine := New()
	defer func() {
		machine.shared.NetLock.Lock()
		defer machine.shared.NetLock.Unlock()
		for _, listener := range machine.shared.NetListeners {
			_ = listener.Close()
		}
		for _, connection := range machine.shared.NetConns {
			_ = connection.Close()
		}
	}()

	listener := callBuiltinWithinBound(t, machine, "net_listen", value.NewString("127.0.0.1"), value.NewInt(0))
	assertBuiltinValue(t, builtinMapField(t, listener, "open"), value.NewBool(true))
	listenerFD := int(builtinMapField(t, listener, "fd").AsInt)
	machine.shared.NetLock.Lock()
	registeredListener := machine.shared.NetListeners[listenerFD]
	machine.shared.NetLock.Unlock()
	if registeredListener == nil {
		t.Fatalf("listener descriptor %d is not registered", listenerFD)
	}
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
	machine.shared.NetLock.Lock()
	listenerCount := len(machine.shared.NetListeners)
	connectionCount := len(machine.shared.NetConns)
	machine.shared.NetLock.Unlock()
	if listenerCount != 0 || connectionCount != 0 {
		t.Fatalf("network state after close: %d listeners, %d connections", listenerCount, connectionCount)
	}
}
