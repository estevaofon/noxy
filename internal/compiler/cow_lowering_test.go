package compiler

import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
)

func compileSource(t *testing.T, source string) *chunk.Chunk {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := New().Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}
	return code
}

// collectOpcodes percorre o bytecode do chunk raiz e de todas as funções
// (constantes ObjFunction), respeitando o tamanho dos operandos.
func collectOpcodes(t *testing.T, code *chunk.Chunk) map[chunk.OpCode]int {
	t.Helper()
	seen := map[chunk.OpCode]int{}
	var walk func(c *chunk.Chunk)
	walk = func(c *chunk.Chunk) {
		for offset := 0; offset < len(c.Code); {
			op := chunk.OpCode(c.Code[offset])
			seen[op]++
			offset++
			switch op {
			case chunk.OP_CONSTANT, chunk.OP_GET_LOCAL, chunk.OP_SET_LOCAL,
				chunk.OP_GET_UPVALUE, chunk.OP_SET_UPVALUE, chunk.OP_CALL,
				// perf fase 1: mesmo layout de operando do OP_CALL (1 byte argCount)
				chunk.OP_CALL_STATIC,
				chunk.OP_DEFER, chunk.OP_REF_LOCAL, chunk.OP_REF_UPVALUE,
				chunk.OP_GET_LOCAL_MUT, chunk.OP_GET_UPVALUE_MUT,
				chunk.OP_SET_PROPERTY_DEREF,
				// RC (Task 7): gemeos de emprestimo e a marca de upvalue —
				// operando de 1 byte. Sem eles aqui o walker le o operando como
				// opcode e dessincroniza, perdendo a contagem dos opcodes
				// seguintes.
				chunk.OP_SET_LOCAL_BORROW, chunk.OP_GET_LOCAL_MUT_BORROW,
				chunk.OP_MARK_UPVALUE_BORROW, chunk.OP_REF_LOCAL_BORROW:
				offset++
			case chunk.OP_CONSTANT_LONG:
				offset += 3
			case chunk.OP_GET_GLOBAL, chunk.OP_SET_GLOBAL, chunk.OP_GET_PROPERTY,
				chunk.OP_SET_PROPERTY, chunk.OP_JUMP, chunk.OP_JUMP_IF_FALSE,
				chunk.OP_JUMP_IF_TRUE, chunk.OP_LOOP, chunk.OP_ARRAY, chunk.OP_MAP,
				chunk.OP_REF_GLOBAL, chunk.OP_REF_PROPERTY, chunk.OP_CONTEXT_REF_PROPERTY,
				chunk.OP_IMPORT, chunk.OP_IMPORT_FROM_ALL, chunk.OP_SELECT,
				chunk.OP_GET_GLOBAL_MUT, chunk.OP_GET_PROP_MUT,
				chunk.OP_MARK_REF_TARGET_TYPE, chunk.OP_MARK_RUNTIME_VALUE_TYPE,
				chunk.OP_SET_GLOBAL_BORROW:
				offset += 2
			case chunk.OP_CLOSURE:
				// [const_index] [upvalue_count] ([is_local, index])*
				if offset+1 < len(c.Code) {
					upvalues := int(c.Code[offset+1])
					offset += 2 + upvalues*2
				} else {
					offset = len(c.Code)
				}
			}
		}
		for _, constant := range c.Constants {
			if fn, ok := constant.Obj.(*value.ObjFunction); ok && fn != nil {
				if fnChunk, ok := fn.Chunk.(*chunk.Chunk); ok && fnChunk != nil {
					walk(fnChunk)
				}
			}
		}
	}
	walk(code)
	return seen
}

func TestLoweringLocalIndexAssignment(t *testing.T) {
	// Indexacao tipada (issue #66): `a[0] = 9` com a local int[] e operandos
	// puros sai pela forma fundida OP_SET_LOCAL_INDEX_ARRAY_NORC, que carrega
	// a unicizacao do slot dentro do opcode (unicizeOwnedSlot, a mesma de
	// OP_GET_LOCAL_MUT) — a cadeia MUT explicita nao aparece.
	code := compileSource(t, `func f()
    let a: int[] = [1, 2]
    a[0] = 9
end`)
	ops := collectOpcodes(t, code)
	if ops[chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC] == 0 {
		t.Fatal("a[0] = 9 com a local int[] deve emitir OP_SET_LOCAL_INDEX_ARRAY_NORC")
	}
	if ops[chunk.OP_GET_LOCAL_MUT] != 0 {
		t.Fatal("a forma fundida substitui a cadeia OP_GET_LOCAL_MUT")
	}
	// Com operando impuro (chamada), a forma fundida nao sai e a cadeia MUT
	// continua unicizando o local antes da escrita.
	code = compileSource(t, `func g() -> int
    return 9
end
func f()
    let a: int[] = [1, 2]
    a[0] = g()
end`)
	ops = collectOpcodes(t, code)
	if ops[chunk.OP_GET_LOCAL_MUT] == 0 {
		t.Fatal("a[0] = g() com a local deve emitir OP_GET_LOCAL_MUT")
	}
}

func TestLoweringGlobalIndexAssignment(t *testing.T) {
	code := compileSource(t, `let a: int[] = [1, 2]
a[0] = 9`)
	ops := collectOpcodes(t, code)
	if ops[chunk.OP_GET_GLOBAL_MUT] == 0 {
		t.Fatal("a[0] = 9 com a global deve emitir OP_GET_GLOBAL_MUT")
	}
}

func TestLoweringNestedIndexAssignment(t *testing.T) {
	code := compileSource(t, `let a: int[][] = [[1]]
a[0][0] = 9`)
	ops := collectOpcodes(t, code)
	if ops[chunk.OP_GET_INDEX_MUT] == 0 {
		t.Fatal("a[0][0] = 9 deve emitir OP_GET_INDEX_MUT no nível intermediário")
	}
}

func TestLoweringStructPathAssignment(t *testing.T) {
	code := compileSource(t, `struct P
    x: int
end
let a: P[] = [P(1)]
a[0].x = 9`)
	ops := collectOpcodes(t, code)
	if ops[chunk.OP_GET_GLOBAL_MUT] == 0 || ops[chunk.OP_GET_INDEX_MUT] == 0 {
		t.Fatal("a[0].x = 9 deve unicizar a base (GET_GLOBAL_MUT) e o elemento (GET_INDEX_MUT)")
	}
}

func TestLoweringRefParamIndexAssignment(t *testing.T) {
	// Indexacao tipada (issue #66): `a[0] = 9` com a parametro `ref int[]` e
	// operandos puros sai pela forma fundida OP_SET_REF_LOCAL_INDEX_ARRAY_NORC,
	// que carrega a semantica de GET_LOCAL_MUT_BORROW + DEREF_MUT dentro do
	// opcode (unicizeThroughRefValue no fallback) — a cadeia explicita nao
	// aparece.
	code := compileSource(t, `func f(a: ref int[])
    a[0] = 9
end`)
	ops := collectOpcodes(t, code)
	if ops[chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC] == 0 {
		t.Fatal("a[0] = 9 com a ref int[] deve emitir OP_SET_REF_LOCAL_INDEX_ARRAY_NORC")
	}
	if ops[chunk.OP_DEREF_MUT] != 0 {
		t.Fatal("a forma fundida de ref substitui a cadeia OP_DEREF_MUT")
	}
	// Com operando impuro (chamada), a cadeia MUT continua unicizando atraves
	// do ref antes da escrita.
	code = compileSource(t, `func g() -> int
    return 9
end
func f(a: ref int[])
    a[0] = g()
end`)
	ops = collectOpcodes(t, code)
	if ops[chunk.OP_DEREF_MUT] == 0 {
		t.Fatal("a[0] = g() com a ref deve emitir OP_DEREF_MUT")
	}
}

// NOTA (Task 8): havia aqui três testes que auditavam quando o compilador
// emitia (ou deixava de emitir) o antigo opcode de marcação sticky —
// aliasing de let devia emitir, literal fresco e atribuição escalar não
// deviam. O compilador não emite mais esse opcode em nenhum caso (a
// unicidade é decidida em runtime pelo contador Owners), então os três
// testes ficaram sem objeto: testavam a emissão de uma máquina removida,
// não um comportamento observável. Removidos junto com a Task 8.
