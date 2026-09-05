package vm

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
	"github.com/estevaofon/noxy/internal/version"
)

func TestSysVersionNativeReportsBuildVersion(t *testing.T) {
	machine := New()
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_version"), value.NewString(version.Version))
}

func TestSysModuleExposesVersionBinding(t *testing.T) {
	// `use sys` + `sys.version`: a mesma string de `noxy --version`, sem
	// chamada — um `let` de topo do modulo, como http_parser.HTTP_200_OK.
	got := runTypedFunctionProgram(t, `
use sys
test_report(sys.version)`)
	assertBuiltinValue(t, got, value.NewString(version.Version))

	selected := runTypedFunctionProgram(t, `
use sys select version
let v: string = version
test_report(v)`)
	assertBuiltinValue(t, selected, value.NewString(version.Version))
}

func TestSysVersionSelectedBindingIsTypedString(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
use sys select version
let n: int = version`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}
