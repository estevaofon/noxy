package vm

import (
	"fmt"
	"net"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

func captureServerInt(t *testing.T, body string) int64 {
	t.Helper()
	captured := captureVMSource(t, "use http_server select *\nuse http_parser select *\n"+body)
	if captured.Type != value.VAL_INT {
		t.Fatalf("test_report value = %#v, want int", captured)
	}
	return captured.AsInt
}

func captureServerBool(t *testing.T, body string) bool {
	t.Helper()
	captured := captureVMSource(t, "use http_server select *\nuse http_parser select *\n"+body)
	if captured.Type != value.VAL_BOOL {
		t.Fatalf("test_report value = %#v, want bool", captured)
	}
	return captured.AsBool
}

func TestNewServerInstallsDefaults(t *testing.T) {
	tests := []struct {
		field string
		want  int64
	}{
		{field: "max_header_bytes", want: 16384},
		{field: "max_body_bytes", want: 1048576},
		{field: "header_timeout_ms", want: 5000},
		{field: "body_timeout_ms", want: 15000},
		{field: "write_timeout_ms", want: 15000},
		{field: "read_chunk_bytes", want: 8192},
	}
	for _, test := range tests {
		t.Run(test.field, func(t *testing.T) {
			source := "let s: HttpServer = new_server(\"127.0.0.1\", 8080)\ntest_report(s." + test.field + ")"
			if got := captureServerInt(t, source); got != test.want {
				t.Fatalf("%s = %d, want %d", test.field, got, test.want)
			}
		})
	}
}

// server_limits, bind_server, and stop_server take `ref HttpServer`.
// `use http_server select *` erases the exact static signature at this call
// site (see docs/REF_SEMANTICS.md section 2: a module member whose exact
// type isn't known is a dynamic boundary), so the contextual ref conversion
// that lets same-file callers write a plain local variable does not apply
// here. Every call below is explicit `ref s` for that reason; omitting it
// raises "expected ref HttpServer, got object".

func TestServerLimitsReplacesNonPositiveWithDefaults(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 8080)
s.max_header_bytes = 0
s.max_body_bytes = -1
s.header_timeout_ms = 0
s.body_timeout_ms = -7
s.write_timeout_ms = 0
s.read_chunk_bytes = -3
let limits: HttpLimits = server_limits(ref s)
test_report(limits.max_header_bytes + limits.max_body_bytes + limits.header_timeout_ms + limits.body_timeout_ms + limits.write_timeout_ms + limits.read_chunk_bytes)`
	want := int64(16384 + 1048576 + 5000 + 15000 + 15000 + 8192)
	if got := captureServerInt(t, source); got != want {
		t.Fatalf("sum = %d, want %d", got, want)
	}
}

func TestServerLimitsClampsChunkToHeaderBudget(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 8080)
s.max_header_bytes = 512
let limits: HttpLimits = server_limits(ref s)
test_report(limits.read_chunk_bytes)`
	if got := captureServerInt(t, source); got != 512 {
		t.Fatalf("read_chunk_bytes = %d, want 512", got)
	}
}

func TestServerLimitsKeepsConfiguredValues(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 8080)
s.max_body_bytes = 4096
let limits: HttpLimits = server_limits(ref s)
test_report(limits.max_body_bytes)`
	if got := captureServerInt(t, source); got != 4096 {
		t.Fatalf("max_body_bytes = %d, want 4096", got)
	}
}

func TestBindServerWritesEphemeralPortBack(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 0)
let ok: bool = bind_server(ref s)
if !ok then
    test_report(0)
else
    let assigned: int = s.port
    stop_server(ref s)
    if assigned > 0 then test_report(1) else test_report(0) end
end`
	if got := captureServerInt(t, source); got != 1 {
		t.Fatal("bind_server did not write a positive bound port back to server.port")
	}
}

func TestBindServerIsIdempotent(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 0)
let first: bool = bind_server(ref s)
let assigned: int = s.port
let second: bool = bind_server(ref s)
let same: bool = s.port == assigned
stop_server(ref s)
test_report(first && second && same)`
	if !captureServerBool(t, source) {
		t.Fatal("a second bind_server call rebound the listener")
	}
}

// send_all must resume from the transferred offset after a partial write,
// not just retry the whole buffer, and must bound the whole response with
// one write deadline rather than resetting it on every partial write. A
// slow-reading real TCP peer is what actually forces socket_send to return
// a partial count -- a fake/mocked socket would not exercise that.
func TestSendAllWritesCompletelyToSlowReader(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	const payloadSize = 4 << 20
	received := make(chan int, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			received <- -1
			return
		}
		defer conn.Close()
		total := 0
		buffer := make([]byte, 4096)
		for total < payloadSize {
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, readErr := conn.Read(buffer)
			total += n
			if readErr != nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		received <- total
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	source := fmt.Sprintf(`use http_server select *
use net select *
use strings select *
let sock: Socket = connect("127.0.0.1", %d)
let payload: bytes = to_bytes(repeat("x", %d))
let ok: bool = send_all(sock, payload, 20000)
socket_close(sock)
test_report(ok)`, port, payloadSize)

	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if interpretErr := interpretVMSourceWithinBound(t, machine, source); interpretErr != nil {
		t.Fatal(interpretErr)
	}
	if captured.Type != value.VAL_BOOL || !captured.AsBool {
		t.Fatalf("send_all = %#v, want true", captured)
	}

	select {
	case total := <-received:
		if total != payloadSize {
			t.Fatalf("peer received %d bytes, want %d", total, payloadSize)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("peer did not finish reading")
	}
}

// A write that transfers nothing and fails must abort rather than retry
// forever, and once the deadline governs the whole call, a peer that never
// reads must not hang the caller indefinitely either.
func TestSendAllFailsWhenPeerIsGone(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	closed := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			// A graceful close (FIN) lets the local OS keep accepting writes
			// into its send buffer for a while -- on this platform's loopback,
			// long enough to swallow the whole 4 MB payload without ever
			// blocking, which would make send_all report success despite the
			// peer being gone. SetLinger(0) sends an immediate RST instead, so
			// the client's next write fails deterministically rather than
			// depending on OS-specific buffer sizing.
			if tcpConn, ok := conn.(*net.TCPConn); ok {
				_ = tcpConn.SetLinger(0)
			}
			_ = conn.Close()
		}
		_ = listener.Close()
		close(closed)
	}()

	source := fmt.Sprintf(`use http_server select *
use net select *
use strings select *
use time select *
let sock: Socket = connect("127.0.0.1", %d)
sleep(200)
let payload: bytes = to_bytes(repeat("y", 4194304))
let ok: bool = send_all(sock, payload, 2000)
socket_close(sock)
test_report(ok)`, port)

	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if interpretErr := interpretVMSourceWithinBound(t, machine, source); interpretErr != nil {
		t.Fatal(interpretErr)
	}
	<-closed
	if captured.Type != value.VAL_BOOL || captured.AsBool {
		t.Fatalf("send_all = %#v, want false", captured)
	}
}
