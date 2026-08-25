package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// A spec (§1.3/§8) lista `>`, `<`, `>=`, `<=` como operadores de comparacao
// sem restringi-los a numeros, e define comparacao de strings como byte-exata
// (sem normalizacao Unicode, como Python). Ate aqui o executor so aceitava
// numeros em OP_GREATER/OP_LESS, entao `"a" < "b"` compilava e estourava em
// RUNTIME com "operands must be numbers". Estes testes fixam a semantica
// correta: ordenacao lexicografica byte a byte — que, para UTF-8 valido
// (invariante de toda string Noxy), coincide com a ordem por code point.
func TestStringOrderingIsLexicographic(t *testing.T) {
	got := captureVMSource(t, `
test_report([
    "abc" < "abd",
    "b" > "a",
    "a" <= "a",
    "abc" >= "abc",
    "ab" < "b",
    "Z" < "a",
    "e" < "é",
    "a" > "b",
    "b" <= "a"
])`)
	arr, ok := got.Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("esperava array de bools, veio %v", got)
	}
	want := []bool{true, true, true, true, true, true, true, false, false}
	if len(arr.Elements) != len(want) {
		t.Fatalf("esperava %d resultados, vieram %d", len(want), len(arr.Elements))
	}
	for i, expected := range want {
		if arr.Elements[i].Bool() != expected {
			t.Errorf("comparacao %d: esperava %v, veio %v", i, expected, arr.Elements[i].Bool())
		}
	}
}

// Um operando ref de `<`/`>` e lido explicitamente com `*` (R2, spec
// 2026-08-24-explicit-ref); com strings suportadas no executor, `*r` sobre
// um `ref string` ordena pelo valor apontado.
func TestStringOrderingReadsDerefRefOperands(t *testing.T) {
	got := captureVMSource(t, `
let s: string = "a"
let r: ref string = ref s
test_report(*r < "b")`)
	if !got.Bool() {
		t.Fatal("ref string < string deveria comparar o valor apontado")
	}
}

// A fronteira dinamica tambem ordena: operandos `any` que carregam strings
// em runtime usam o mesmo caminho generico de OP_LESS.
func TestStringOrderingThroughAnyBoundary(t *testing.T) {
	got := captureVMSource(t, `
let a: any = "abc"
test_report(a < "abd")`)
	if !got.Bool() {
		t.Fatal("any carregando string deveria ordenar lexicograficamente")
	}
}

// Misturar string com numero continua erro de runtime — agora com mensagem
// que reconhece os dois domínios validos.
func TestStringOrderingMixedOperandsStillError(t *testing.T) {
	err := interpretVMSource(t, New(), `
let a: any = "a"
let r: bool = a < 1
print(r)`)
	if err == nil || !strings.Contains(err.Error(), "operands must be numbers or strings") {
		t.Fatalf("esperava erro de operandos mistos, veio %v", err)
	}
}

// bytes seguem FORA da ordenacao (so strings ganharam suporte): a ponte
// explicita e to_str, como em toda a stdlib.
func TestBytesOrderingRemainsUnsupported(t *testing.T) {
	err := interpretVMSource(t, New(), `
let a: any = b"a"
let b: any = b"b"
let r: bool = a < b
print(r)`)
	if err == nil || !strings.Contains(err.Error(), "operands must be numbers or strings") {
		t.Fatalf("esperava erro para ordenacao de bytes, veio %v", err)
	}
}
