//go:build windows || aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package vm

import (
	"net"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

func networkPollIntegrationSelect(
	t *testing.T,
	machine *VM,
	read, write, errors []value.Value,
	timeoutMilliseconds int64,
) value.Value {
	t.Helper()
	return callBuiltinWithinBound(t, machine, "net_select",
		value.NewArray(read), value.NewArray(write), value.NewArray(errors), value.NewInt(timeoutMilliseconds))
}

func requireNetworkPollIntegrationSet(t *testing.T, result value.Value, field string, want ...value.Value) {
	t.Helper()
	count := builtinMapField(t, result, field+"_count")
	assertBuiltinValue(t, count, value.NewInt(int64(len(want))))
	arrayValue := builtinMapField(t, result, field)
	array, ok := arrayValue.Obj.(*value.ObjArray)
	if arrayValue.Type != value.VAL_OBJ || !ok || array == nil {
		t.Fatalf("select %s=%v, want array", field, arrayValue)
	}
	if len(array.Elements) != 64 {
		t.Fatalf("select %s length=%d, want 64", field, len(array.Elements))
	}
	for index, expected := range want {
		gotMap := requireBuiltinMap(t, array.Elements[index])
		wantMap := requireBuiltinMap(t, expected)
		if gotMap != wantMap {
			t.Fatalf("select %s[%d] did not preserve the requested occurrence", field, index)
		}
		gotFD := builtinMapField(t, array.Elements[index], "fd")
		wantFD := builtinMapField(t, expected, "fd")
		assertBuiltinValue(t, gotFD, wantFD)
	}
	for index := len(want); index < len(array.Elements); index++ {
		if array.Elements[index].Type != value.VAL_NULL {
			t.Fatalf("select %s[%d]=%v, want null padding", field, index, array.Elements[index])
		}
	}
}

func requireNetworkPollIntegrationTCPConn(t *testing.T, machine *VM, socket value.Value) *net.TCPConn {
	t.Helper()
	_, resource := requireSocketResource(t, machine, socket)
	resource.stateMu.Lock()
	underlying := resource.connection
	resource.stateMu.Unlock()
	connection, ok := underlying.(*net.TCPConn)
	if !ok {
		t.Fatalf("socket connection=%T, want *net.TCPConn", underlying)
	}
	return connection
}

func requireNetworkPollIntegrationPromptClose(
	t *testing.T,
	machine *VM,
	candidate value.Value,
	poll <-chan networkInvocationResult,
) {
	t.Helper()
	closeNative := requireBuiltin(t, machine, "net_close")
	closed := make(chan error, 1)
	go func() {
		_, err := closeNative.Invoke(machine, []value.Value{candidate})
		closed <- err
	}()
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	for poll != nil || closed != nil {
		select {
		case invocation := <-poll:
			if invocation.err != nil {
				t.Fatal(invocation.err)
			}
			for _, field := range []string{"read", "write", "error"} {
				requireNetworkPollIntegrationSet(t, invocation.value, field)
			}
			poll = nil
		case err := <-closed:
			if err != nil {
				t.Fatal(err)
			}
			closed = nil
		case <-timer.C:
			t.Fatal("net_select and local close did not complete promptly")
		}
	}
}

func TestNetworkPollIntegrationRepeatedReadPollsPreserveExactUnreadBytes(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)

	sent := callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("exact"))
	assertBuiltinValue(t, builtinMapField(t, sent, "count"), value.NewInt(5))
	for attempt := 0; attempt < 2; attempt++ {
		selected := networkPollIntegrationSelect(t, machine, []value.Value{server}, nil, nil, 0)
		requireNetworkPollIntegrationSet(t, selected, "read", server)
	}
	received := callBuiltinWithinBound(t, machine, "net_recv", server, value.NewInt(5))
	assertBuiltinValue(t, builtinMapField(t, received, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, received, "count"), value.NewInt(5))
	assertBuiltinValue(t, builtinMapField(t, received, "data"), value.NewBytes("exact"))
}

func TestNetworkPollIntegrationRepeatedListenerPollsPreservePendingAccept(t *testing.T) {
	machine := New()
	cleanupNetworkResources(t, machine)
	listener := callBuiltinWithinBound(t, machine, "net_listen", value.NewString("127.0.0.1"), value.NewInt(0))
	listenerFD := int(builtinMapField(t, listener, "fd").AsInt)
	resource, exists := machine.shared.Listeners.get(listenerFD)
	if !exists {
		t.Fatalf("listener descriptor %d is not registered", listenerFD)
	}
	resource.stateMu.Lock()
	address := resource.listener.Addr().(*net.TCPAddr)
	resource.stateMu.Unlock()
	client := callBuiltinWithinBound(t, machine, "net_connect", value.NewString("127.0.0.1"), value.NewInt(int64(address.Port)))

	for attempt := 0; attempt < 2; attempt++ {
		selected := networkPollIntegrationSelect(t, machine, []value.Value{listener}, nil, nil, 0)
		requireNetworkPollIntegrationSet(t, selected, "read", listener)
	}
	accepted := callBuiltinWithinBound(t, machine, "net_accept", listener)
	assertBuiltinValue(t, builtinMapField(t, accepted, "open"), value.NewBool(true))
	sent := callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("ok"))
	assertBuiltinValue(t, builtinMapField(t, sent, "count"), value.NewInt(2))
	received := callBuiltinWithinBound(t, machine, "net_recv", accepted, value.NewInt(2))
	assertBuiltinValue(t, builtinMapField(t, received, "data"), value.NewBytes("ok"))
}

func TestNetworkPollIntegrationConnectedSocketIsWritable(t *testing.T) {
	machine := New()
	_, client, _ := setupAcceptedLoopback(t, machine)

	selected := networkPollIntegrationSelect(t, machine, nil, []value.Value{client}, nil, 250)
	requireNetworkPollIntegrationSet(t, selected, "write", client)
}

func TestNetworkPollIntegrationDuplicateOccurrencesPreserveOrder(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("x"))
	handle := int(builtinMapField(t, server, "fd").AsInt)
	first := socketValue(handle, "first occurrence", 0, true)
	second := socketValue(handle, "second occurrence", 0, true)

	selected := networkPollIntegrationSelect(t, machine, []value.Value{first, second}, nil, nil, 250)
	requireNetworkPollIntegrationSet(t, selected, "read", first, second)
	received := callBuiltinWithinBound(t, machine, "net_recv", server, value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, received, "data"), value.NewBytes("x"))
}

func TestNetworkPollIntegrationCopiesSocketToEveryMatchingSet(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("x"))

	selected := networkPollIntegrationSelect(t, machine,
		[]value.Value{server}, []value.Value{server}, []value.Value{server}, 250)
	requireNetworkPollIntegrationSet(t, selected, "read", server)
	requireNetworkPollIntegrationSet(t, selected, "write", server)
	requireNetworkPollIntegrationSet(t, selected, "error")
}

func TestNetworkPollIntegrationIgnoresCandidatesAfterIndex63(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("s"))
	callBuiltinWithinBound(t, machine, "net_send", server, value.NewBytes("c"))
	read := make([]value.Value, 65)
	for index := 0; index < 64; index++ {
		read[index] = server
	}
	read[64] = client

	selected := networkPollIntegrationSelect(t, machine, read, nil, nil, 250)
	want := make([]value.Value, 64)
	for index := range want {
		want[index] = server
	}
	requireNetworkPollIntegrationSet(t, selected, "read", want...)
	requireNetworkPollIntegrationSet(t, selected, "write")
	requireNetworkPollIntegrationSet(t, selected, "error")
}

func TestNetworkPollIntegrationZeroTimeoutEmptyPollReturnsPromptly(t *testing.T) {
	machine := New()
	started := time.Now()
	selected := networkPollIntegrationSelect(t, machine, nil, nil, nil, 0)
	elapsed := time.Since(started)
	if elapsed >= 100*time.Millisecond {
		t.Fatalf("zero-time empty poll elapsed=%s, want less than 100ms", elapsed)
	}
	for _, field := range []string{"read", "write", "error"} {
		requireNetworkPollIntegrationSet(t, selected, field)
	}
}

func TestNetworkPollIntegrationPositiveEmptyPollUsesOneGlobalTimeout(t *testing.T) {
	machine := New()
	started := time.Now()
	selected := networkPollIntegrationSelect(t, machine, nil, nil, nil, 80)
	elapsed := time.Since(started)
	if elapsed < 60*time.Millisecond || elapsed > 400*time.Millisecond {
		t.Fatalf("positive empty poll elapsed=%s, want 60-400ms", elapsed)
	}
	for _, field := range []string{"read", "write", "error"} {
		requireNetworkPollIntegrationSet(t, selected, field)
	}
}

func TestNetworkPollIntegrationIdleSocketsUseOneGlobalTimeout(t *testing.T) {
	machine := New()
	servers := make([]value.Value, 8)
	for index := range servers {
		_, _, servers[index] = setupAcceptedLoopback(t, machine)
	}

	started := time.Now()
	selected := networkPollIntegrationSelect(t, machine, servers, nil, nil, 80)
	elapsed := time.Since(started)
	if elapsed < 60*time.Millisecond || elapsed > 400*time.Millisecond {
		t.Fatalf("idle socket poll elapsed=%s, want one 60-400ms timeout", elapsed)
	}
	requireNetworkPollIntegrationSet(t, selected, "read")
}

func TestNetworkPollIntegrationOrderlyEOFIsReadableAndRecvSucceeds(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	callBuiltinWithinBound(t, machine, "net_close", client)

	selected := networkPollIntegrationSelect(t, machine, []value.Value{server}, nil, nil, 500)
	requireNetworkPollIntegrationSet(t, selected, "read", server)
	received := callBuiltinWithinBound(t, machine, "net_recv", server, value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, received, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, received, "count"), value.NewInt(0))
	assertBuiltinValue(t, builtinMapField(t, received, "data"), value.NewBytes(""))
}

func TestNetworkPollIntegrationTCPHalfCloseIsReadable(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	peer := requireNetworkPollIntegrationTCPConn(t, machine, client)
	if err := peer.CloseWrite(); err != nil {
		t.Skipf("TCP half-close is not supported: %v", err)
	}

	selected := networkPollIntegrationSelect(t, machine, []value.Value{server}, nil, nil, 500)
	requireNetworkPollIntegrationSet(t, selected, "read", server)
}

func TestNetworkPollIntegrationTailPrecedesEOF(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("tail"))
	callBuiltinWithinBound(t, machine, "net_close", client)

	selected := networkPollIntegrationSelect(t, machine, []value.Value{server}, nil, nil, 500)
	requireNetworkPollIntegrationSet(t, selected, "read", server)
	tail := callBuiltinWithinBound(t, machine, "net_recv", server, value.NewInt(4))
	assertBuiltinValue(t, builtinMapField(t, tail, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, tail, "count"), value.NewInt(4))
	assertBuiltinValue(t, builtinMapField(t, tail, "data"), value.NewBytes("tail"))

	selected = networkPollIntegrationSelect(t, machine, []value.Value{server}, nil, nil, 500)
	requireNetworkPollIntegrationSet(t, selected, "read", server)
	eof := callBuiltinWithinBound(t, machine, "net_recv", server, value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, eof, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, eof, "count"), value.NewInt(0))
	assertBuiltinValue(t, builtinMapField(t, eof, "data"), value.NewBytes(""))
}

func TestNetworkPollIntegrationCloseWakesAndOmitsLocalResource(t *testing.T) {
	t.Run("socket", func(t *testing.T) {
		machine := New()
		_, _, server := setupAcceptedLoopback(t, machine)
		_, resource := requireSocketResource(t, machine, server)
		native := requireBuiltin(t, machine, "net_select")
		result := make(chan networkInvocationResult, 1)
		go func() {
			selected, err := native.Invoke(machine, []value.Value{
				value.NewArray([]value.Value{server}), value.NewArray(nil), value.NewArray(nil), value.NewInt(5000),
			})
			result <- networkInvocationResult{value: selected, err: err}
		}()
		waitForNetworkPollWaiter(t, func() int {
			resource.stateMu.Lock()
			defer resource.stateMu.Unlock()
			return len(resource.pollWaiters)
		})
		requireNetworkPollIntegrationPromptClose(t, machine, server, result)
	})

	t.Run("listener", func(t *testing.T) {
		machine := New()
		cleanupNetworkResources(t, machine)
		listener := callBuiltinWithinBound(t, machine, "net_listen", value.NewString("127.0.0.1"), value.NewInt(0))
		handle := int(builtinMapField(t, listener, "fd").AsInt)
		resource, exists := machine.shared.Listeners.get(handle)
		if !exists {
			t.Fatalf("listener descriptor %d is not registered", handle)
		}
		native := requireBuiltin(t, machine, "net_select")
		result := make(chan networkInvocationResult, 1)
		go func() {
			selected, err := native.Invoke(machine, []value.Value{
				value.NewArray([]value.Value{listener}), value.NewArray(nil), value.NewArray(nil), value.NewInt(5000),
			})
			result <- networkInvocationResult{value: selected, err: err}
		}()
		waitForNetworkPollWaiter(t, func() int {
			resource.stateMu.Lock()
			defer resource.stateMu.Unlock()
			return len(resource.pollWaiters)
		})
		requireNetworkPollIntegrationPromptClose(t, machine, listener, result)
	})
}

func TestNetworkPollIntegrationConcurrentNetSelect(t *testing.T) {
	machine := New()
	_, client, server := setupAcceptedLoopback(t, machine)
	_, resource := requireSocketResource(t, machine, server)
	const pollCount = 4
	start := make(chan struct{})
	done := make(chan networkInvocationResult, pollCount)
	for index := 0; index < pollCount; index++ {
		current := NewWithShared(machine.shared, machine.Config)
		native := requireBuiltin(t, current, "net_select")
		go func(current *VM, native *value.ObjNative) {
			<-start
			selected, err := native.Invoke(current, []value.Value{
				value.NewArray([]value.Value{server}), value.NewArray(nil), value.NewArray(nil), value.NewInt(5000),
			})
			done <- networkInvocationResult{value: selected, err: err}
		}(current, native)
	}
	close(start)
	waitForNetworkPollWaiter(t, func() int {
		resource.stateMu.Lock()
		defer resource.stateMu.Unlock()
		return len(resource.pollWaiters)
	})
	waitDeadline := time.NewTimer(statefulBuiltinTimeout)
	defer waitDeadline.Stop()
	waitTicker := time.NewTicker(time.Millisecond)
	defer waitTicker.Stop()
	for {
		resource.stateMu.Lock()
		waiters := len(resource.pollWaiters)
		resource.stateMu.Unlock()
		if waiters == pollCount {
			break
		}
		select {
		case <-waitTicker.C:
		case <-waitDeadline.C:
			t.Fatalf("registered poll waiters=%d, want %d", waiters, pollCount)
		}
	}
	sent := callBuiltinWithinBound(t, machine, "net_send", client, value.NewBytes("x"))
	assertBuiltinValue(t, builtinMapField(t, sent, "ok"), value.NewBool(true))

	for i := 0; i < pollCount; i++ {
		select {
		case invocation := <-done:
			if invocation.err != nil {
				t.Fatal(invocation.err)
			}
			requireNetworkPollIntegrationSet(t, invocation.value, "read", server)
		case <-time.After(statefulBuiltinTimeout):
			t.Fatal("concurrent net_select did not complete")
		}
	}
	received := callBuiltinWithinBound(t, machine, "net_recv", server, value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, received, "data"), value.NewBytes("x"))
}
