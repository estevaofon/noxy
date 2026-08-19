// internal/vm/builtins_call_result_test.go
package vm

import (
	"testing"
)

func TestErrorsModuleShapes(t *testing.T) {
	source := `
use errors select *

let f: Failure = Failure("runtime", "boom", "st", [])
let nested: Failure = Failure("runtime", "outer", "st", [f])
let r: CallResult = CallResult(true, 42, null)
test_report(nested.causes[0].message + "|" + to_str(r.ok) + "|" + to_str(r.value))
`
	reported := captureVMSource(t, source)
	text, ok := reported.Obj.(string)
	if !ok || text != "boom|true|42" {
		t.Fatalf("unexpected report: %#v", reported)
	}
}
