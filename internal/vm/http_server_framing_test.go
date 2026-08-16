package vm

import (
	"testing"

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
