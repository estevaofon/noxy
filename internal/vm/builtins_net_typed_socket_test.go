package vm

import (
	"io"
	"net"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// Issue #121: net_accept/net_recv/net_send aceitavam so o map que o runtime
// constroi (socketValue) e devolviam null para qualquer outra forma — sob os
// wrappers `-> NetResult`/`-> Socket` da stdlib. Uma dessas formas e o
// `Socket(...)` que net.poll reconstroi, entao socket_recv sobre
// poll(...).read[0] devolvia null. A identidade do socket e o fd registrado,
// nao a forma do valor (networkSocketDescriptor, o mesmo de settimeout).

func typedSocketInstance(fd int) value.Value {
	definition := &value.ObjStruct{Name: "Socket", Fields: []string{"fd", "addr", "port", "open"}}
	instance := value.NewInstance(definition)
	instance.Obj.(*value.ObjInstance).MustSet("fd", value.NewInt(int64(fd)))
	instance.Obj.(*value.ObjInstance).MustSet("addr", value.NewString("127.0.0.1"))
	instance.Obj.(*value.ObjInstance).MustSet("port", value.NewInt(0))
	instance.Obj.(*value.ObjInstance).MustSet("open", value.NewBool(true))
	return instance
}

func TestNetRecvAndSendAcceptTypedSocketInstance(t *testing.T) {
	machine := New()
	connection, peer := net.Pipe()
	t.Cleanup(func() {
		_ = connection.Close()
		_ = peer.Close()
	})
	handle := machine.shared.Sockets.add(&SocketResource{connection: connection})
	t.Cleanup(func() { machine.shared.Sockets.remove(handle) })
	socket := typedSocketInstance(handle)

	go func() { _, _ = peer.Write([]byte("hi")) }()
	received := callBuiltinWithinBound(t, machine, "net_recv", socket, value.NewInt(2))
	assertBuiltinValue(t, builtinMapField(t, received, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, received, "data"), value.NewBytes("hi"))

	go func() {
		buffer := make([]byte, 2)
		_, _ = io.ReadFull(peer, buffer)
	}()
	sent := callBuiltinWithinBound(t, machine, "net_send", socket, value.NewBytes("ok"))
	assertBuiltinValue(t, builtinMapField(t, sent, "ok"), value.NewBool(true))
	assertBuiltinValue(t, builtinMapField(t, sent, "count"), value.NewInt(2))
}

// Socket com a forma certa mas fd fora do registro (construido pelo
// programa, ou ja fechado): resultado tipado — ok=false / open=false — como
// sempre foi para o map do runtime. Nunca null.
func TestNetWrappersOnUnregisteredSocketReturnTypedFailure(t *testing.T) {
	machine := New()
	ghost := typedSocketInstance(9999)

	received := callBuiltinWithinBound(t, machine, "net_recv", ghost, value.NewInt(2))
	assertBuiltinValue(t, builtinMapField(t, received, "ok"), value.NewBool(false))
	assertBuiltinValue(t, builtinMapField(t, received, "error"), value.NewString("invalid socket"))

	sent := callBuiltinWithinBound(t, machine, "net_send", ghost, value.NewBytes("x"))
	assertBuiltinValue(t, builtinMapField(t, sent, "ok"), value.NewBool(false))
	assertBuiltinValue(t, builtinMapField(t, sent, "error"), value.NewString("invalid socket"))

	accepted := callBuiltinWithinBound(t, machine, "net_accept", ghost)
	assertBuiltinValue(t, builtinMapField(t, accepted, "open"), value.NewBool(false))
	assertBuiltinValue(t, builtinMapField(t, accepted, "fd"), value.NewInt(-1))
}

// Valor que nao tem a forma de socket (sem campo fd int): erro tipado, a
// mesma mensagem de net_settimeout — nunca null.
func TestNetRecvSendAcceptRejectNonSocketWithError(t *testing.T) {
	machine := New()
	invalid := []value.Value{
		value.NewString("not a socket"),
		value.NewMap(),
		value.NewMapWithData(map[string]value.Value{"fd": value.NewString("1")}),
		{Type: value.VAL_OBJ, Obj: (*value.ObjInstance)(nil)},
	}
	for _, socket := range invalid {
		for _, name := range []string{"net_recv", "net_send", "net_accept"} {
			args := []value.Value{socket}
			if name != "net_accept" {
				args = append(args, value.NewInt(1))
			}
			if _, err := invokeBuiltin(t, machine, name, args...); err == nil || !strings.Contains(err.Error(), "invalid socket") {
				t.Fatalf("%s(%#v) error = %v, want invalid socket", name, socket, err)
			}
		}
	}
	for name, args := range map[string][]value.Value{
		"net_recv":   {typedSocketInstance(1)},
		"net_send":   {typedSocketInstance(1)},
		"net_accept": {},
	} {
		if _, err := invokeBuiltin(t, machine, name, args...); err == nil || !strings.Contains(err.Error(), name+" expects exactly") {
			t.Fatalf("%s with %d args error = %v, want an arity error", name, len(args), err)
		}
	}
}

func captureNetProgram(t *testing.T, source string) value.Value {
	t.Helper()
	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if err := interpretVMSourceWithinBound(t, machine, source); err != nil {
		t.Fatal(err)
	}
	return captured
}

// Fluxo completo em loopback pela stdlib: o Socket que sai de
// net.poll(...).read[0] (instancia reconstruida em net.nx) recebe os bytes.
func TestNetRecvOnPolledSocketDeliversData(t *testing.T) {
	got := captureNetProgram(t, `use net
let listener: net.Socket = net.listen("127.0.0.1", 0)
let client: net.Socket = net.connect("127.0.0.1", listener.port)
let server: net.Socket = net.accept(listener)
net.settimeout(server, 2000)
let sent: net.NetResult = net.socket_send(client, b"poll-data")
let server_read: net.Socket?[64] = net.socket_set()
let empty: net.Socket?[64] = net.socket_set()
server_read[0] = server
let ready: net.SelectResult = net.poll(server_read, empty, empty, 2000)
let score: int = 0
if sent.ok && sent.count == 9 then
    score = score + 1
end
if ready.read_count == 1 then
    let polled: net.Socket = ready.read[0]
    let received: net.NetResult = net.socket_recv(polled, 64)
    if received.ok && received.data == b"poll-data" then
        score = score + 10
    end
end
net.socket_close(server)
net.socket_close(client)
net.socket_close(listener)
test_report(score)`)
	testExpectedObject(t, 11, got)
}

func TestNetWrappersOnProgramBuiltSocketNeverReturnNull(t *testing.T) {
	got := captureNetProgram(t, `use net
let ghost: net.Socket = net.Socket(9999, "x", 0, true)
let received: net.NetResult = net.socket_recv(ghost, 10)
let sent: net.NetResult = net.socket_send(ghost, b"hi")
let accepted: net.Socket = net.accept(ghost)
let score: int = 0
if !received.ok && received.error == "invalid socket" then
    score = score + 1
end
if !sent.ok && sent.error == "invalid socket" then
    score = score + 10
end
if !accepted.open && accepted.fd == -1 then
    score = score + 100
end
test_report(score)`)
	testExpectedObject(t, 111, got)
}
