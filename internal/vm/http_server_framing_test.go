package vm

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
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
	return captured.Int()
}

func captureServerBool(t *testing.T, body string) bool {
	t.Helper()
	captured := captureVMSource(t, "use http_server select *\nuse http_parser select *\n"+body)
	if captured.Type != value.VAL_BOOL {
		t.Fatalf("test_report value = %#v, want bool", captured)
	}
	return captured.Bool()
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
	if captured.Type != value.VAL_BOOL || !captured.Bool() {
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
	if captured.Type != value.VAL_BOOL || captured.Bool() {
		t.Fatalf("send_all = %#v, want false", captured)
	}
}

type noxyHTTPServer struct {
	t       *testing.T
	port    int
	release chan struct{}
	done    chan error
	once    sync.Once
}

// startNoxyHTTPServer runs the Noxy HTTP server on an ephemeral port.
// handlerBody is Noxy source for the body of `func handler(req: HttpRequest) -> HttpResponse`.
// config is Noxy source assigning fields on the `server` variable, e.g. `server.body_timeout_ms = 400`.
//
// bind_server, serve, and stop_server all take `ref HttpServer`. This
// harness calls them through `use http_server select *`, which erases the
// exact static signature at the call site (docs/REF_SEMANTICS.md section
// 2), so every call below is explicit `ref server` -- the plain form only
// works for a same-file caller who compiles against the real signature.
func startNoxyHTTPServer(t *testing.T, handlerBody string, config string) *noxyHTTPServer {
	t.Helper()

	harness := &noxyHTTPServer{
		t:       t,
		release: make(chan struct{}),
		done:    make(chan error, 1),
	}
	ready := make(chan int, 1)

	machine := New()
	machine.DefineNative("harness_ready", func(args []value.Value) value.Value {
		port := 0
		if len(args) == 1 {
			port = int(args[0].Int())
		}
		ready <- port
		return value.NewNull()
	})
	machine.DefineNative("harness_wait", func([]value.Value) value.Value {
		<-harness.release
		return value.NewNull()
	})

	source := `use http_server select *
use http_parser select *

func handler(req: HttpRequest) -> HttpResponse
` + handlerBody + `
end

func serve_loop()
    serve(ref server, handler)
end

let server: HttpServer = new_server("127.0.0.1", 0)
` + config + `
if bind_server(ref server) then
    spawn(serve_loop)
    harness_ready(server.port)
    harness_wait()
    stop_server(ref server)
else
    harness_ready(0)
    harness_wait()
end
`

	code := compileVMSource(t, source)
	go func() {
		harness.done <- machine.Interpret(code)
	}()

	select {
	case port := <-ready:
		if port <= 0 {
			harness.stop()
			t.Fatal("noxy http server failed to bind")
		}
		harness.port = port
	case <-time.After(10 * time.Second):
		harness.stop()
		t.Fatal("noxy http server did not report its port")
	}

	t.Cleanup(harness.stop)
	return harness
}

func (h *noxyHTTPServer) stop() {
	h.once.Do(func() {
		close(h.release)
		select {
		case err := <-h.done:
			if err != nil {
				h.t.Errorf("noxy http server exited with error: %v", err)
			}
		case <-time.After(10 * time.Second):
			h.t.Error("noxy http server did not shut down")
		}
	})
}

func (h *noxyHTTPServer) dial() net.Conn {
	h.t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", h.port), 5*time.Second)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	return conn
}

// readRawResponse reads until EOF, which the server guarantees by closing after one response.
func readRawResponse(t *testing.T, conn net.Conn) string {
	t.Helper()
	var builder strings.Builder
	buffer := make([]byte, 4096)
	for {
		n, err := conn.Read(buffer)
		builder.Write(buffer[:n])
		if err != nil {
			break
		}
	}
	return builder.String()
}

func responseStatus(t *testing.T, raw string) int {
	t.Helper()
	if raw == "" {
		return 0
	}
	line, _, found := strings.Cut(raw, "\r\n")
	if !found {
		t.Fatalf("response has no status line: %q", raw)
	}
	fields := strings.Split(line, " ")
	if len(fields) < 2 {
		t.Fatalf("malformed status line: %q", line)
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("status line %q: %v", line, err)
	}
	return code
}

func responseBody(t *testing.T, raw string) string {
	t.Helper()
	_, body, found := strings.Cut(raw, "\r\n\r\n")
	if !found {
		t.Fatalf("response has no body separator: %q", raw)
	}
	return body
}

const echoHandler = `    if req.path == "/echo" then
        return response_text(to_str(req.body))
    end
    if req.path == "/len" then
        return response_text(to_str(length(req.body)))
    end
    if req.path == "/query" then
        return response_text(req.query)
    end
    return response_text("ok")`

func TestServerFramesRequestDeliveredOneByteAtATime(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	request := "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 11\r\n\r\nhello world"
	for i := 0; i < len(request); i++ {
		if _, err := conn.Write([]byte(request[i : i+1])); err != nil {
			t.Fatal(err)
		}
	}
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := responseBody(t, raw); got != "hello world" {
		t.Fatalf("body = %q, want %q", got, "hello world")
	}
}

func TestServerFramesRequestSplitInsideTerminator(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	head := "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 5\r\n\r"
	if _, err := conn.Write([]byte(head)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := conn.Write([]byte("\nabcde")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := responseBody(t, raw); got != "abcde" {
		t.Fatalf("body = %q, want %q", got, "abcde")
	}
}

func TestServerReadsBodyLargerThanReadChunk(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "server.read_chunk_bytes = 64")
	conn := server.dial()
	payload := strings.Repeat("z", 5000)
	request := fmt.Sprintf("POST /len HTTP/1.1\r\nHost: a\r\nContent-Length: %d\r\n\r\n%s", len(payload), payload)
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := responseBody(t, raw); got != "5000" {
		t.Fatalf("body = %q, want %q", got, "5000")
	}
}

func TestServerReadsBodyDeliveredInSegments(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	if _, err := conn.Write([]byte("POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 9\r\n\r\nabc")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := conn.Write([]byte("def")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := conn.Write([]byte("ghi")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseBody(t, raw); got != "abcdefghi" {
		t.Fatalf("body = %q, want %q", got, "abcdefghi")
	}
}

func TestServerTreatsMissingContentLengthAsEmptyBody(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	if _, err := conn.Write([]byte("POST /len HTTP/1.1\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := responseBody(t, raw); got != "0" {
		t.Fatalf("body = %q, want %q", got, "0")
	}
}

func TestServerIgnoresSurplusPipelinedBytes(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	request := "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 2\r\n\r\nokGET /extra HTTP/1.1\r\nHost: a\r\n\r\n"
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseBody(t, raw); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
	if strings.Count(raw, "HTTP/1.1 ") != 1 {
		t.Fatalf("expected exactly one response, got %q", raw)
	}
}

func TestServerParsesQuery(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	if _, err := conn.Write([]byte("GET /query?a=1&b=2 HTTP/1.1\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseBody(t, raw); got != "a=1&b=2" {
		t.Fatalf("body = %q, want %q", got, "a=1&b=2")
	}
}

func TestServerRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		request string
		want    int
	}{
		{name: "header block too large", config: "server.max_header_bytes = 256", request: "GET / HTTP/1.1\r\nX-Big: " + strings.Repeat("p", 400) + "\r\n\r\n", want: 431},
		{name: "body too large", config: "server.max_body_bytes = 16", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 64\r\n\r\n", want: 413},
		{name: "duplicate content length", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 5\r\nContent-Length: 5\r\n\r\nabcde", want: 400},
		{name: "signed content length", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: +5\r\n\r\nabcde", want: 400},
		{name: "chunked encoding", request: "POST /echo HTTP/1.1\r\nHost: a\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n", want: 501},
		{name: "chunked with length", request: "POST /echo HTTP/1.1\r\nHost: a\r\nTransfer-Encoding: chunked\r\nContent-Length: 5\r\n\r\nabcde", want: 400},
		{name: "unsupported version", request: "GET / HTTP/2.0\r\nHost: a\r\n\r\n", want: 505},
		{name: "obs fold", request: "GET / HTTP/1.1\r\nHost: a\r\n more\r\n\r\n", want: 400},
		{name: "space before colon", request: "GET / HTTP/1.1\r\nHost : a\r\n\r\n", want: 400},
		{name: "target with space", request: "GET /a b HTTP/1.1\r\nHost: a\r\n\r\n", want: 400},
		{name: "absolute form target", request: "GET http://a/b HTTP/1.1\r\nHost: a\r\n\r\n", want: 400},
		{name: "target too long", request: "GET /" + strings.Repeat("q", 2100) + " HTTP/1.1\r\nHost: a\r\n\r\n", want: 414},
		// The regression that the UTF-8 invariant introduces if left ungated:
		// to_str raises inside the spawned routine, the client is left hanging,
		// and the VM prints Thread Error to the server's stdout.
		{name: "non utf8 header value", request: "GET / HTTP/1.1\r\nX-Bad: \xff\xfe\r\nHost: a\r\n\r\n", want: 400},
		{name: "non utf8 target", request: "GET /\xff HTTP/1.1\r\nHost: a\r\n\r\n", want: 400},
		{name: "content length above int64", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 9999999999999999999\r\n\r\n", want: 400},
		// Response splitting. A bare LF survives the \r\n split, so without the
		// is_field_text rule these reach a handler intact and any handler that
		// echoes the value into a response header forges a second response.
		{name: "bare lf in header value", request: "GET / HTTP/1.1\r\nX-Echo: a\nInjected: yes\r\nHost: a\r\n\r\n", want: 400},
		{name: "bare cr in header value", request: "GET / HTTP/1.1\r\nX-Echo: a\rInjected: yes\r\nHost: a\r\n\r\n", want: 400},
		{name: "nul in header value", request: "GET / HTTP/1.1\r\nX-Echo: a\x00b\r\nHost: a\r\n\r\n", want: 400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startNoxyHTTPServer(t, echoHandler, test.config)
			conn := server.dial()
			if _, err := conn.Write([]byte(test.request)); err != nil {
				t.Fatal(err)
			}
			raw := readRawResponse(t, conn)
			if got := responseStatus(t, raw); got != test.want {
				t.Fatalf("status = %d, want %d (response %q)", got, test.want, raw)
			}
			if !strings.Contains(raw, "Connection: close") {
				t.Fatalf("response is missing Connection: close: %q", raw)
			}
			declared := fmt.Sprintf("Content-Length: %d\r\n", len(responseBody(t, raw)))
			if !strings.Contains(raw, declared) {
				t.Fatalf("response does not declare %q: %q", declared, raw)
			}
		})
	}
}

func TestServerRejectsEofMidRequest(t *testing.T) {
	tests := []struct {
		name    string
		request string
	}{
		{name: "eof mid header", request: "GET / HTTP/1.1\r\nHost: a"},
		{name: "eof mid body", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 10\r\n\r\nabc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startNoxyHTTPServer(t, echoHandler, "")
			conn := server.dial()
			if _, err := conn.Write([]byte(test.request)); err != nil {
				t.Fatal(err)
			}
			if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatal(err)
			}
			raw := readRawResponse(t, conn)
			if got := responseStatus(t, raw); got != 400 {
				t.Fatalf("status = %d, want 400 (response %q)", got, raw)
			}
		})
	}
}

func TestServerTimesOutStalledClients(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		request string
	}{
		{name: "stalled mid header", config: "server.header_timeout_ms = 400", request: "GET / HTTP/1.1\r\nHost: a\r\n"},
		{name: "stalled mid body", config: "server.body_timeout_ms = 400", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 10\r\n\r\nabc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startNoxyHTTPServer(t, echoHandler, test.config)
			conn := server.dial()
			started := time.Now()
			if _, err := conn.Write([]byte(test.request)); err != nil {
				t.Fatal(err)
			}
			raw := readRawResponse(t, conn)
			if got := responseStatus(t, raw); got != 408 {
				t.Fatalf("status = %d, want 408 (response %q)", got, raw)
			}
			if elapsed := time.Since(started); elapsed > 10*time.Second {
				t.Fatalf("timeout took %s, want a bounded wait", elapsed)
			}
		})
	}
}

func TestServerTimesOutSlowlorisTrickle(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "server.header_timeout_ms = 600")
	conn := server.dial()
	request := "GET / HTTP/1.1\r\nHost: a\r\nX-Pad: 0123456789\r\n\r\n"
	go func() {
		for i := 0; i < len(request); i++ {
			if _, err := conn.Write([]byte(request[i : i+1])); err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 408 {
		t.Fatalf("status = %d, want 408 (response %q)", got, raw)
	}
}

func TestServerClosesSilentlyOnEmptyConnection(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if raw := readRawResponse(t, conn); raw != "" {
		t.Fatalf("response = %q, want no bytes", raw)
	}
}

func TestServerClosesConnectionWhenHandlerFails(t *testing.T) {
	failing := `    if req.path == "/boom" then
        let numbers: int[2]
        return response_text(to_str(numbers[9]))
    end
    return response_text("ok")`
	server := startNoxyHTTPServer(t, failing, "")

	boom := server.dial()
	if _, err := boom.Write([]byte("GET /boom HTTP/1.1\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = readRawResponse(t, boom)

	survivor := server.dial()
	if _, err := survivor.Write([]byte("GET /ok HTTP/1.1\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, survivor)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status after failing handler = %d, want 200 (response %q)", got, raw)
	}
}

// The end-to-end response-splitting proof: a handler that echoes a received
// header value into a response header is the realistic vulnerable shape, and
// the request that would exploit it must never reach that handler.
func TestServerRejectsResponseSplittingAttempt(t *testing.T) {
	echoing := `    let out: string[64]
    out[0] = "Content-Type: text/plain"
    out[1] = "X-Echo: " + get_header(req.headers, req.header_count, "X-Echo")
    out[2] = "Content-Length: 2"
    out[3] = "Connection: close"
    return HttpResponse("HTTP/1.1", 200, "OK", out, 4, b"ok")`
	server := startNoxyHTTPServer(t, echoing, "")
	conn := server.dial()
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nX-Echo: a\r\nContent-Length: 0\r\n\r\nHTTP/1.1 200 OK\r\n\r\nInjected: yes\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if strings.Count(raw, "HTTP/1.1 ") != 1 {
		t.Fatalf("expected exactly one response, got %q", raw)
	}
	if strings.Contains(raw, "Injected") {
		t.Fatalf("injected header reached the response: %q", raw)
	}
}

// A non-UTF-8 request must be an ordinary rejection, not an event that takes
// the connection routine down. Ungated, to_str raises: this client would hang
// with no response and the next one would still be served, so the assertion
// that matters is that THIS connection is answered.
func TestServerSurvivesNonUTF8Request(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")

	bad := server.dial()
	if _, err := bad.Write([]byte("GET / HTTP/1.1\r\nX-Bad: \xff\xfe\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if got := responseStatus(t, readRawResponse(t, bad)); got != 400 {
		t.Fatalf("status = %d, want 400", got)
	}

	survivor := server.dial()
	if _, err := survivor.Write([]byte("GET /ok HTTP/1.1\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, survivor)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status after non-UTF-8 request = %d, want 200 (response %q)", got, raw)
	}
}
