package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Testes de semântica aritmética/relacional do executor (spec §8) que o
// perfil de cobertura mostrou sem nenhum teste Go: o opcode especializado
// OP_GREATER_FLOAT, os ramos mistos int/float de OP_SUBTRACT/OP_MULTIPLY/
// OP_DIVIDE, e todos os erros de tipo do caminho genérico (operandos `any`).
// Cada expectativa foi conferida no binário atual antes de virar asserção.

// semArray devolve os elementos de um array reportado por test_report.
func semArray(t *testing.T, v value.Value) []value.Value {
	t.Helper()
	arr, ok := v.Obj.(*value.ObjArray)
	if v.Type != value.VAL_OBJ || !ok || arr == nil {
		t.Fatalf("test_report recebeu %s, esperava array", v.String())
	}
	return arr.Elements
}

// chunkTreeEmits procura opcode no chunk principal e, recursivamente, em
// toda função constante. Mesma heurística byte-a-byte de containsOpcode em
// internal/compiler: um operando pode coincidir com o valor do opcode, mas
// nos programas curtos daqui isso não acontece.
func chunkTreeEmits(c *chunk.Chunk, opcode chunk.OpCode) bool {
	for _, instruction := range c.Code {
		if chunk.OpCode(instruction) == opcode {
			return true
		}
	}
	for _, constant := range c.Constants {
		if constant.Type != value.VAL_FUNCTION {
			continue
		}
		fn, ok := constant.Obj.(*value.ObjFunction)
		if !ok || fn == nil {
			continue
		}
		if body, ok := fn.Chunk.(*chunk.Chunk); ok && body != nil && chunkTreeEmits(body, opcode) {
			return true
		}
	}
	return false
}

func TestFloatComparisonsUseSpecializedOpcodesAndOrderCorrectly(t *testing.T) {
	source := `
func cmp(a: float, b: float) -> bool[]
    return [a > b, a < b, a >= b, a <= b, a == b, a != b]
end
test_report([cmp(1.5, 0.5), cmp(0.5, 1.5), cmp(2.0, 2.0)])
`
	code := compileVMSource(t, source)
	// `>`/`<` com os dois lados float vão pelos irmãos float dos opcodes
	// _INT; `>=`/`<=` continuam no genérico (OP_LESS/OP_GREATER + OP_NOT).
	if !chunkTreeEmits(code, chunk.OP_GREATER_FLOAT) || !chunkTreeEmits(code, chunk.OP_LESS_FLOAT) {
		t.Fatalf("comparação float/float deveria emitir OP_GREATER_FLOAT e OP_LESS_FLOAT")
	}
	got := captureVMSource(t, source)
	want := [][]bool{
		{true, false, true, false, false, true}, // 1.5 ? 0.5
		{false, true, false, true, false, true}, // 0.5 ? 1.5
		{false, false, true, true, true, false}, // 2.0 ? 2.0
	}
	rows := semArray(t, got)
	if len(rows) != len(want) {
		t.Fatalf("linhas=%d, want %d", len(rows), len(want))
	}
	for i, row := range rows {
		cells := semArray(t, row)
		for j, cell := range cells {
			if cell.Type != value.VAL_BOOL || cell.Bool() != want[i][j] {
				t.Fatalf("linha %d coluna %d: got %s, want %v", i, j, cell.String(), want[i][j])
			}
		}
	}
}

func TestFloatDivisionSpecializedOpcodeResult(t *testing.T) {
	source := `
func div(a: float, b: float) -> float
    return a / b
end
test_report(div(7.0, 2.0))
`
	if !chunkTreeEmits(compileVMSource(t, source), chunk.OP_DIV_FLOAT) {
		t.Fatalf("divisão float/float deveria emitir OP_DIV_FLOAT")
	}
	got := captureVMSource(t, source)
	if got.Type != value.VAL_FLOAT || got.Float() != 3.5 {
		t.Fatalf("7.0 / 2.0 = %s, want 3.500000", got.String())
	}
}

// Promoção numérica (spec §8): misto int/float cai no caminho genérico, que
// promove para float em qualquer ordem dos operandos. Os casos aqui cobrem os
// ramos int×float e float×int de OP_ADD/OP_SUBTRACT/OP_MULTIPLY/OP_DIVIDE.
func TestMixedIntFloatArithmeticPromotesToFloatInBothOrders(t *testing.T) {
	got := captureVMSource(t, `
func calc() -> float[]
    let i: int = 3
    let x: float = 0.5
    return [i + x, x + i, i - x, x - i, i * x, x * i, i / x, x / i]
end
test_report(calc())
`)
	want := []float64{3.5, 3.5, 2.5, -2.5, 1.5, 1.5, 6.0, 0.5 / 3.0}
	cells := semArray(t, got)
	if len(cells) != len(want) {
		t.Fatalf("células=%d, want %d", len(cells), len(want))
	}
	for i, cell := range cells {
		if cell.Type != value.VAL_FLOAT || cell.Float() != want[i] {
			t.Fatalf("célula %d: got %s, want %v", i, cell.String(), want[i])
		}
	}
}

func TestMixedIntFloatComparisonsAndEquality(t *testing.T) {
	got := captureVMSource(t, `
func cmp() -> bool[]
    let i: int = 1
    let x: float = 1.5
    let one: float = 1.0
    return [i < x, x < i, i > x, x > i, i == one, one == i, i != one, i >= one, i <= x, x >= i]
end
test_report(cmp())
`)
	want := []bool{true, false, false, true, true, true, false, true, true, true}
	cells := semArray(t, got)
	if len(cells) != len(want) {
		t.Fatalf("células=%d, want %d", len(cells), len(want))
	}
	for i, cell := range cells {
		if cell.Type != value.VAL_BOOL || cell.Bool() != want[i] {
			t.Fatalf("célula %d: got %s, want %v", i, cell.String(), want[i])
		}
	}
}

func TestMixedArithmeticHasStaticTypeFloat(t *testing.T) {
	machine := New()
	err := interpretOrCompileErr(t, machine, "let n: int = 1 + 2.5\n")
	if err == nil || !strings.Contains(err.Error(), "got float") {
		t.Fatalf("int = int + float deveria ser erro de tipo (got float), obtido %v", err)
	}
	got := captureVMSource(t, "let f: float = 1 + 2.5\ntest_report(f)\n")
	if got.Type != value.VAL_FLOAT || got.Float() != 3.5 {
		t.Fatalf("float = int + float: got %s, want 3.500000", got.String())
	}
}

// Divisão e resto inteiros truncam em direção a zero (como Go), inclusive o
// sinal do resto.
func TestIntegerDivisionAndModuloTruncateTowardZero(t *testing.T) {
	got := captureVMSource(t, `
func calc() -> int[]
    let seven: int = 7
    let neg_seven: int = -7
    let two: int = 2
    let three: int = 3
    let neg_three: int = -3
    return [seven / two, neg_seven / two, seven % three, neg_seven % three, seven % neg_three]
end
test_report(calc())
`)
	want := []int64{3, -3, 1, -1, 1}
	cells := semArray(t, got)
	for i, cell := range cells {
		if cell.Type != value.VAL_INT || cell.Int() != want[i] {
			t.Fatalf("célula %d: got %s, want %d", i, cell.String(), want[i])
		}
	}
}

func TestDivisionByZeroIsRuntimeErrorForEveryOperandCombination(t *testing.T) {
	cases := []struct{ name, source string }{
		{"int/int", "let z: int = 0\nlet a: int = 7\ntest_report(a / z)\n"},
		{"int/float", "let z: float = 0.0\nlet a: int = 7\ntest_report(a / z)\n"},
		{"float/int", "let z: int = 0\nlet a: float = 7.5\ntest_report(a / z)\n"},
		{"float/float", "let z: float = 0.0\nlet a: float = 7.5\ntest_report(a / z)\n"},
		{"any/any", "let z: any = 0\nlet a: any = 7\ntest_report(a / z)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machine := New()
			machine.DefineNative("test_report", func(args []value.Value) value.Value { return value.NewNull() })
			err := interpretVMSource(t, machine, tc.source)
			if err == nil || !strings.Contains(err.Error(), "division by zero") {
				t.Fatalf("esperava 'division by zero', obtido %v", err)
			}
		})
	}
}

func TestModuloByZeroIsRuntimeError(t *testing.T) {
	cases := []struct{ name, source string }{
		{"int typed (OP_MOD_INT)", "let z: int = 0\nlet a: int = 7\nprint(a % z)\n"},
		{"any (OP_MODULO)", "let z: any = 0\nlet a: any = 7\nprint(a % z)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretVMSource(t, New(), tc.source)
			if err == nil || !strings.Contains(err.Error(), "modulo by zero") {
				t.Fatalf("esperava 'modulo by zero', obtido %v", err)
			}
		})
	}
}

func TestNegativeShiftCountIsRuntimeError(t *testing.T) {
	for _, source := range []string{
		"let n: int = -1\nprint(1 << n)\n",
		"let n: int = -1\nprint(16 >> n)\n",
	} {
		err := interpretVMSource(t, New(), source)
		if err == nil || !strings.Contains(err.Error(), "negative shift count") {
			t.Fatalf("%q: esperava 'negative shift count', obtido %v", source, err)
		}
	}
}

func TestIntegerBitwiseAndShiftResults(t *testing.T) {
	got := captureVMSource(t, `
func calc() -> int[]
    let six: int = 6
    let three: int = 3
    let five: int = 5
    let one: int = 1
    let four: int = 4
    let big: int = 256
    let two: int = 2
    let neg: int = -16
    return [six & three, six | three, six ^ three, ~five, one << four, big >> two, neg >> two]
end
test_report(calc())
`)
	// -16 >> 2 = -4: deslocamento aritmético (preserva o sinal), como Go.
	want := []int64{2, 7, 5, -6, 16, 64, -4}
	cells := semArray(t, got)
	for i, cell := range cells {
		if cell.Type != value.VAL_INT || cell.Int() != want[i] {
			t.Fatalf("célula %d: got %s, want %d", i, cell.String(), want[i])
		}
	}
}

// Fronteira dinâmica: com operandos `any` a checagem de tipo acontece em
// runtime, com as mensagens do executor. Cada linha exercita um ramo de erro
// que a tipagem estática torna inalcançável de outra forma.
func TestDynamicOperandTypeErrorsAtRuntime(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"not on int", "let x: any = 1\nlet r: any = !x\n", "operand must be boolean"},
		{"negate string", "let x: any = \"s\"\nlet r: any = -x\n", "operand must be number"},
		{"add bool and int", "let x: any = true\nlet y: any = 1\nlet r: any = x + y\n", "operands must be numbers or strings or bytes"},
		{"add string and int", "let x: any = \"s\"\nlet y: any = 1\nlet r: any = x + y\n", "operands must be numbers or strings or bytes"},
		{"subtract string", "let x: any = \"s\"\nlet y: any = 1\nlet r: any = x - y\n", "operands must be numbers"},
		{"multiply string", "let x: any = \"s\"\nlet y: any = 1\nlet r: any = x * y\n", "operands must be numbers"},
		{"divide string", "let x: any = \"s\"\nlet y: any = 1\nlet r: any = x / y\n", "operands must be numbers"},
		{"modulo float", "let x: any = 1.5\nlet y: any = 1\nlet r: any = x % y\n", "operands for % must be integers"},
		{"greater on bools", "let x: any = true\nlet y: any = false\nlet r: any = x > y\n", "operands must be numbers or strings"},
		{"less on arrays", "let x: any = [1]\nlet y: any = [2]\nlet r: any = x < y\n", "operands must be numbers or strings"},
		{"bitand bool", "let x: any = true\nlet y: any = 1\nlet r: any = x & y\n", "operands for & must be integers or bytes"},
		{"shift float", "let x: any = 1.5\nlet y: any = 1\nlet r: any = x << y\n", "operands for << must be integers"},
		{"if condition int", "let x: any = 1\nif x then\n    print(1)\nend\n", "condition must be bool, got int"},
		{"while condition string", "let x: any = \"s\"\nwhile x do\n    print(1)\nend\n", "condition must be bool, got string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretVMSource(t, New(), tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("esperava %q, obtido %v", tc.want, err)
			}
		})
	}
}

// O mesmo caminho genérico aceita o que a spec permite: promoção numérica,
// concatenação e ordenação de strings também valem para operandos `any`.
func TestDynamicOperandsStillFollowTheStaticRules(t *testing.T) {
	got := captureVMSource(t, `
let i: any = 1
let x: any = 2.5
let a: any = "a"
let b: any = "b"
let sum: any = i + x
let cat: any = a + b
let less: any = a < b
test_report([sum == 3.5, cat == "ab", less])
`)
	for i, cell := range semArray(t, got) {
		if cell.Type != value.VAL_BOOL || !cell.Bool() {
			t.Fatalf("célula %d: got %s, want true", i, cell.String())
		}
	}
}

// `%` entre floats tipados estaticamente não é rejeitado pelo compilador;
// cai em OP_MODULO e falha em runtime. Caracterização do comportamento atual
// (a spec §8 só define `%` para inteiros via a mensagem de erro).
func TestStaticFloatModuloIsRuntimeError(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "let a: float = 1.5\nlet b: float = 1.0\nprint(a % b)\n")
	if err == nil || !strings.Contains(err.Error(), "operands for % must be integers") {
		t.Fatalf("esperava 'operands for %% must be integers', obtido %v", err)
	}
}

// bytes (spec §2.1/§8): `+` concatena; `&`, `|`, `^` operam octeto a octeto e
// exigem o mesmo comprimento; indexação devolve o octeto como int.
func TestBytesConcatBitwiseAndIndexing(t *testing.T) {
	got := captureVMSource(t, `
let a: bytes = b"\x0f\xf0"
let b: bytes = b"\xff\x00"
test_report([hex_encode(a & b), hex_encode(a | b), hex_encode(a ^ b), hex_encode(a + b), to_str(length(a + b)), to_str(a[0]), to_str(a[1])])
`)
	want := []string{"0f00", "fff0", "f0f0", "0ff0ff00", "4", "15", "240"}
	cells := semArray(t, got)
	if len(cells) != len(want) {
		t.Fatalf("células=%d, want %d", len(cells), len(want))
	}
	for i, cell := range cells {
		if s, ok := cell.Obj.(string); !ok || s != want[i] {
			t.Fatalf("célula %d: got %s, want %q", i, cell.String(), want[i])
		}
	}
}

func TestBytesBitwiseOperatorsRequireSameLength(t *testing.T) {
	for _, op := range []string{"&", "|", "^"} {
		source := "let a: bytes = b\"\\x01\"\nlet b: bytes = b\"\\x01\\x02\"\nprint(a " + op + " b)\n"
		err := interpretVMSource(t, New(), source)
		want := "operands for " + op + " must have same length"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: esperava %q, obtido %v", op, want, err)
		}
	}
}

// Indexação de string é por code point (spec §12), não por byte; fora da
// faixa (para os dois lados) é erro de runtime, assim como em bytes.
func TestStringIndexingIsByCodePointAndBoundsChecked(t *testing.T) {
	got := captureVMSource(t, "let s: string = \"héllo\"\ntest_report([s[0], s[1], s[4], to_str(length(s))])\n")
	want := []string{"h", "é", "o", "5"}
	for i, cell := range semArray(t, got) {
		if s, ok := cell.Obj.(string); !ok || s != want[i] {
			t.Fatalf("célula %d: got %s, want %q", i, cell.String(), want[i])
		}
	}
	// ASCII toma o ramo por byte (issue #66, item 2): mesmo resultado e mesma
	// mensagem de erro que o ramo por rune.
	gotASCII := captureVMSource(t, "let s: string = \"item_1\"\ntest_report([s[0], s[4], s[5]])\n")
	wantASCII := []string{"i", "_", "1"}
	for i, cell := range semArray(t, gotASCII) {
		if s, ok := cell.Obj.(string); !ok || s != wantASCII[i] {
			t.Fatalf("ascii célula %d: got %s, want %q", i, cell.String(), wantASCII[i])
		}
	}
	cases := []struct{ name, source, want string }{
		{"string past end", "let s: string = \"abc\"\nlet i: int = 3\nprint(s[i])\n", "string index out of bounds"},
		{"string negative", "let s: string = \"abc\"\nlet i: int = -1\nprint(s[i])\n", "string index out of bounds"},
		{"ascii past end", "let s: string = \"item_1\"\nlet i: int = 6\nprint(s[i])\n", "string index out of bounds"},
		{"ascii negative", "let s: string = \"item_1\"\nlet i: int = -1\nprint(s[i])\n", "string index out of bounds"},
		{"empty string", "let s: string = \"\"\nlet i: int = 0\nprint(s[i])\n", "string index out of bounds"},
		{"accent past end", "let s: string = \"héllo\"\nlet i: int = 5\nprint(s[i])\n", "string index out of bounds"},
		{"bytes past end", "let b: bytes = b\"ab\"\nlet i: int = 2\nprint(b[i])\n", "bytes index out of bounds"},
		{"array past end", "let a: int[] = [1]\nlet i: int = 5\nprint(a[i])\n", "array index out of bounds"},
		{"array set past end", "let a: int[] = [1]\nlet i: int = 5\na[i] = 2\n", "array index out of bounds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretVMSource(t, New(), tc.source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("esperava %q, obtido %v", tc.want, err)
			}
		})
	}
}

// Chave de map via `any` com tipo não indexável: o executor rejeita com a
// mensagem do caminho dinâmico (o compilador não vê o tipo).
func TestDynamicMapKeyMustBeIntOrString(t *testing.T) {
	err := interpretVMSource(t, New(), "let m: any = {\"a\": 1}\nlet k: any = 1.5\nprint(m[k])\n")
	if err == nil || !strings.Contains(err.Error(), "map key must be int or string") {
		t.Fatalf("esperava 'map key must be int or string', obtido %v", err)
	}
}
