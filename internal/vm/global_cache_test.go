package vm

import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Guarda o cache de globais contra staleness: a função lê o global duas
// vezes com uma reatribuição no meio — a segunda leitura TEM de ver o valor
// novo mesmo com o site de OP_GET_GLOBAL cacheado pela primeira.
func TestGlobalReadSeesReassignmentBetweenCalls(t *testing.T) {
	result := captureVMSource(t, `
let x: int = 1
func get() -> int
    return x
end
let first: int = get()
x = 2
let second: int = get()
test_report(first * 10 + second)
`)
	if result.Type != value.VAL_INT || result.AsInt != 12 {
		t.Fatalf("esperado 12 (first=1, second=2), obtido %s", result.String())
	}
}

// Mesmo chunk executado sob DOIS ambientes diferentes (o caso de módulo /
// InterpretWithEnvironment): a entrada cacheada do primeiro ambiente não pode
// vazar para o segundo — a comparação entry.Env tem de falhar e re-resolver.
// Chunk montado à mão (padrão cow_mut_opcodes_test.go): OP_GET_GLOBAL "x" sem
// OP_RETURN — o loop cai fora e deixa o valor lido em stack[1] para inspeção.
func TestGlobalCacheKeyedByEnvironment(t *testing.T) {
	machine := New()
	code := &chunk.Chunk{}
	nameIdx := code.AddConstant(value.NewString("x"))
	code.Write(byte(chunk.OP_GET_GLOBAL), 1)
	code.Write(byte(nameIdx>>8), 1)
	code.Write(byte(nameIdx&0xff), 1)

	envA := value.NewGlobalEnvironmentFrom(map[string]value.Value{"x": value.NewInt(1)}, nil)
	envB := value.NewGlobalEnvironmentFrom(map[string]value.Value{"x": value.NewInt(2)}, nil)

	if err := machine.InterpretWithEnvironment(code, envA); err != nil {
		t.Fatalf("vm error (envA): %v", err)
	}
	if got := machine.stack[1]; got.Type != value.VAL_INT || got.AsInt != 1 {
		t.Fatalf("envA: esperado 1, obtido %s", got.String())
	}

	if err := machine.InterpretWithEnvironment(code, envB); err != nil {
		t.Fatalf("vm error (envB): %v", err)
	}
	if got := machine.stack[1]; got.Type != value.VAL_INT || got.AsInt != 2 {
		t.Fatalf("envB: cache vazou o valor do envA — esperado 2, obtido %s", got.String())
	}
}
