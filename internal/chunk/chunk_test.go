package chunk_test

import (
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
)

// OpCode.String() e o disassembler (`noxy --disassembly`, `DisassembleAll` no
// REPL) não tinham nenhum teste. Um opcode novo esquecido em String() aparece
// como "OP_<n>" e um esquecido em disassembleInstruction como "Unknown
// opcode" — e, pior, desalinha a decodificação de tudo que vem depois.

var numericOpName = regexp.MustCompile(`^OP_[0-9]+$`)

func TestEveryOpcodeHasASymbolicNameWithoutGaps(t *testing.T) {
	seen := map[string]chunk.OpCode{}
	firstUnnamed := -1
	for op := range 256 {
		name := chunk.OpCode(op).String()
		if !strings.HasPrefix(name, "OP_") {
			t.Fatalf("opcode %d: nome %q não começa com OP_", op, name)
		}
		if numericOpName.MatchString(name) {
			if firstUnnamed < 0 {
				firstUnnamed = op
			}
			continue
		}
		if firstUnnamed >= 0 {
			t.Fatalf("opcode %d (%s) tem nome, mas o opcode %d antes dele não: buraco na tabela de String()", op, name, firstUnnamed)
		}
		if prior, dup := seen[name]; dup {
			t.Fatalf("opcodes %d e %d compartilham o nome %q", prior, op, name)
		}
		seen[name] = chunk.OpCode(op)
	}
	// Sentinela: o último opcode declarado em chunk.go precisa estar nomeado.
	// Se você acrescentou um opcode depois dele, atualize aqui também.
	if last := int(chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC); firstUnnamed >= 0 && firstUnnamed <= last {
		t.Fatalf("opcode %d está abaixo do último declarado (%d) e não tem nome em String()", firstUnnamed, last)
	}
}

func compileForDisassembly(t *testing.T, source string) *chunk.Chunk {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.New().Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}
	return code
}

// captureStdout lê o pipe numa goroutine: a saída do disassembler é maior
// que o buffer do pipe, e ler só depois de fechar travaria o escritor.
func captureStdout(t *testing.T, run func()) string {
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
	run()
	_ = writer.Close()
	os.Stdout = previous
	return <-done
}

// Programa representativo: toca os grupos de opcodes com operandos de larguras
// diferentes (constantes curtas/longas, jumps, closures, chamadas, refs,
// fused compare-jump, laços, contêineres, campos, for sobre map/string).
const disassemblyProgram = `
struct Point
    x: int
    y: int
end

func soma(a: int, b: int) -> int
    return a + b
end

func conta(limite: int) -> int
    let total: int = 0
    let i: int = 0
    while i < limite do
        i = i + 1
        if i % 2 == 0 then
            continue
        end
        if i > 7 then
            break
        end
        total = total + i
    end
    return total
end

func fabrica(base: int) -> func(int) -> int
    return func(x: int) -> int
        return base + x
    end
end

func incrementa(r: ref int) -> void
    *r = *r + 1
end

let p: Point = Point(1, 2)
p.x = soma(p.x, 10)
let xs: int[] = [1, 2, 3]
xs[0] = 9
let m: map[string, int] = {"a": 1, "b": 2}
m["c"] = 3
let acc: int = 0
for v in xs do
    acc = acc + v
end
for k in m do
    acc = acc + m[k]
end
for ch in "ab" do
    print(ch)
end
let f: func(int) -> int = fabrica(5)
let n: int = 0
incrementa(ref n)
let msg: string = f"{p.x} {f(1)} {conta(10)} {n} {acc} {2.5 * 2.0} {1.5 > 0.5}"
print(msg)

// Operadores genéricos e especializados, fused compare-jump de cada espécie,
// lógicos com curto-circuito, bitwise, módulo, negação e literais booleanos.
let fa: float = 1.5
let fb: float = 2.5
let sa: string = "a"
let ok: bool = true
let mixed: float = 1 + fa
let fsub: float = fa - fb
let fmul: float = fa * fb
let fdiv: float = fa / fb
let fless: bool = fa < fb
let cmp: bool = sa > "b" || sa < "c" || sa == "a" || fa >= fb || fa <= fb || !ok || false
let bits: int = (6 & 3) | (6 ^ 3) | (~5) | (1 << 2) | (8 >> 1) | (7 % 3) | -(2)
let va: any = 7
let fm: any = va % 3    // OP_MODULO generico: float % float e erro de compilacao (#75)
let ge: bool = 1 >= 2
if 1 == 1 then print("eq") end
if 1 > 0 then print("gt") end
if 1 < 2 then print("lt") end
while acc > 100 do acc = acc - 1 end

// Mutação através de local, upvalue, campo e ref (opcodes *_MUT, BORROW,
// STORE_VIA_REF), defer, zeros, array fixo, use/import, when.
func muta(valores: ref int[]) -> void
    valores[0] = 42
    let local: int[] = [1]
    local[0] = 2
    let contador: int[] = [0]
    let incr: func() -> void = func() -> void
        contador[0] = contador[0] + 1
    end
    incr()
    let rl: ref int[] = ref local
    rl[0] = 3
    // indexacao tipada (issue #66): leitura fundida de local e de ref local,
    // leitura/escrita tipadas genericas no nivel de dentro de um nested.
    print(local[0] + rl[0])
    let g: int[][] = [[1, 2], [3, 4]]
    g[0][1] = g[1][0]
    print(g[0][1])
    let pt: Point = Point(1, 2)
    let rp: ref Point = ref pt
    rp.x = 5
    let rx: ref int = ref pt.x
    *rx = 6
    let e: ref int = ref local[0]
    *e = 7
    defer print("fim")
    let zs: int[] = zeros(2)
    let fixo: int[3]
    print(addr(ref pt))
end
muta(ref xs)
use strings
use strings select to_upper
print(to_upper("a"), strings.to_lower("B"))
let ch: chan int = make_chan(1)
chan_send(ch, 1)
when
    case v = chan_recv(ch) then
        print(v)
    case chan_send(ch, 2) then
        print("sent")
    default
        print("none")
end
`

func TestDisassemblerDecodesEveryEmittedInstruction(t *testing.T) {
	code := compileForDisassembly(t, disassemblyProgram)
	out := captureStdout(t, func() { code.DisassembleAll("test") })
	if strings.Contains(out, "Unknown opcode") {
		t.Fatalf("disassembler encontrou opcode sem decodificação:\n%s", out)
	}
	// Cada função constante é listada com seu próprio cabeçalho e o chunk
	// principal também: o disassembler percorre a árvore inteira.
	for _, name := range []string{"== test ==", "soma", "conta", "fabrica", "incrementa"} {
		if !strings.Contains(out, name) {
			t.Fatalf("saída do disassembler deveria mencionar %q:\n%s", name, out)
		}
	}
	// A última instrução do chunk principal é um retorno do script, e cada
	// linha de instrução começa com o offset de 4 dígitos.
	lines := strings.Split(strings.TrimSpace(out), "\n")
	instructionLines := 0
	for _, line := range lines {
		if len(line) >= 5 && line[4] == ' ' && strings.Trim(line[:4], "0123456789") == "" {
			instructionLines++
		}
	}
	if instructionLines < 50 {
		t.Fatalf("esperava dezenas de instruções decodificadas, obtido %d:\n%s", instructionLines, out)
	}
}

// Write/AddConstant/TruncateTo são a API que o compilador usa para emitir e
// desfazer bytecode (rollback do compare-jump fundido): o tamanho de Code e
// Lines andam juntos e TruncateTo recua os dois.
func TestWriteTracksLinesAndTruncateToRewindsBoth(t *testing.T) {
	c := chunk.New()
	c.Write(byte(chunk.OP_NULL), 1)
	c.Write(byte(chunk.OP_POP), 1)
	c.Write(byte(chunk.OP_NULL), 2)
	if len(c.Code) != 3 || len(c.Lines) != 3 || c.Lines[2] != 2 {
		t.Fatalf("Code=%v Lines=%v, want 3 bytes com a terceira na linha 2", c.Code, c.Lines)
	}
	c.TruncateTo(1)
	if len(c.Code) != 1 || len(c.Lines) != 1 {
		t.Fatalf("após TruncateTo(1): Code=%v Lines=%v", c.Code, c.Lines)
	}
	if index := c.AddConstant(value.NewInt(7)); index != 0 || len(c.Constants) != 1 {
		t.Fatalf("AddConstant devolveu %d com %d constantes, want 0 e 1", index, len(c.Constants))
	}
}
