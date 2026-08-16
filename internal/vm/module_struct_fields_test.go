package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// A struct defined in the importing module with a field typed as a struct
// imported from another module (e.g. `listener: Socket` from `net`, which is
// exactly HttpServer's shape) must be constructible. Before this fix, the
// compiler's cross-module struct registry (c.structs) was populated only from
// structs defined in the SAME compilation unit; `use net select *` bound
// Socket as a value (c.globals["Socket"] = nil) but never registered its AST
// field layout, so building the constructor's runtime type schema for a
// wrapper struct silently failed and every call to the wrapper's constructor
// raised "struct constructor has incomplete runtime type metadata" -- always,
// unconditionally, for any struct with an imported struct-typed field.
func TestStructWithImportedStructFieldIsConstructible(t *testing.T) {
	source := `use net select *

struct Wrapper
    listener: Socket
    running: bool
end

func mk() -> Wrapper
    let sock: Socket = Socket(-1, "", 0, false)
    let w: Wrapper = Wrapper(sock, false)
    return w
end

let w: Wrapper = mk()
test_report(w.listener.fd)`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_INT || captured.AsInt != -1 {
		t.Fatalf("w.listener.fd = %#v, want -1", captured)
	}
}

// The exact real-world shape: HttpServer embeds net's Socket. This is the bug
// that made new_server() unusable before the fix.
func TestHttpServerConstructorWorks(t *testing.T) {
	source := `use http_server select *
let s: HttpServer = new_server("127.0.0.1", 8080)
test_report(s.port)`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_INT || captured.AsInt != 8080 {
		t.Fatalf("s.port = %#v, want 8080", captured)
	}
}

// Selective imports (`use pkg select a, b`) must register struct field
// layouts too, not just wildcard imports.
func TestStructWithSelectivelyImportedStructFieldIsConstructible(t *testing.T) {
	source := `use net select Socket

struct Wrapper
    listener: Socket
end

let sock: Socket = Socket(7, "", 0, true)
let w: Wrapper = Wrapper(sock)
test_report(w.listener.fd)`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_INT || captured.AsInt != 7 {
		t.Fatalf("w.listener.fd = %#v, want 7", captured)
	}
}
