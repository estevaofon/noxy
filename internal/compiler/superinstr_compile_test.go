package compiler

import (
	"testing"

	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/value"
)

// containsOpcodeTree e containsOpcode sobre a funcao E toda funcao constante
// aninhada (closures), para provar que a fusao NAO dispara dentro de um corpo
// interno que so enxerga a variavel como upvalue.
func containsOpcodeTree(fn *value.ObjFunction, opcode chunk.OpCode) bool {
	body := fn.Chunk.(*chunk.Chunk)
	if containsOpcode(body.Code, opcode) {
		return true
	}
	for _, constant := range body.Constants {
		if constant.Type != value.VAL_FUNCTION {
			continue
		}
		if inner, ok := constant.Obj.(*value.ObjFunction); ok && inner != nil && containsOpcodeTree(inner, opcode) {
			return true
		}
	}
	return false
}

// Superinstrucoes (issue #66, item 3): `local ± K` com local PLANO int e K em
// i8 vira OP_GET_LOCAL_ADD_IMM_INT; dois operandos locais primitivos viram
// OP_GET_LOCAL_2. Tudo em nivel de AST — sem peephole.
func TestLocalAddImmFuses(t *testing.T) {
	fn := compiledFunction(t, "func f(n: int) -> int\n    return n - 1 + (n + 2)\nend\n", "f")
	code := fn.Chunk.(*chunk.Chunk).Code
	if !containsOpcode(code, chunk.OP_GET_LOCAL_ADD_IMM_INT) {
		t.Fatalf("n - 1 / n + 2 nao fundiram em OP_GET_LOCAL_ADD_IMM_INT")
	}
	if containsOpcode(code, chunk.OP_SUB_INT) {
		t.Fatalf("OP_SUB_INT presente: n - 1 caiu no caminho generico")
	}
}

func TestLocalAddImmDoesNotFuse(t *testing.T) {
	cases := map[string]string{
		"global":       "let x: int = 0\nfunc f() -> int\n    return x + 1\nend\n",
		"float":        "func f(x: float) -> float\n    return x + 1.0\nend\n",
		"big imm":      "func f(n: int) -> int\n    return n + 1000\nend\n",
		"literal left": "func f(n: int) -> int\n    return 1 + n\nend\n",
		"ref":          "func f(r: ref int) -> int\n    return *r + 1\nend\n",
		"upvalue":      "func f(n: int) -> func() -> int\n    return func() -> int\n        return n + 1\n    end\nend\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			fn := compiledFunction(t, src, "f")
			if containsOpcodeTree(fn, chunk.OP_GET_LOCAL_ADD_IMM_INT) {
				t.Fatalf("%s fundiu indevidamente em OP_GET_LOCAL_ADD_IMM_INT", name)
			}
		})
	}
}

func TestLocalPairFuses(t *testing.T) {
	fn := compiledFunction(t, "func f(a: int, b: int) -> int\n    let i: int = 0\n    while i < b do\n        i = i + 1\n    end\n    return a + b\nend\n", "f")
	code := fn.Chunk.(*chunk.Chunk).Code
	if !containsOpcode(code, chunk.OP_GET_LOCAL_2) {
		t.Fatalf("a + b / i < b nao fundiram em OP_GET_LOCAL_2")
	}
}

func TestLocalPairDoesNotFuseForRefOrGlobal(t *testing.T) {
	cases := map[string]string{
		"global":  "let g: int = 1\nfunc f(a: int) -> int\n    return a + g\nend\n",
		"ref":     "func f(a: int, r: ref int) -> bool\n    return r == null\nend\n",
		"upvalue": "func f(a: int, b: int) -> func() -> int\n    return func() -> int\n        return a + b\n    end\nend\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			fn := compiledFunction(t, src, "f")
			if containsOpcodeTree(fn, chunk.OP_GET_LOCAL_2) {
				t.Fatalf("%s fundiu indevidamente em OP_GET_LOCAL_2", name)
			}
		})
	}
}
