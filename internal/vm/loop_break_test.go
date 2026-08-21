package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// traceVMSource roda o programa coletando os inteiros passados a test_trace(),
// na ordem em que o VM os observa. Serve para auditar QUAIS iteracoes de um
// laco rodaram e se o codigo depois do laco chegou a executar.
func traceVMSource(t *testing.T, source string) []int64 {
	t.Helper()
	machine := New()
	trace := []int64{}
	machine.DefineNative("test_trace", func(args []value.Value) value.Value {
		if len(args) != 0 {
			trace = append(trace, args[0].AsInt)
		}
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, source); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	return trace
}

func requireTrace(t *testing.T, got []int64, want ...int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("trace esperado %v, veio %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("trace esperado %v, veio %v", want, got)
		}
	}
}

// break dentro de for...in deve sair do laco e continuar no statement
// seguinte — nao encerrar o programa silenciosamente.
func TestBreakInsideForEachExitsLoopAndResumesAfterIt(t *testing.T) {
	trace := traceVMSource(t, `
for item in [1, 2, 3] do
    test_trace(item)
    if item == 2 then break end
end
test_trace(99)`)

	requireTrace(t, trace, 1, 2, 99)
}

// break sobre map (o for compila um slot oculto $map extra) segue a mesma
// regra: sai do laco e o programa continua.
func TestBreakInsideForEachOverMapResumesAfterLoop(t *testing.T) {
	trace := traceVMSource(t, `
let m: map[string, int] = {"a": 1}
for key in m do
    test_trace(m[key])
    break
end
test_trace(99)`)

	requireTrace(t, trace, 1, 99)
}

// break no for interno encerra apenas o laco interno; o externo prossegue.
func TestBreakInsideNestedForEachExitsOnlyInnerLoop(t *testing.T) {
	trace := traceVMSource(t, `
for outer in [1, 2] do
    for inner in [10, 20] do
        test_trace(inner)
        break
    end
    test_trace(outer)
end
test_trace(99)`)

	requireTrace(t, trace, 10, 1, 10, 2, 99)
}

// break dentro de um for aninhado em while nao pode escapar o while: o laco
// externo deve continuar iterando.
func TestBreakInsideForNestedInWhileExitsOnlyTheFor(t *testing.T) {
	trace := traceVMSource(t, `
let i: int = 0
while i < 2 do
    for item in [10, 20] do
        test_trace(item)
        break
    end
    test_trace(i)
    i = i + 1
end
test_trace(99)`)

	requireTrace(t, trace, 10, 0, 10, 1, 99)
}

// Depois do for, o break do while que o envolve tem de voltar a mirar o
// while — nao o for ja encerrado.
func TestBreakAfterForEachTargetsEnclosingWhile(t *testing.T) {
	trace := traceVMSource(t, `
let i: int = 0
while i < 3 do
    for item in [10] do
        test_trace(item)
    end
    if i == 1 then break end
    test_trace(i)
    i = i + 1
end
test_trace(99)`)

	requireTrace(t, trace, 10, 0, 10, 99)
}

// A forma inline `if cond then break end` (a que a doc de concorrencia usa)
// tem de fechar o if no seu proprio 'end' — o while que a envolve continua
// sendo fechado pelo 'end' dele.
func TestBreakInlineInsideWhileClosesOnlyTheIf(t *testing.T) {
	trace := traceVMSource(t, `
let i: int = 0
while i < 5 do
    test_trace(i)
    if i == 1 then break end
    i = i + 1
end
test_trace(99)`)

	requireTrace(t, trace, 0, 1, 99)
}

// O for deixa a pilha de valores equilibrada apos um break: expressoes
// avaliadas depois do laco veem os mesmos slots locais de antes.
func TestBreakInsideForEachLeavesLocalsIntact(t *testing.T) {
	trace := traceVMSource(t, `
func run() -> int
    let total: int = 0
    for item in [1, 2, 3] do
        total = total + item
        if item == 2 then break end
    end
    return total
end

test_trace(run())
test_trace(99)`)

	requireTrace(t, trace, 3, 99)
}

// continue dentro de while volta para a condicao pulando o resto do corpo.
func TestContinueInWhileSkipsRestOfBody(t *testing.T) {
	trace := traceVMSource(t, `
let i: int = 0
while i < 6 do
    i = i + 1
    if i % 2 == 0 then continue end
    test_trace(i)
end
test_trace(99)`)

	requireTrace(t, trace, 1, 3, 5, 99)
}

// continue dentro de for...in salta para o passo de incremento: a iteracao
// seguinte roda normalmente.
func TestContinueInForEachSkipsToNextElement(t *testing.T) {
	trace := traceVMSource(t, `
for item in [1, 2, 3, 4] do
    let dobro: int = item * 2
    if dobro == 4 then continue end
    test_trace(dobro)
end
test_trace(99)`)

	requireTrace(t, trace, 2, 6, 8, 99)
}

// continue mira sempre o laco mais interno.
func TestContinueInNestedLoopsTargetsInnermost(t *testing.T) {
	trace := traceVMSource(t, `
for a in [1, 2] do
    let j: int = 0
    while j < 3 do
        j = j + 1
        if j == 2 then continue end
        test_trace(a * 10 + j)
    end
end`)

	requireTrace(t, trace, 11, 13, 21, 23)
}

// let do corpo capturado por closure + continue DEPOIS da closure: o continue
// tem de fechar a caixa (como endScope) — senao o slot e reusado pela
// iteracao seguinte e a closure passa a ler o valor dela.
func TestContinueClosesUpvalueOfCapturedBodyLocal(t *testing.T) {
	trace := traceVMSource(t, `
let saved: func() -> int = func() -> int return -1 end
let i: int = 0
while i < 2 do
    i = i + 1
    let x: int = i
    if i == 1 then
        saved = func() -> int return x end
        continue
    end
end
test_trace(saved())`)

	requireTrace(t, trace, 1)
}

// Mesmo bug latente no break: closure capturada no corpo e break depois dela.
func TestBreakClosesUpvalueOfCapturedBodyLocal(t *testing.T) {
	trace := traceVMSource(t, `
let saved: func() -> int = func() -> int return -1 end
for item in [7, 8, 9] do
    let x: int = item
    saved = func() -> int return x end
    break
end
let other: int[] = [1, 2, 3]
test_trace(saved())`)

	requireTrace(t, trace, 7)
}

// Closure textualmente DEPOIS do continue: a iteracao que continua nunca cria
// a caixa; as outras guardam o valor certo.
func TestContinueBeforeClosureCreationKeepsOtherIterationsIntact(t *testing.T) {
	trace := traceVMSource(t, `
let saved1: func() -> int = func() -> int return -1 end
let saved3: func() -> int = func() -> int return -1 end
let i: int = 0
while i < 3 do
    i = i + 1
    let x: int = i
    if i == 2 then continue end
    if i == 1 then saved1 = func() -> int return x end end
    if i == 3 then saved3 = func() -> int return x end end
end
test_trace(saved1())
test_trace(saved3())`)

	requireTrace(t, trace, 1, 3)
}
