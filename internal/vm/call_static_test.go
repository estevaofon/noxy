package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Chamada tipada in-module: caminho que a Task 3 acelera. O resultado não
// pode mudar.
func TestTypedCallStillWorks(t *testing.T) {
	result := captureVMSource(t, `
func double(n: int) -> int
    return n * 2
end
test_report(double(21))
`)
	if result.Type != value.VAL_INT || result.AsInt != 42 {
		t.Fatalf("esperado 42, obtido %s", result.String())
	}
}

// Caminho dinâmico (any): validateParameterModes TEM de continuar disparando —
// passar valor plano onde a função espera ref é erro de runtime aqui.
func TestDynamicCallStillValidatesModes(t *testing.T) {
	machine := New()
	err := interpretVMSource(t, machine, `
func mutate(target: ref int) -> void
    return
end
let f: any = mutate
f(5)
`)
	if err == nil {
		t.Fatal("chamada dinâmica com modo errado deveria falhar em runtime")
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Fatalf("mensagem inesperada: %v", err)
	}
}

// Slot tipado re-atribuído para OUTRA função de mesmo tipo: o call site
// OP_CALL_STATIC continua correto porque a prova é por TIPO, não por valor
// (invariantes 1-3 do preâmbulo da task). Sintaxe do tipo função conforme
// docs/NOXY_LANGUAGE_SPEC.md §4.2 — ajustar a grafia se o parser divergir.
func TestStaticCallSurvivesSameTypedRebind(t *testing.T) {
	result := captureVMSource(t, `
func inc(n: int) -> int
    return n + 1
end
func dec(n: int) -> int
    return n - 1
end
let op: func(int) -> int = inc
let a: int = op(10)
op = dec
let b: int = op(10)
test_report(a * 100 + b)
`)
	if result.Type != value.VAL_INT || result.AsInt != 1109 {
		t.Fatalf("esperado 1109 (11*100+9), obtido %s", result.String())
	}
}
