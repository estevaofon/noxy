package vm

import (
	"math"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

func TestFloatArithmeticSpecialized(t *testing.T) {
	result := captureVMSource(t, `
func mandel_step(cr: float, ci: float) -> float
    let zr: float = 0.0
    let zi: float = 0.0
    let i: int = 0
    while i < 10 do
        let tmp: float = zr * zr - zi * zi + cr
        zi = 2.0 * zr * zi + ci
        zr = tmp
        i = i + 1
    end
    return zr * zr + zi * zi
end
test_report(mandel_step(-0.5, 0.25))
`)
	if result.Type != value.VAL_FLOAT {
		t.Fatalf("esperado float, obtido %s", result.String())
	}
	if math.IsNaN(result.Float()) || math.IsInf(result.Float(), 0) {
		t.Fatalf("resultado invalido: %v", result.Float())
	}
	// Valor exato: 10 iteracoes de z = z^2+c a partir de z=0, c=-0.5+0.25i,
	// reproduzidas em float64 puro (Python e Go concordam bit a bit — ver
	// registro em .superpowers/sdd/2026-08-18-vm-perf-fase1-dispatch-e-chamadas).
	// Sem isto, um typo trocando o operador de um opcode _FLOAT especializado
	// (ex.: OP_MUL_FLOAT calculando a-b) passaria pela suite sem detectar,
	// desde que o resultado continuasse sendo um float finito.
	const want = 0x1.76b0f0f446e8ep-03
	if result.Float() != want {
		t.Fatalf("resultado incorreto: got %v (%x), want %v (%x)", result.Float(), result.Float(), want, want)
	}
}

func TestFloatDivisionByZeroStillErrors(t *testing.T) {
	machine := New()
	err := interpretVMSource(t, machine, `
func f(a: float, b: float) -> float
    return a / b
end
f(1.0, 0.0)
`)
	if err == nil {
		t.Fatal("divisao float por zero deveria continuar sendo erro de runtime")
	}
	// A mensagem (nao so a presenca de erro) precisa bater com o caminho
	// generico (ramo float de OP_DIVIDE): sem isto, um typo introduzido em
	// so um dos dois handlers passaria despercebido pela suite.
	if !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("mensagem de erro divergiu do caminho generico: esperava conter %q, obtido %q", "division by zero", err.Error())
	}
}
