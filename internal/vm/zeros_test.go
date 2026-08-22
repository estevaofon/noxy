package vm

// zeros(n) com tamanho negativo era panic de Go (makeslice: len out of
// range) atravessando a VM inteira ate o recover do main — deve ser erro de
// runtime do noxy, com linha do script e capturavel.

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

func TestZerosNegativeSizeIsRuntimeError(t *testing.T) {
	machine := New()
	err := interpretVMSource(t, machine, "let n: int = 0 - 5\nlet xs: int[] = zeros(n)")
	if err == nil || !strings.Contains(err.Error(), "zeros size must be non-negative") {
		t.Fatalf("tamanho negativo deveria ser erro de runtime do noxy, veio %v", err)
	}
}

func TestZerosZeroSizeIsEmptyArray(t *testing.T) {
	got := captureVMSource(t, "let xs: int[] = zeros(0)\ntest_report(length(xs))")
	expectInt(t, got, 0, "zeros(0)")
}

func TestFixedArrayDeclarationDoesNotUseOperandStack(t *testing.T) {
	reported := captureVMSource(t, "let buf: int[10000]\ntest_report(length(buf))\n")
	if reported.Type != value.VAL_INT || reported.Int() != 10000 {
		t.Fatalf("length = %v, want 10000", reported)
	}
	reported = captureVMSource(t, "let big: int[100000]\ntest_report(big[99999])\n")
	if reported.Type != value.VAL_INT || reported.Int() != 0 {
		t.Fatalf("big[99999] = %v, want 0", reported)
	}
}

func TestFixedArrayDefaultsPerElementType(t *testing.T) {
	reported := captureVMSource(t, `
struct P
    x: int
end
let fs: float[2]
let ss: string[2]
let bs: bool[2]
let ps: P[2]
test_report(to_str(fs[1]) + "|" + ss[1] + "|" + to_str(bs[1]) + "|" + to_str(ps[1] == null))`)
	if got := reported.Obj.(string); got != "0.000000||false|true" {
		t.Fatalf("defaults = %q", got)
	}
}

// Default composto: os N slots comecam compartilhando o mesmo objeto
// (Owners = N); a CoW clona na primeira escrita, entao escrever em g[0]
// NAO pode aparecer em g[1].
func TestFixedNestedArrayElementsAreIndependentUnderCoW(t *testing.T) {
	reported := captureVMSource(t, "let g: int[3][3]\ng[0][0] = 1\ntest_report(g[1][0] + g[0][0] * 10)\n")
	if reported.Type != value.VAL_INT || reported.Int() != 10 {
		t.Fatalf("g[1][0] + 10*g[0][0] = %v, want 10", reported)
	}
}
