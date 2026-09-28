// internal/vm/builtins_sys_executable_test.go
package vm

import (
	"os"
	"testing"

	"github.com/noxylang/noxy/internal/value"
)

func TestSysExecutableIsTheRunningBinary(t *testing.T) {
	want, err := os.Executable()
	if err != nil {
		t.Skip("os.Executable unavailable here")
	}
	got := captureVMSourceAtRoot(t, t.TempDir(), "use sys\ntest_report(sys.executable())\n")
	assertBuiltinValue(t, got, value.NewString(want))
}
