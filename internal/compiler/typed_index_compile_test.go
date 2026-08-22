package compiler

import (
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

var opNamePattern = regexp.MustCompile(`\bOP_[A-Z_]+\b`)

// opcodeNames devolve os nomes dos opcodes de um chunk NA ORDEM, lidos da
// saida do disassembler (que conhece a largura de cada instrucao) —
// containsOpcode varre bytes e confundiria um operando com um opcode.
func opcodeNames(t *testing.T, code *chunk.Chunk) []string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string)
	go func() {
		out, _ := io.ReadAll(reader)
		done <- string(out)
	}()
	previous := os.Stdout
	os.Stdout = writer
	code.Disassemble("t")
	_ = writer.Close()
	os.Stdout = previous
	return opNamePattern.FindAllString(<-done, -1)
}

// findFunctionChunk procura a funcao pelo nome em toda a arvore de constantes
// (compiledFunction so olha o nivel de cima; closures aninhadas — `inner`
// dentro de `outer` — moram no chunk da funcao que as declara).
func findFunctionChunk(code *chunk.Chunk, name string) *chunk.Chunk {
	for _, constant := range code.Constants {
		if constant.Type != value.VAL_FUNCTION {
			continue
		}
		fn := constant.Obj.(*value.ObjFunction)
		child := fn.Chunk.(*chunk.Chunk)
		if fn.Name == name {
			return child
		}
		if found := findFunctionChunk(child, name); found != nil {
			return found
		}
	}
	return nil
}

func functionOpcodes(t *testing.T, source, name string) []string {
	t.Helper()
	code, _, err := New().Compile(parse(source))
	if err != nil {
		t.Fatal(err)
	}
	fnChunk := findFunctionChunk(code, name)
	if fnChunk == nil {
		t.Fatalf("function %q not found", name)
	}
	return opcodeNames(t, fnChunk)
}

func topLevelOpcodes(t *testing.T, source string) []string {
	t.Helper()
	code, _, err := New().Compile(parse(source))
	if err != nil {
		t.Fatal(err)
	}
	return opcodeNames(t, code)
}

func assertHas(t *testing.T, ops []string, want string) {
	t.Helper()
	if !slices.Contains(ops, want) {
		t.Fatalf("esperava %s no bytecode, obtido: %s", want, strings.Join(ops, " "))
	}
}

func assertLacks(t *testing.T, ops []string, unwanted string) {
	t.Helper()
	if slices.Contains(ops, unwanted) {
		t.Fatalf("nao esperava %s no bytecode, obtido: %s", unwanted, strings.Join(ops, " "))
	}
}

// assertNotFollowedByPop: as formas de escrita tipadas sao statement (nao
// empilham), entao o compilador nao pode emitir o OP_POP da sequencia
// generica depois delas.
func assertNotFollowedByPop(t *testing.T, ops []string, op string) {
	t.Helper()
	for k, name := range ops {
		if name == op && k+1 < len(ops) && ops[k+1] == "OP_POP" {
			t.Fatalf("%s seguido de OP_POP: a forma tipada de escrita nao empilha", op)
		}
	}
}

// Base T[] em posicao generica (global): leitura tipada, escrita NORC sem OP_POP.
func TestGlobalArrayUsesTypedIndexOpcodes(t *testing.T) {
	ops := topLevelOpcodes(t, "let xs: int[] = [1, 2]\nlet i: int = 0\nxs[i] = xs[i] + 1\n")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertLacks(t, ops, "OP_GET_INDEX")
	assertLacks(t, ops, "OP_SET_INDEX")
	assertNotFollowedByPop(t, ops, "OP_SET_INDEX_ARRAY_NORC")
}

// Nested: o nivel de fora e MUT generico, o de dentro e tipado.
func TestNestedArrayWriteUsesTypedInnerOpcode(t *testing.T) {
	ops := functionOpcodes(t, `
func f(g: int[][]) -> int
    g[0][1] = g[1][0] + 1
    return g[0][1]
end
`, "f")
	assertHas(t, ops, "OP_GET_INDEX_MUT")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_INDEX")
}

// Elemento composto: escrita segue OP_SET_INDEX (RC do generico); leitura e tipada.
func TestCompositeElementKeepsGenericSetIndex(t *testing.T) {
	ops := functionOpcodes(t, `
struct P
    x: int
end
func f(ps: P[][]) -> P
    ps[0][0] = P(1)
    return ps[0][0]
end
`, "f")
	assertHas(t, ops, "OP_SET_INDEX")
	assertLacks(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
}

// Map e any: tudo generico.
func TestMapAndAnyKeepGenericIndexOpcodes(t *testing.T) {
	ops := functionOpcodes(t, `
func f(m: map[string, int], a: any) -> int
    m["k"] = a[0]
    return m["k"]
end
`, "f")
	assertHas(t, ops, "OP_GET_INDEX")
	assertHas(t, ops, "OP_SET_INDEX")
	assertLacks(t, ops, "OP_GET_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_INDEX_ARRAY_NORC")
}

// Upvalue de tipo T[] (closure): forma generica tipada, nunca a fundida por slot.
func TestUpvalueArrayUsesGenericTypedOpcodes(t *testing.T) {
	ops := functionOpcodes(t, `
func outer() -> int
    let xs: int[] = [1, 2]
    func inner() -> int
        xs[0] = 5
        return xs[1]
    end
    return inner()
end
`, "inner")
	assertHas(t, ops, "OP_GET_UPVALUE_MUT")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertLacks(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
}
