package compiler

import (
	"testing"

	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
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

func TestFusedWhileCompiles(t *testing.T) {
	if err := compileFusedSource(t, `
func f() -> int
    let i: int = 0
    while i < 10 do
        i = i + 1
    end
    return i
end
f()
`); err != nil {
		t.Fatalf("compile error: %v", err)
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
