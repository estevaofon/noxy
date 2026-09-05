package compiler

import (
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/value"
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

// Local plano T[] com indice puro: forma fundida por slot, sem GET_LOCAL do
// array e sem OP_POP depois da escrita.
func TestLocalArrayFusesIndexIntoSlotForm(t *testing.T) {
	ops := functionOpcodes(t, `
func f() -> int
    let xs: int[] = [1, 2, 3]
    let i: int = 1
    xs[i + 1] = xs[i] + xs[0]
    return xs[2]
end
`, "f")
	assertHas(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
	assertLacks(t, ops, "OP_GET_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertLacks(t, ops, "OP_GET_LOCAL_MUT")
	assertNotFollowedByPop(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
}

// Indice ou valor com chamada: a forma fundida NAO sai (le o slot depois de
// avaliar os operandos; uma chamada poderia rebindar o local via closure ou
// ref). Fica a forma generica tipada, com o container avaliado primeiro.
func TestLocalArrayDoesNotFuseWhenOperandHasCall(t *testing.T) {
	ops := functionOpcodes(t, `
func idx() -> int
    return 0
end
func f() -> int
    let xs: int[] = [1, 2, 3]
    xs[idx()] = 5
    xs[0] = idx()
    return xs[idx()]
end
`, "f")
	assertLacks(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_GET_LOCAL_MUT")
}

// Parametro T[] (sem ref) e local possuidor: funde.
func TestArrayParameterFusesIndex(t *testing.T) {
	ops := functionOpcodes(t, `
func sum(data: int[]) -> int
    let s: int = 0
    let i: int = 0
    while i < 3 do
        s = s + data[i]
        i = i + 1
    end
    data[0] = s
    return s
end
`, "sum")
	assertHas(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
}

// for-each sobre array: o item e lido pela forma fundida no slot $collection.
func TestForEachOverArrayUsesFusedRead(t *testing.T) {
	ops := functionOpcodes(t, `
func f(xs: int[]) -> int
    let s: int = 0
    for x in xs do
        s = s + x
    end
    return s
end
`, "f")
	assertHas(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_GET_INDEX")
}

// for-each sobre map continua generico (a colecao iterada e o array de chaves
// sem tipo estatico).
func TestForEachOverMapKeepsGenericRead(t *testing.T) {
	ops := functionOpcodes(t, `
func f(m: map[string, int]) -> int
    let n: int = 0
    for k in m do
        n = n + 1
    end
    return n
end
`, "f")
	assertHas(t, ops, "OP_GET_INDEX")
	assertLacks(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
}

// Elemento composto em local: leitura funde (leitura nao tem RC), escrita nao.
func TestLocalCompositeArrayFusesReadOnly(t *testing.T) {
	ops := functionOpcodes(t, `
struct P
    x: int
end
func f() -> P
    let ps: P[] = [P(1), P(2)]
    ps[0] = ps[1]
    return ps[0]
end
`, "f")
	assertHas(t, ops, "OP_GET_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_LOCAL_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_SET_INDEX")
}

// Parametro `ref T[]`: leitura e escrita pela forma fundida de ref — sem
// OP_DEREF/OP_DEREF_MUT (o opcode resolve a caixa), sem OP_POP.
func TestRefArrayParameterFusesIndex(t *testing.T) {
	// (as escritas vem ANTES do `if` de proposito: no fim de um bloco o
	// compilador emite OP_POP para os locais do bloco, e assertNotFollowedByPop
	// confundiria esse pop de escopo com o pop da atribuicao.)
	ops := functionOpcodes(t, `
func bubble(data: ref int[]) -> void
    let j: int = 0
    let tmp: int = data[j]
    data[j] = data[j + 1]
    data[j + 1] = tmp
    if data[j] > data[j + 1] then
        j = j + 1
    end
end
`, "bubble")
	assertHas(t, ops, "OP_GET_REF_LOCAL_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_REF_LOCAL_INDEX_ARRAY_NORC")
	assertLacks(t, ops, "OP_DEREF")
	assertLacks(t, ops, "OP_DEREF_MUT")
	assertLacks(t, ops, "OP_GET_LOCAL_MUT_BORROW")
	assertLacks(t, ops, "OP_GET_INDEX")
	assertLacks(t, ops, "OP_SET_INDEX")
	assertNotFollowedByPop(t, ops, "OP_SET_REF_LOCAL_INDEX_ARRAY_NORC")
}

// Com chamada no operando, o ref segue o caminho de hoje (DEREF / DEREF_MUT)
// com os opcodes tipados genericos.
func TestRefArrayParameterDoesNotFuseWithCall(t *testing.T) {
	ops := functionOpcodes(t, `
func g() -> int
    return 0
end
func f(data: ref int[]) -> int
    data[g()] = 1
    return data[g()]
end
`, "f")
	assertLacks(t, ops, "OP_GET_REF_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_REF_LOCAL_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_DEREF")
	assertHas(t, ops, "OP_DEREF_MUT")
	assertHas(t, ops, "OP_GET_INDEX_ARRAY")
	assertHas(t, ops, "OP_SET_INDEX_ARRAY_NORC")
}

// `ref P[]` (elemento composto): leitura funde, escrita fica no generico.
func TestRefCompositeArrayFusesReadOnly(t *testing.T) {
	ops := functionOpcodes(t, `
struct P
    x: int
end
func f(ps: ref P[]) -> P
    ps[0] = ps[1]
    return ps[0]
end
`, "f")
	assertHas(t, ops, "OP_GET_REF_LOCAL_INDEX_ARRAY")
	assertLacks(t, ops, "OP_SET_REF_LOCAL_INDEX_ARRAY_NORC")
	assertHas(t, ops, "OP_SET_INDEX")
}
