package compiler

import (
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
)

// Campo de struct por índice em compilação (issue #96). Quando o tipo
// estático da base é um struct do PROGRAMA, `p.x` resolve para o índice de
// slot na declaração e o compilador emite OP_GET_FIELD / OP_SET_FIELD /
// OP_GET_FIELD_MUT com `[idx][nome]`; base `any` e struct de módulo ficam nos
// opcodes por nome. Cada regra tem caso positivo e negativo, lidos da saída
// do disassembler (que conhece a largura de cada operando).

const fieldIndexPrelude = `
struct Ponto
    x: int
    y: int
end
struct Caixa
    tag: string
    p: Ponto
end
`

// disassemblyText devolve o texto do disassembler de um chunk, para afirmar
// o OPERANDO (índice e nome) e não só o nome do opcode.
func disassemblyText(t *testing.T, code *chunk.Chunk) string {
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
	return <-done
}

func compiledFunctionChunk(t *testing.T, source, name string) *chunk.Chunk {
	t.Helper()
	code, _, err := New().Compile(parse(source))
	if err != nil {
		t.Fatal(err)
	}
	fnChunk := findFunctionChunk(code, name)
	if fnChunk == nil {
		t.Fatalf("function %q not found", name)
	}
	return fnChunk
}

func assertFieldOperand(t *testing.T, text, op string, idx int, name string) {
	t.Helper()
	pattern := regexp.MustCompile(op + `\s+` + strconv.Itoa(idx) + ` '` + regexp.QuoteMeta(name) + `'`)
	if !pattern.MatchString(text) {
		t.Fatalf("esperava %s %d '%s' no disassembly:\n%s", op, idx, name, text)
	}
}

func TestTypedStructFieldReadUsesSlotIndex(t *testing.T) {
	fn := compiledFunctionChunk(t, fieldIndexPrelude+`
func f(p: Ponto) -> int
    return p.y
end
`, "f")
	ops := opcodeNames(t, fn)
	assertHas(t, ops, "OP_GET_FIELD")
	assertLacks(t, ops, "OP_GET_PROPERTY")
	assertFieldOperand(t, disassemblyText(t, fn), "OP_GET_FIELD", 1, "y")
}

func TestAnyBaseFieldReadStaysByName(t *testing.T) {
	ops := functionOpcodes(t, fieldIndexPrelude+`
func g(a: any) -> any
    return a.y
end
`, "g")
	assertHas(t, ops, "OP_GET_PROPERTY")
	assertLacks(t, ops, "OP_GET_FIELD")
}

func TestRefStructBaseDerefsThenReadsBySlotIndex(t *testing.T) {
	ops := functionOpcodes(t, fieldIndexPrelude+`
func f(r: ref Ponto) -> int
    return r.x
end
`, "f")
	assertHas(t, ops, "OP_GET_FIELD")
	assertLacks(t, ops, "OP_GET_PROPERTY")
	for k, name := range ops {
		if name == "OP_GET_FIELD" {
			if k == 0 || ops[k-1] != "OP_DEREF" {
				t.Fatalf("OP_GET_FIELD sobre base ref precisa vir depois de OP_DEREF: %s", strings.Join(ops, " "))
			}
		}
	}
}

func TestTypedStructFieldWriteIsStatementBySlotIndex(t *testing.T) {
	fn := compiledFunctionChunk(t, fieldIndexPrelude+`
func f(p: Ponto) -> int
    p.y = 5
    return p.y
end
`, "f")
	ops := opcodeNames(t, fn)
	assertHas(t, ops, "OP_SET_FIELD")
	assertLacks(t, ops, "OP_SET_PROPERTY")
	assertNotFollowedByPop(t, ops, "OP_SET_FIELD")
	assertFieldOperand(t, disassemblyText(t, fn), "OP_SET_FIELD", 1, "y")
}

func TestAnyBaseFieldWriteStaysByName(t *testing.T) {
	ops := functionOpcodes(t, fieldIndexPrelude+`
func g(a: any) -> void
    a.y = 5
end
`, "g")
	assertHas(t, ops, "OP_SET_PROPERTY")
	assertLacks(t, ops, "OP_SET_FIELD")
}

func TestNestedFieldWriteUsesMutBySlotIndex(t *testing.T) {
	fn := compiledFunctionChunk(t, fieldIndexPrelude+`
func f(c: Caixa) -> int
    c.p.x = 9
    return c.p.x
end
`, "f")
	ops := opcodeNames(t, fn)
	assertHas(t, ops, "OP_GET_FIELD_MUT")
	assertHas(t, ops, "OP_SET_FIELD")
	assertLacks(t, ops, "OP_GET_PROP_MUT")
	assertLacks(t, ops, "OP_SET_PROPERTY")
	text := disassemblyText(t, fn)
	assertFieldOperand(t, text, "OP_GET_FIELD_MUT", 1, "p")
	assertFieldOperand(t, text, "OP_SET_FIELD", 0, "x")
}

func TestAnyBaseNestedFieldWriteStaysByName(t *testing.T) {
	ops := functionOpcodes(t, fieldIndexPrelude+`
func g(a: any) -> void
    a.p.x = 9
end
`, "g")
	assertHas(t, ops, "OP_GET_PROP_MUT")
	assertHas(t, ops, "OP_SET_PROPERTY")
	assertLacks(t, ops, "OP_GET_FIELD_MUT")
	assertLacks(t, ops, "OP_SET_FIELD")
}

func TestGlobalAndArrayElementStructBasesUseSlotIndex(t *testing.T) {
	ops := topLevelOpcodes(t, fieldIndexPrelude+`
let p: Ponto = Ponto(1, 2)
let xs: Ponto[] = [p]
let i: int = 0
xs[i].y = p.x + xs[i].y
`)
	assertHas(t, ops, "OP_GET_FIELD")
	assertHas(t, ops, "OP_SET_FIELD")
	assertLacks(t, ops, "OP_GET_PROPERTY")
	assertLacks(t, ops, "OP_SET_PROPERTY")
}

func TestGenericStructInstanceUsesSlotIndex(t *testing.T) {
	fn := compiledFunctionChunk(t, `
struct Pilha<T>
    nome: string
    itens: T[]
end
func f(s: Pilha<int>) -> int
    return length(s.itens)
end
`, "f")
	ops := opcodeNames(t, fn)
	assertHas(t, ops, "OP_GET_FIELD")
	assertLacks(t, ops, "OP_GET_PROPERTY")
	assertFieldOperand(t, disassemblyText(t, fn), "OP_GET_FIELD", 1, "itens")
}

// Campo declarado `ref T`: a escrita por base tipada continua por índice — a
// checagem do slot ref é do runtime (o fast path recusa e o genérico decide).
func TestRefTypedFieldWriteUsesSlotIndex(t *testing.T) {
	ops := functionOpcodes(t, `
struct Node
    v: int
    next: ref Node
end
func f(a: Node, b: Node) -> void
    a.next = ref b
    a.next = null
end
`, "f")
	assertHas(t, ops, "OP_SET_FIELD")
	assertLacks(t, ops, "OP_SET_PROPERTY")
}

// `ref p.x` fica em OP_REF_PROPERTY: o ObjRef resolve por nome (issue #93b).
func TestBorrowOfFieldStaysByName(t *testing.T) {
	ops := functionOpcodes(t, fieldIndexPrelude+`
func bump(r: ref int) -> void
    *r = *r + 1
end
func f(p: Ponto) -> void
    bump(ref p.y)
end
`, "f")
	assertHas(t, ops, "OP_REF_PROPERTY")
	assertLacks(t, ops, "OP_GET_FIELD")
}
