package vm

// zeros(n) com tamanho negativo era panic de Go (makeslice: len out of
// range) atravessando a VM inteira ate o recover do main — deve ser erro de
// runtime do noxy, com linha do script e capturavel.

import (
	"strings"
	"testing"
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
