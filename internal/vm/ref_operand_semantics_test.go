package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Refs como operandos (spec §2.3 e §7): a condição de `while`/`if` com um
// `ref bool` é dereferenciada automaticamente; um ref no lado DIREITO de um
// infix e como operando de um unário também é lido (o compilador emite
// OP_DEREF); `*r = s` com s: ref T grava o valor apontado por s; e um índice
// que é `ref int` vale como o int apontado tanto em `ref arr[ri]` quanto em
// `arr[ri] = v`. O perfil mostrou cada um desses caminhos do compilador sem
// teste Go.

func TestWhileAndIfConditionsDereferenceRefBool(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let flag: bool = true
    let rf: ref bool = ref flag
    let n: int = 0
    while rf do
        n = n + 1
        flag = false
    end
    let after: int = 0
    if rf then
        after = 1
    else
        after = 2
    end
    test_report([n, after])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 2 || cells[0].Int() != 1 || cells[1].Int() != 2 {
		t.Fatalf("[iterações, ramo do if] = %s, want [1, 2]", got.String())
	}
}

func TestInfixRightOperandAndUnaryOperandDereferenceRefs(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let x: int = 10
    let rx: ref int = ref x
    let flag: bool = false
    let rb: ref bool = ref flag
    let ints: int[] = [1 + rx, rx + 1, rx * rx, -rx]
    let bools: bool[] = [5 > rx, rx < 5, !rb]
    test_report([to_str(ints), to_str(bools)])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 2 {
		t.Fatalf("esperava 2 células, obtido %s", got.String())
	}
	if s, _ := cells[0].Obj.(string); s != "[11, 11, 100, -10]" {
		t.Fatalf("ints = %q, want [11, 11, 100, -10]", s)
	}
	if s, _ := cells[1].Obj.(string); s != "[false, false, true]" {
		t.Fatalf("bools = %q, want [false, false, true]", s)
	}
}

// R2: `*r = *s` copia o valor apontado por s (o RHS `s` sem '*' e erro de
// compilacao — explicit_read_test.go). E copia, nao aliasing: mudar y
// depois nao alcanca x.
func TestDerefAssignmentFromRefRHSCopiesPointedValue(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let x: int = 10
    let y: int = 3
    let rx: ref int = ref x
    let ry: ref int = ref y
    *rx = *ry
    let x_after_assign: int = x
    y = 4
    test_report([x_after_assign, x, y])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 3 || cells[0].Int() != 3 || cells[1].Int() != 3 || cells[2].Int() != 4 {
		t.Fatalf("[x após *rx = ry, x após y = 4, y] = %s, want [3, 3, 4]", got.String())
	}
}

func TestRefIntIndexIsDereferencedInIndexExpressions(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let arr: int[] = [10, 20, 30]
    let i: int = 1
    let ri: ref int = ref i
    let re: ref int = ref arr[ri]
    *re = 200
    let after_ref: int = arr[1]
    arr[ri] = 222
    test_report([after_ref, arr[0], arr[1], arr[2]])
end
main()
`)
	cells := semArray(t, got)
	want := []int64{200, 10, 222, 30}
	for i, cell := range cells {
		if cell.Type != value.VAL_INT || cell.Int() != want[i] {
			t.Fatalf("célula %d: got %s, want %d (tudo: %s)", i, cell.String(), want[i], got.String())
		}
	}
}

func TestDerefAssignmentIsTypeCheckedAtCompileTime(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"wrong value type", "let x: int = 1\nlet r: ref int = ref x\n*r = \"s\"\n", "type mismatch in assignment: expected int, got string"},
		{"target is not a ref", "let x: int = 1\n*x = 2\n", "cannot dereference non-reference type int in assignment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretOrCompileErr(t, New(), tc.source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("esperava %q, obtido %v", tc.want, err)
			}
		})
	}
}

// addr(ref x) é a função de depuração que expõe a identidade do alvo; só
// aceita referência e devolve uma string não vazia.
func TestAddrRequiresAReferenceAndReturnsAString(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "let x: int = 1\nprint(addr(x))\n")
	if err == nil || !strings.Contains(err.Error(), "addr() requires a reference") {
		t.Fatalf("addr(x) deveria ser rejeitado, obtido %v", err)
	}
	got := captureVMSource(t, "let x: int = 1\ntest_report(addr(ref x))\n")
	if s, ok := got.Obj.(string); !ok || s == "" {
		t.Fatalf("addr(ref x) deveria devolver string não vazia, obtido %s", got.String())
	}
}

// `let b: bytes` sem inicializador começa vazio, como `let s: string` começa
// em "" e `let m: map[...]` em {} (inicialização padrão do compilador).
func TestDefaultInitializationOfBytesMapAndString(t *testing.T) {
	got := captureVMSource(t, `
let b: bytes
let m: map[string, int]
let s: string
let arr: int[]
let f: float
let ok: bool
test_report([to_str(length(b)), to_str(m), s, to_str(arr), to_str(f), to_str(ok)])
`)
	want := []string{"0", "{}", "", "[]", "0.000000", "false"}
	for i, cell := range semArray(t, got) {
		if s, ok := cell.Obj.(string); !ok || s != want[i] {
			t.Fatalf("célula %d: got %s, want %q", i, cell.String(), want[i])
		}
	}
}
