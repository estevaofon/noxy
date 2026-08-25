package compiler

// Atribuicao NAO faz auto-deref (spec §2.3, tabela de Type-Based
// Assignment): `x = r` com x: T e r: ref T e a terceira combinacao invalida,
// irma das duas ja documentadas (`r = 50`, que tem hint proprio via
// referenceAssignmentTypeError, e `*r = <nao-T>`). Ate aqui ela caia no
// mismatch generico "expected int, got ref int" sem apontar o conserto.
// Estes testes fixam o hint novo — "use '*r' to read the referenced value" —
// em cada shape de alvo de atribuicao, e que ele NAO aparece quando o deref
// nao consertaria o programa.

import (
	"strings"
	"testing"
)

const derefReadHintText = "to read the referenced value"

func requireDerefReadHint(t *testing.T, src string, wantHint string) {
	t.Helper()
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatal("atribuicao de ref a alvo de valor deveria falhar")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("erro deveria continuar sendo type mismatch: %v", err)
	}
	if !strings.Contains(err.Error(), wantHint) {
		t.Fatalf("erro sem o hint %q: %v", wantHint, err)
	}
}

func TestLocalAssignmentFromRefSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `func f()
    let x: int = 10
    let r: ref int = ref x
    let a: int = 0
    a = r
end`, "hint: use '*r' "+derefReadHintText)
}

func TestGlobalAssignmentFromRefSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `let x: int = 10
let r: ref int = ref x
let g: int = 0
g = r`, "hint: use '*r' "+derefReadHintText)
}

func TestUpvalueAssignmentFromRefSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `func outer()
    let n: int = 0
    let base: int = 9
    let r: ref int = ref base
    let f: func() -> int = func() -> int
        n = r
        return n
    end
    f()
end`, "hint: use '*r' "+derefReadHintText)
}

func TestArrayElementAssignmentFromRefSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `let base: int = 10
let r: ref int = ref base
let arr: int[] = [1]
arr[0] = r`, "hint: use '*r' "+derefReadHintText)
}

func TestMapEntryAssignmentFromRefSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `let base: int = 10
let r: ref int = ref base
let m: map[string, int] = {"k": 1}
m["k"] = r`, "hint: use '*r' "+derefReadHintText)
}

func TestFieldAssignmentFromRefSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `struct P
    x: int
end
let base: int = 10
let r: ref int = ref base
let p: P = P(0)
p.x = r`, "hint: use '*r' "+derefReadHintText)
}

// RHS que nao e identificador simples ainda ganha hint, na forma generica.
func TestNonIdentifierRefRhsGetsGenericDerefHint(t *testing.T) {
	requireDerefReadHint(t, `func cria() -> ref int
    let n: int = 42
    return ref n
end
let a: int = 0
a = cria()`, "hint: use '*' "+derefReadHintText)
}

// Quando o deref NAO consertaria (tipos incompativeis mesmo apos deref),
// o mismatch continua sem hint — sugerir '*' seria orientacao errada.
func TestMismatchedRefRhsDoesNotSuggestDeref(t *testing.T) {
	_, _, err := New().Compile(parse(`func f()
    let x: int = 10
    let r: ref int = ref x
    let s: string = ""
    s = r
end`))
	if err == nil {
		t.Fatal("atribuir ref int a string deveria falhar")
	}
	if strings.Contains(err.Error(), derefReadHintText) {
		t.Fatalf("hint de deref nao deveria aparecer quando deref nao conserta: %v", err)
	}
}

// R2: `let x: T = r` deixa de ler implicitamente — mesma mensagem e hint
// da atribuicao.
func TestLetDeclarationFromRefSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `let x: int = 10
let r: ref int = ref x
let m: int = r`, "hint: use '*r' "+derefReadHintText)
}

// R2: o RHS de `*r = s` tambem nao le.
func TestDerefAssignmentFromRefRhsSuggestsDeref(t *testing.T) {
	requireDerefReadHint(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
let s: ref int = ref z
*r = s`, "hint: use '*s' "+derefReadHintText)
}
