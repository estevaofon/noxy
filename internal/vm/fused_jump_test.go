package vm

import (
	"fmt"
	"testing"

	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/value"
)

// Frame raiz: stack[0] = script closure; empilhamos a,b e o opcode fundido
// salta (ou não) sobre um OP_CONSTANT sentinela. Depois do sentinela vem um
// OP_CONSTANT marcador que os DOIS caminhos executam — isso prova não só que
// saltou, mas que o pouso caiu exatamente no início da instrução seguinte ao
// sentinela: um pouso errado por 1 byte para qualquer lado decodificaria um
// opcode/operando no meio de uma instrução, o que não produz nem a pilha
// "saltou" (closure+marcador) nem a "não saltou" (closure+sentinela+marcador)
// — cairia no `default` abaixo (ou o próprio VM devolveria erro). Sem
// OP_RETURN de propósito (o loop cai fora no fim do código e deixa a pilha
// para inspeção).
func runFusedJump(t *testing.T, op chunk.OpCode, a, b int64) (jumped bool) {
	t.Helper()
	machine := New()
	code := &chunk.Chunk{}
	ca := code.AddConstant(value.NewInt(a))
	cb := code.AddConstant(value.NewInt(b))
	sentinel := code.AddConstant(value.NewInt(777))
	marker := code.AddConstant(value.NewInt(888))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(ca), 1)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(cb), 1)
	code.Write(byte(op), 1)
	code.Write(0, 1) // offset hi
	code.Write(2, 1) // offset lo: pula exatamente a instrucao OP_CONSTANT sentinela (2 bytes)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(sentinel), 1)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(marker), 1)
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	switch machine.stackTop {
	case 2: // closure + marcador: saltou e pousou exatamente certo
		top := machine.stack[1]
		if top.Type != value.VAL_INT || top.Int() != 888 {
			t.Fatalf("pouso do salto incorreto: top=%s", top.String())
		}
		return true
	case 3: // closure + sentinela + marcador: nao saltou
		sentinelTop := machine.stack[1]
		markerTop := machine.stack[2]
		if sentinelTop.Type != value.VAL_INT || sentinelTop.Int() != 777 ||
			markerTop.Type != value.VAL_INT || markerTop.Int() != 888 {
			t.Fatalf("pilha inesperada (nao saltou): sentinela=%s marcador=%s", sentinelTop.String(), markerTop.String())
		}
		return false
	default:
		t.Fatalf("pilha com formato inesperado apos execucao (pouso de salto provavelmente errado): stackTop=%d", machine.stackTop)
		return false
	}
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
	if result.Type != value.VAL_INT || result.Int() != 7 {
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
	if trueCase.Type != value.VAL_INT || trueCase.Int() != 10 {
		t.Fatalf("esperado 10 (a<b), obtido %s", trueCase.String())
	}
	falseCase := captureVMSource(t, fmt.Sprintf(source, 2, 1))
	if falseCase.Type != value.VAL_INT || falseCase.Int() != 0 {
		t.Fatalf("esperado 0 (a>=b), obtido %s", falseCase.String())
	}
}
