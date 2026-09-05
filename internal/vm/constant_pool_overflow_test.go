package vm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// OP_GET_GLOBAL, OP_SET_GLOBAL, OP_GET_PROPERTY, OP_SET_PROPERTY, OP_IMPORT,
// OP_CLOSURE, OP_REF_GLOBAL, OP_REF_PROPERTY, and OP_CONTEXT_REF_PROPERTY used
// to encode their constant-pool index as a single byte. AddConstant never
// deduplicates -- every distinct string literal and every reference to a
// global name (not just its declaration) claims its own slot -- so a chunk
// with more than 255 constants silently truncated the 256th index (256 mod
// 256 = 0) and read whatever constant happened to sit at index 0 instead.
// 255 constants is not a large program: an ordinary 85-line stdlib example
// script already carries that many once each PASS/FAIL message, each
// identifier lookup, and each struct field name is counted individually.
//
// This test builds a script with enough distinct top-level globals to push
// the constant pool past 255 and confirms both a global declared EARLY (near
// index 0, where a truncated high index would wrongly land) and one declared
// LATE (only reachable through the correct wide index) still resolve to their
// own values.
func TestGlobalResolutionSurvivesLargeConstantPool(t *testing.T) {
	const junkCount = 200 // each junk global costs 2 constants (name, literal)

	var body strings.Builder
	body.WriteString("let first: int = 111\n")
	for i := range junkCount {
		fmt.Fprintf(&body, "let junk%d: int = %d\n", i, i)
	}
	body.WriteString("let last: int = 222\n")
	body.WriteString("test_report(first * 1000 + last)\n")

	captured := captureVMSource(t, body.String())
	if captured.Type != value.VAL_INT {
		t.Fatalf("test_report value = %#v, want int", captured)
	}
	if captured.Int() != 111222 {
		t.Fatalf("first*1000+last = %d, want 111222 (first or last resolved to the wrong global)", captured.Int())
	}
}

// The same truncation hazard applies to struct field access (OP_GET_PROPERTY
// / OP_SET_PROPERTY): each struct literal's field-name constants and each
// property access site add their own entries.
func TestPropertyAccessSurvivesLargeConstantPool(t *testing.T) {
	const junkCount = 200

	var body strings.Builder
	body.WriteString("struct Box\n    value: int\nend\n")
	body.WriteString("let target: Box = Box(777)\n")
	for i := range junkCount {
		fmt.Fprintf(&body, "let junk%d: int = %d\n", i, i)
	}
	body.WriteString("target.value = target.value + 1\n")
	body.WriteString("test_report(target.value)\n")

	captured := captureVMSource(t, body.String())
	if captured.Type != value.VAL_INT || captured.Int() != 778 {
		t.Fatalf("target.value = %#v, want 778", captured)
	}
}

// Function declarations (OP_CLOSURE, which also carries a constant-pool
// index for the function object) must survive the same pressure, and the
// call must dispatch to the CORRECT function once enough of them exist to
// cross the same boundary.
func TestFunctionCallSurvivesLargeConstantPool(t *testing.T) {
	const junkFuncCount = 150

	var body strings.Builder
	body.WriteString("func first_fn() -> int\n    return 111\nend\n")
	for i := range junkFuncCount {
		fmt.Fprintf(&body, "func junk_fn%d() -> int\n    return %d\nend\n", i, i)
	}
	body.WriteString("func last_fn() -> int\n    return 222\nend\n")
	body.WriteString("test_report(first_fn() * 1000 + last_fn())\n")

	captured := captureVMSource(t, body.String())
	if captured.Type != value.VAL_INT || captured.Int() != 111222 {
		t.Fatalf("first_fn()*1000+last_fn() = %#v, want 111222", captured)
	}
}
