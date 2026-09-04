package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Fronteira dinâmica, parte 2: com `any` o executor é quem valida índice,
// chave, propriedade e operando — e cada mensagem abaixo é o que o usuário
// vê quando um valor dinâmico não serve. O perfil mostrou esses ramos de
// OP_GET_INDEX/OP_SET_INDEX/OP_GET_PROPERTY/OP_SET_PROPERTY/OP_*_MUT/
// OP_REF_*/OP_SELECT/OP_ZEROS sem nenhum teste. Todas as expectativas foram
// conferidas no binário.

const dynamicBoundaryPrelude = "struct Point\n    x: int\n    y: int\nend\n"

func TestDynamicIndexingAndPropertyErrorsAtRuntime(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"map literal key float", "let k: any = 1.5\nlet m: any = {k: 1}\n", "map key must be int or string"},
		{"map get with array key", "let m: any = {\"a\": 1}\nlet k: any = [1]\nlet v: any = m[k]\n", "map key must be int or string"},
		{"map set with float key", "let m: any = {\"a\": 1}\nlet k: any = 1.5\nm[k] = 1\n", "map key must be int or string"},
		{"array set with string index", "let a: any = [1]\nlet i: any = \"x\"\na[i] = 2\n", "array index must be integer"},
		{"string index with string", "let s: any = \"abc\"\nlet i: any = \"x\"\nlet c: any = s[i]\n", "string index must be integer"},
		{"bytes index with string", "let b: any = b\"ab\"\nlet i: any = \"x\"\nlet c: any = b[i]\n", "bytes index must be integer"},
		{"index an int", "let x: any = 1\nlet v: any = x[0]\n", "cannot index non-array/map/bytes"},
		{"set index on an int", "let x: any = 1\nx[0] = 1\n", "cannot set index on non-array/map"},
		{"property of an int", "let x: any = 1\nlet v: any = x.foo\n", "only instances/maps have properties"},
		{"property of a string", "let s: any = \"s\"\nlet v: any = s.foo\n", "only instances and maps have properties"},
		{"undefined property read", "let p: any = Point(1, 2)\nlet v: any = p.zzz\n", "undefined property 'zzz'"},
		{"set property on an int", "let x: any = 1\nx.foo = 1\n", "only instances and maps have properties"},
		{"nested write through property of an int", "let x: any = 1\nx.foo[0] = 1\n", "only instances/maps have properties"},
		{"nested write through missing property", "let p: any = Point(1, 2)\np.zzz[0] = 1\n", "undefined property 'zzz'"},
		{"nested write through property of a string", "let s: any = \"s\"\ns.foo[0] = 1\n", "only instances and maps have properties"},
		{"nested write through missing map key as property", "let m: any = {\"a\": [1]}\nm.zzz[0] = 1\n", "undefined property 'zzz' in module/map"},
		{"nested write indexing an int", "let x: any = 1\nx[0][0] = 1\n", "cannot index non-array/map in mutation path"},
		{"nested write with string index", "let a: any = [[1]]\nlet i: any = \"x\"\na[i][0] = 1\n", "array index must be integer"},
		{"nested write out of bounds", "let a: any = [[1]]\nlet i: any = 5\na[i][0] = 1\n", "array index out of bounds"},
		{"ref to property of an int", "let x: any = 1\nlet r: any = ref x.foo\n", "Property reference base must be an object"},
		// R2 (spec 2026-08-24-explicit-ref): `let r: any = ref ...` nao le
		// mais implicitamente, entao o '*' explicito e o que forca a
		// validacao da propriedade/indice no momento da leitura.
		{"ref to missing property", "let p: any = Point(1, 2)\nlet r: any = *ref p.zzz\n", "undefined property 'zzz'"},
		{"ref to index out of bounds", "let a: any = [1]\nlet i: any = 9\nlet r: any = *ref a[i]\n", "Index out of bounds"},
		{"zeros with string size", "let n: any = \"s\"\nlet z: any = zeros(n)\n", "zeros size must be integer"},
		{"bitor on bool", "let x: any = true\nlet y: any = 1\nlet r: any = x | y\n", "operands for | must be integers or bytes"},
		{"bitxor on bool", "let x: any = true\nlet y: any = 1\nlet r: any = x ^ y\n", "operands for ^ must be integers or bytes"},
		{"shift right on float", "let x: any = 1.5\nlet y: any = 1\nlet r: any = x >> y\n", "operands for >> must be integers"},
		{"when case on a non-channel", "let x: any = 1\nwhen\n    case chan_recv(x) then\n        print(1)\n    default\n        print(2)\nend\n", "select case expects channel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretVMSource(t, New(), dynamicBoundaryPrelude+tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("esperava %q, obtido %v", tc.want, err)
			}
		})
	}
}

// As mesmas operações com valores válidos seguem a semântica estática: chaves
// string e int, promoção float, escrita aninhada via "propriedade" de map.
func TestDynamicIndexingAndArithmeticHappyPaths(t *testing.T) {
	got := captureVMSource(t, `
let m: any = {"a": 1, "b": 2}
let n: any = {2: "b"}
let ka: any = "a"
let kb: any = 2
m[ka] = 10
n[kb] = "c"
let a: any = 7.5
let b: any = 2.5
let i: any = 7
let j: any = 3
let grid: any = {"row": [1]}
grid.row[0] = 5
test_report([to_str(m[ka]), to_str(n[kb]), to_str(a - b), to_str(a * b), to_str(a / b), to_str(a + b), to_str(i % j), to_str(grid)])
`)
	want := []string{"10", "c", "5.000000", "18.750000", "3.000000", "10.000000", "1", "{row: [5]}"}
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

func TestUndefinedGlobalIsRuntimeErrorOnReadAndNestedWrite(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"read", "print(nao_existe)\n", "undefined global variable 'nao_existe'"},
		{"nested write", "nao_existe2[0] = 1\n", "undefined global variable 'nao_existe2'"},
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

func TestZerosWithRuntimeSizeBuildsZeroFilledArray(t *testing.T) {
	got := captureVMSource(t, "let n: int = 3\ntest_report(zeros(n))\n")
	cells := semArray(t, got)
	if len(cells) != 3 {
		t.Fatalf("zeros(3) = %s, want 3 elementos", got.String())
	}
	for i, cell := range cells {
		if cell.Type != value.VAL_INT || cell.Int() != 0 {
			t.Fatalf("zeros(3)[%d] = %s, want 0", i, cell.String())
		}
	}
}

// for sobre string itera code points; sobre bytes, octetos como int.
func TestForLoopOverStringAndBytes(t *testing.T) {
	got := captureVMSource(t, `
let chars: string[] = []
for c in "héllo" do
    append(ref chars, c)
end
let octets: int[] = []
for b in b"ab" do
    append(ref octets, b)
end
test_report([to_str(chars), to_str(octets)])
`)
	cells := semArray(t, got)
	if len(cells) != 2 {
		t.Fatalf("esperava 2 células, obtido %s", got.String())
	}
	if s, _ := cells[0].Obj.(string); s != "[h, é, l, l, o]" {
		t.Fatalf("chars = %q, want [h, é, l, l, o]", s)
	}
	if s, _ := cells[1].Obj.(string); s != "[97, 98]" {
		t.Fatalf("octets = %q, want [97, 98]", s)
	}
}
