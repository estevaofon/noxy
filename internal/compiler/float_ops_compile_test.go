package compiler

import (
	"fmt"
	"testing"

	"github.com/estevaofon/noxy/internal/chunk"
)

// TestFloatArithmeticOpcodesEmitted fixa, por bytecode, que operandos
// estaticamente float emitem os opcodes _FLOAT especializados (Task 7) em vez
// do caminho generico. Sem isto, a especializacao poderia "nao disparar
// silenciosamente" (isFloat sempre false) e os testes de comportamento em
// internal/vm/float_ops_test.go passariam do mesmo jeito, ja que o caminho
// generico produz o mesmo resultado numerico.
func TestFloatArithmeticOpcodesEmitted(t *testing.T) {
	cases := []struct {
		operator    string
		returnType  string
		want        chunk.OpCode
		genericWant chunk.OpCode
	}{
		{"+", "float", chunk.OP_ADD_FLOAT, chunk.OP_ADD},
		{"-", "float", chunk.OP_SUB_FLOAT, chunk.OP_SUBTRACT},
		{"*", "float", chunk.OP_MUL_FLOAT, chunk.OP_MULTIPLY},
		{"/", "float", chunk.OP_DIV_FLOAT, chunk.OP_DIVIDE},
		{">", "bool", chunk.OP_GREATER_FLOAT, chunk.OP_GREATER},
		{"<", "bool", chunk.OP_LESS_FLOAT, chunk.OP_LESS},
	}
	for _, tc := range cases {
		t.Run(tc.operator, func(t *testing.T) {
			source := fmt.Sprintf(`
func f(a: float, b: float) -> %s
    return a %s b
end
`, tc.returnType, tc.operator)
			fn := compiledFunction(t, source, "f")
			code := fn.Chunk.(*chunk.Chunk).Code

			if !containsOpcode(code, tc.want) {
				t.Fatalf("operador %q com operandos float: esperava %s no bytecode, ausente (especializacao nao disparou)", tc.operator, tc.want)
			}
			if containsOpcode(code, tc.genericWant) {
				t.Fatalf("operador %q com operandos float: opcode generico %s presente apesar de ambos operandos serem float estaticamente", tc.operator, tc.genericWant)
			}
		})
	}
}

// TestMixedIntFloatArithmeticStaysGeneric prova que um operando int e outro
// float NAO disparam nem OP_*_INT nem OP_*_FLOAT: soh o caminho generico faz
// a promocao numerica int->float. Cobre todos os seis operadores que
// ganharam o ramo isFloat (+ - * / > <), nao so os quatro aritmeticos: o
// gate isFloat e um unico bool computado antes do switch, mas cada braco do
// switch e um site de emissao separado, entao cada um merece sua propria
// prova.
func TestMixedIntFloatArithmeticStaysGeneric(t *testing.T) {
	cases := []struct {
		operator   string
		returnType string
		generic    chunk.OpCode
		intOp      chunk.OpCode
		floatOp    chunk.OpCode
	}{
		{"+", "float", chunk.OP_ADD, chunk.OP_ADD_INT, chunk.OP_ADD_FLOAT},
		{"-", "float", chunk.OP_SUBTRACT, chunk.OP_SUB_INT, chunk.OP_SUB_FLOAT},
		{"*", "float", chunk.OP_MULTIPLY, chunk.OP_MUL_INT, chunk.OP_MUL_FLOAT},
		{"/", "float", chunk.OP_DIVIDE, chunk.OP_DIV_INT, chunk.OP_DIV_FLOAT},
		{">", "bool", chunk.OP_GREATER, chunk.OP_GREATER_INT, chunk.OP_GREATER_FLOAT},
		{"<", "bool", chunk.OP_LESS, chunk.OP_LESS_INT, chunk.OP_LESS_FLOAT},
	}
	for _, tc := range cases {
		t.Run(tc.operator, func(t *testing.T) {
			source := fmt.Sprintf(`
func f(a: int, b: float) -> %s
    return a %s b
end
`, tc.returnType, tc.operator)
			fn := compiledFunction(t, source, "f")
			code := fn.Chunk.(*chunk.Chunk).Code

			if !containsOpcode(code, tc.generic) {
				t.Fatalf("operador %q misto int/float: esperava opcode generico %s, ausente", tc.operator, tc.generic)
			}
			if containsOpcode(code, tc.intOp) {
				t.Fatalf("operador %q misto int/float: opcode _INT %s presente indevidamente", tc.operator, tc.intOp)
			}
			if containsOpcode(code, tc.floatOp) {
				t.Fatalf("operador %q misto int/float: opcode _FLOAT %s presente indevidamente", tc.operator, tc.floatOp)
			}
		})
	}
}

// TestFloatEqualityStaysGeneric confirma que a Task 7 NAO foi estendida alem
// do escopo do brief: ==, !=, >=, <= com operandos float continuam no
// caminho generico (nenhum OP_LESS_FLOAT/OP_GREATER_FLOAT emitido).
func TestFloatEqualityStaysGeneric(t *testing.T) {
	for _, operator := range []string{"==", "!=", ">=", "<="} {
		t.Run(operator, func(t *testing.T) {
			source := fmt.Sprintf(`
func f(a: float, b: float) -> bool
    return a %s b
end
`, operator)
			fn := compiledFunction(t, source, "f")
			code := fn.Chunk.(*chunk.Chunk).Code

			if containsOpcode(code, chunk.OP_LESS_FLOAT) || containsOpcode(code, chunk.OP_GREATER_FLOAT) {
				t.Fatalf("operador %q com float: opcode _FLOAT emitido, mas Task 7 nao deveria estender >=/<=/==/!=", operator)
			}
		})
	}
}
