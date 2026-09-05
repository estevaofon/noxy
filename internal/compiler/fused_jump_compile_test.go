package compiler

import (
	"fmt"
	"testing"

	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/parser"
)

func compileFusedSource(t *testing.T, source string) error {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	_, _, err := New().Compile(program)
	return err
}

// TestFusedWhileCompiles fundia sem checar o bytecode gerado (so "compila
// sem erro"), o que passaria identico se a fusao fosse desligada ou se a
// tabela de mapeamento operador->opcode saisse invertida. Fortalecido para
// tambem exigir OP_JUMP_IF_GE_INT presente (a fusao de `<` de fato disparou)
// e OP_JUMP_IF_FALSE ausente (nao caiu no caminho generico).
func TestFusedWhileCompiles(t *testing.T) {
	source := `
func f() -> int
    let i: int = 0
    while i < 10 do
        i = i + 1
    end
    return i
end
f()
`
	if err := compileFusedSource(t, source); err != nil {
		t.Fatalf("compile error: %v", err)
	}
	fn := compiledFunction(t, source, "f")
	code := fn.Chunk.(*chunk.Chunk).Code
	if !containsOpcode(code, chunk.OP_JUMP_IF_GE_INT) {
		t.Fatalf("`while i < 10` nao fundiu: OP_JUMP_IF_GE_INT ausente do bytecode")
	}
	if containsOpcode(code, chunk.OP_JUMP_IF_FALSE) {
		t.Fatalf("`while i < 10` caiu no caminho generico (OP_JUMP_IF_FALSE presente) apesar de i e 10 serem int")
	}
}

// TestFusedOperatorMapsToExpectedOpcode fixa, por bytecode, a tabela inteira
// de fusedIntCompareJump — as seis linhas de negacao manual sao o ponto mais
// propenso a inversao desta mudanca (o proprio brief ja apontava a direcao do
// salto como o risco central). Sem este teste, so `<` e `<=` ficavam presos
// por comportamento via TestFusedWhileAndIfBehavior (pacote vm); `>`, `>=`,
// `==` e `!=` nao tinham nenhuma asercao no `go test`. Para nao passar de
// forma vazia (achar o byte certo por acaso em outro lugar do chunk, ou olhar
// para o chunk errado), cada caso: (a) usa compiledFunction, que ja falha com
// t.Fatalf se a funcao nao existir no pool de constantes; (b) exige AUSENCIA
// dos outros cinco opcodes fundidos (uma inversao troca QUAL opcode aparece,
// nao duplica); e (c) exige AUSENCIA de OP_JUMP_IF_FALSE (prova que fundiu,
// nao caiu no generico).
func TestFusedOperatorMapsToExpectedOpcode(t *testing.T) {
	allFused := []chunk.OpCode{
		chunk.OP_JUMP_IF_LT_INT,
		chunk.OP_JUMP_IF_LE_INT,
		chunk.OP_JUMP_IF_GT_INT,
		chunk.OP_JUMP_IF_GE_INT,
		chunk.OP_JUMP_IF_EQ_INT,
		chunk.OP_JUMP_IF_NE_INT,
	}
	cases := []struct {
		operator string
		want     chunk.OpCode
	}{
		{"<", chunk.OP_JUMP_IF_GE_INT},
		{"<=", chunk.OP_JUMP_IF_GT_INT},
		{">", chunk.OP_JUMP_IF_LE_INT},
		{">=", chunk.OP_JUMP_IF_LT_INT},
		{"==", chunk.OP_JUMP_IF_NE_INT},
		{"!=", chunk.OP_JUMP_IF_EQ_INT},
	}
	for _, tc := range cases {
		t.Run(tc.operator, func(t *testing.T) {
			source := fmt.Sprintf(`
func f(a: int, b: int) -> int
    if a %s b then
        return 1
    end
    return 0
end
`, tc.operator)
			fn := compiledFunction(t, source, "f")
			code := fn.Chunk.(*chunk.Chunk).Code

			if !containsOpcode(code, tc.want) {
				t.Fatalf("operador %q: esperava %s no bytecode, ausente", tc.operator, tc.want)
			}
			for _, other := range allFused {
				if other == tc.want {
					continue
				}
				if containsOpcode(code, other) {
					t.Fatalf("operador %q: opcode fundido inesperado %s tambem presente (tabela de mapeamento provavelmente invertida)", tc.operator, other)
				}
			}
			if containsOpcode(code, chunk.OP_JUMP_IF_FALSE) {
				t.Fatalf("operador %q: OP_JUMP_IF_FALSE presente — condicao nao fundiu", tc.operator)
			}
		})
	}
}

// Condição float NÃO pode fundir: o rollback (TruncateTo) tem de deixar o
// caminho genérico intacto.
func TestNonIntConditionFallsBack(t *testing.T) {
	if err := compileFusedSource(t, `
func f() -> int
    let x: float = 0.0
    let n: int = 0
    while x < 10.0 do
        x = x + 1.0
        n = n + 1
    end
    return n
end
f()
`); err != nil {
		t.Fatalf("compile error (fallback quebrado): %v", err)
	}
}

// Condição com curto-circuito dentro do operando (jumps internos) sob
// rollback: `(a < b) == flag` compila o InfixExpression externo `==` — o lado
// esquerdo é bool, então o fuse desiste após compilar o operando esquerdo.
func TestBoolOperandRollsBack(t *testing.T) {
	if err := compileFusedSource(t, `
func f(a: int, b: int, flag: bool) -> int
    if (a < b) == flag then
        return 1
    end
    return 0
end
f(1, 2, true)
`); err != nil {
		t.Fatalf("compile error (rollback com operando bool): %v", err)
	}
}

// Caso da questão aberta do plano: um literal de função (lambda) dentro de um
// operando de comparação, invocado imediatamente (IIFE). A tentativa
// especulativa compila o FunctionLiteral (que captura `a` e `b` do escopo de
// `f` como upvalue, marcando IsCaptured no compilador ENVOLVENTE) antes de
// descobrir que o tipo resultante é bool, não int — e faz TruncateTo. O
// caminho genérico recompila a mesma árvore do zero, então o registro de
// upvalue acontece de novo (idempotente: IsCaptured já true) e o closure
// realmente emitido é o da segunda passada. Isso compila e roda correto —
// ver TestFusedIifeInConditionRollsBackAndRuns no pacote vm para o valor.
func TestLambdaOperandRollsBack(t *testing.T) {
	if err := compileFusedSource(t, `
func f(a: int, b: int) -> int
    let base: int = 10
    if (func() -> bool return a < b end)() == true then
        return base
    end
    return 0
end
f(1, 2)
`); err != nil {
		t.Fatalf("compile error (rollback com lambda literal no operando): %v", err)
	}
}
