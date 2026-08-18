package vm

import (
	"fmt"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Frame raiz: stack[0] = script closure; empilhamos a,b e o opcode fundido
// salta (ou não) sobre um OP_CONSTANT sentinela. Sem OP_RETURN de propósito
// (o loop cai fora no fim do código e deixa a pilha para inspeção).
func runFusedJump(t *testing.T, op chunk.OpCode, a, b int64) (jumped bool) {
	t.Helper()
	machine := New()
	code := &chunk.Chunk{}
	ca := code.AddConstant(value.NewInt(a))
	cb := code.AddConstant(value.NewInt(b))
	sentinel := code.AddConstant(value.NewInt(777))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(ca), 1)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(cb), 1)
	code.Write(byte(op), 1)
	code.Write(0, 1) // offset hi
	code.Write(2, 1) // offset lo: pula o OP_CONSTANT sentinela (2 bytes)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(sentinel), 1)
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	// Se saltou, a pilha ficou só com a closure (stackTop==1); senão, o
	// sentinela 777 está no topo.
	if machine.stackTop == 1 {
		return true
	}
	top := machine.stack[machine.stackTop-1]
	if top.Type != value.VAL_INT || top.AsInt != 777 {
		t.Fatalf("pilha inesperada: top=%s stackTop=%d", top.String(), machine.stackTop)
	}
	return false
}

func TestFusedJumpOpcodes(t *testing.T) {
	cases := []struct {
		name string
		op   chunk.OpCode
		a, b int64
		want bool
	}{
		{"LT salta quando a<b", chunk.OP_JUMP_IF_LT_INT, 1, 2, true},
		{"LT nao salta quando a>=b", chunk.OP_JUMP_IF_LT_INT, 2, 2, false},
		{"LE salta quando a<=b", chunk.OP_JUMP_IF_LE_INT, 2, 2, true},
		{"LE nao salta quando a>b", chunk.OP_JUMP_IF_LE_INT, 3, 2, false},
		{"GT salta quando a>b", chunk.OP_JUMP_IF_GT_INT, 3, 2, true},
		{"GT nao salta quando a<=b", chunk.OP_JUMP_IF_GT_INT, 2, 2, false},
		{"GE salta quando a>=b", chunk.OP_JUMP_IF_GE_INT, 2, 2, true},
		{"GE nao salta quando a<b", chunk.OP_JUMP_IF_GE_INT, 1, 2, false},
		{"EQ salta quando a==b", chunk.OP_JUMP_IF_EQ_INT, 5, 5, true},
		{"EQ nao salta quando a!=b", chunk.OP_JUMP_IF_EQ_INT, 5, 6, false},
		{"NE salta quando a!=b", chunk.OP_JUMP_IF_NE_INT, 5, 6, true},
		{"NE nao salta quando a==b", chunk.OP_JUMP_IF_NE_INT, 5, 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runFusedJump(t, tc.op, tc.a, tc.b); got != tc.want {
				t.Fatalf("jumped=%v, esperado %v", got, tc.want)
			}
		})
	}
}

func TestFusedWhileAndIfBehavior(t *testing.T) {
	result := captureVMSource(t, `
func fib(n: int) -> int
    if n <= 1 then
        return n
    end
    return fib(n - 1) + fib(n - 2)
end
let i: int = 0
let acc: int = 0
while i < 5 do
    acc = acc + fib(i)
    i = i + 1
end
test_report(acc)
`)
	if result.Type != value.VAL_INT || result.AsInt != 7 {
		t.Fatalf("esperado 7 (fib 0..4 = 0+1+1+2+3), obtido %s", result.String())
	}
}

// TestFusedIifeInConditionRollsBackAndRuns cobre a questão aberta do plano:
// um literal de função dentro de um operando de comparação (aqui, chamado
// imediatamente). tryCompileFusedCondition compila o FunctionLiteral
// especulativamente — capturando a/b como upvalue do compilador da função
// envolvente — descobre que o tipo é bool (não int) e desfaz com TruncateTo;
// o caminho genérico recompila a mesma árvore do zero. Validamos aqui que o
// resultado em RUNTIME está correto nos dois ramos da condição, não só que
// compila sem erro (isso já é coberto em
// TestLambdaOperandRollsBack no pacote compiler).
func TestFusedIifeInConditionRollsBackAndRuns(t *testing.T) {
	source := `
func f(a: int, b: int) -> int
    let base: int = 10
    if (func() -> bool return a < b end)() == true then
        return base
    end
    return 0
end
test_report(f(%d, %d))
`
	trueCase := captureVMSource(t, fmt.Sprintf(source, 1, 2))
	if trueCase.Type != value.VAL_INT || trueCase.AsInt != 10 {
		t.Fatalf("esperado 10 (a<b), obtido %s", trueCase.String())
	}
	falseCase := captureVMSource(t, fmt.Sprintf(source, 2, 1))
	if falseCase.Type != value.VAL_INT || falseCase.AsInt != 0 {
		t.Fatalf("esperado 0 (a>=b), obtido %s", falseCase.String())
	}
}
