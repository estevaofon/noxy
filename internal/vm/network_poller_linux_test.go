//go:build linux

package vm

import (
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"noxy-vm/internal/value"
)

func TestUnixReadHangupMatchesLinuxPOLLRDHUP(t *testing.T) {
	if networkPollReadHangup != unix.POLLRDHUP {
		t.Fatalf("networkPollReadHangup=%#x, want POLLRDHUP=%#x", networkPollReadHangup, unix.POLLRDHUP)
	}
	if got := normalizeUnixPollEvents(unix.POLLRDHUP); got != networkReadReady|networkWriteReady|networkErrorReady {
		t.Fatalf("normalized POLLRDHUP=%#x", got)
	}
}

func TestLinuxNetworkPollCloseWriteReportsReadHangup(t *testing.T) {
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
	if err := peer.CloseWrite(); err != nil {
		t.Fatal(err)
	}

	serverHandle := int(builtinMapField(t, server, "fd").AsInt)
	serverResource, exists := machine.shared.Sockets.get(serverHandle)
	if !exists {
		t.Fatalf("server descriptor %d is not registered", serverHandle)
	}
	serverResource.stateMu.Lock()
	serverConnection := serverResource.connection
	serverResource.stateMu.Unlock()
	raw, err := serverConnection.(syscall.Conn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var revents int16
	if err := raw.Control(func(descriptor uintptr) {
		pollfds := []unix.PollFd{{Fd: int32(descriptor), Events: unix.POLLIN | unix.POLLRDHUP}}
		if _, pollErr := unix.Poll(pollfds, 1000); pollErr != nil {
			err = pollErr
			return
		}
		revents = pollfds[0].Revents
	}); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if revents&unix.POLLRDHUP == 0 {
		t.Fatalf("raw poll revents=%#x, want POLLRDHUP", revents)
	}

	selected := callBuiltinWithinBound(t, machine, "net_select",
		value.NewArray([]value.Value{server}), value.NewArray(nil), value.NewArray([]value.Value{server}), value.NewInt(1000))
	assertBuiltinValue(t, builtinMapField(t, selected, "read_count"), value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, selected, "error_count"), value.NewInt(1))
}

func TestLinuxNetworkPollResetPreservesPendingSocketError(t *testing.T) {
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

	poller := networkPoller{platform: systemNetworkPlatform(), now: time.Now, sleep: time.Sleep}
	sets := [3]*value.ObjArray{
		{Elements: []value.Value{server}},
		{Elements: []value.Value{server}},
		{Elements: []value.Value{server}},
	}
	type pollResult struct {
		value value.Value
		err   error
	}
	result := make(chan pollResult, 1)
	go func() {
		selected, err := poller.Poll(machine.shared, sets, time.Second)
		result <- pollResult{value: selected, err: err}
	}()
	var selected value.Value
	select {
	case outcome := <-result:
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}
		selected = outcome.value
	case <-time.After(time.Second):
		t.Fatal("poll did not report loopback RST within one second")
	}
	assertBuiltinValue(t, builtinMapField(t, selected, "read_count"), value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, selected, "write_count"), value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, selected, "error_count"), value.NewInt(1))

	received := callBuiltinWithinBound(t, machine, "net_recv", server, value.NewInt(1))
	assertBuiltinValue(t, builtinMapField(t, received, "ok"), value.NewBool(false))
	message := builtinMapField(t, received, "error").String()
	if message == "" || strings.Contains(strings.ToLower(message), "timeout") {
		t.Fatalf("net_recv error=%q, want non-timeout connection error", message)
	}
}
