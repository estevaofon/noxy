package compiler

// Igualdade estrita de ref (v0.7.1): em `==`/`!=` um operando `ref` NUNCA e
// dereferenciado implicitamente. Dois refs comparam identidade de slot (ja
// vigente desde o fix de 0.7.x), ref vs null pergunta se o REF e nulo, e o
// caso misto ref vs valor — que antes fazia auto-deref silencioso — vira
// erro de compilacao com hint apontando o deref explicito. Fecha a
// assimetria com o `=`, que ja recusa conversao implicita de ref nas duas
// direcoes.

import (
	"strings"
	"testing"
)

const compareHintText = "to compare the referenced value"

func requireStrictCompareError(t *testing.T, src, wantHint string) {
	t.Helper()
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatal("comparacao mista ref vs valor deveria falhar na compilacao")
	}
	if !strings.Contains(err.Error(), "never implicitly dereferenced") {
		t.Fatalf("erro sem a explicacao da regra: %v", err)
	}
	if !strings.Contains(err.Error(), wantHint) {
		t.Fatalf("erro sem o hint %q: %v", wantHint, err)
	}
}

func requireStrictCompareOK(t *testing.T, src string) {
	t.Helper()
	if _, _, err := New().Compile(parse(src)); err != nil {
		t.Fatalf("deveria compilar: %v", err)
	}
}

func TestMixedRefEqualityIsCompileError(t *testing.T) {
	requireStrictCompareError(t, `let x: int = 1
let ra: ref int = ref x
let r: bool = ra == 1
print(r)`, "hint: use '*ra' "+compareHintText)
}

func TestMixedRefEqualityErrorsWithRefOnRight(t *testing.T) {
	requireStrictCompareError(t, `let x: int = 1
let ra: ref int = ref x
let r: bool = 1 == ra
print(r)`, "hint: use '*ra' "+compareHintText)
}

func TestMixedRefInequalityIsCompileError(t *testing.T) {
	requireStrictCompareError(t, `let x: int = 1
let ra: ref int = ref x
let r: bool = ra != 6
print(r)`, "hint: use '*ra' "+compareHintText)
}

func TestMixedRefFieldEqualityGetsGenericHint(t *testing.T) {
	requireStrictCompareError(t, `struct Obs
    alvo: ref int
end
let t: int = 20
let o: Obs = Obs(ref t)
let r: bool = o.alvo == 20
print(r)`, "hint: use '*' "+compareHintText)
}

func TestRefAgainstNullStillCompiles(t *testing.T) {
	requireStrictCompareOK(t, `let x: int = 1
let ra: ref int = ref x
print(ra == null)
print(null != ra)`)
}

func TestRefAgainstRefStillCompiles(t *testing.T) {
	requireStrictCompareOK(t, `let x: int = 1
let y: int = 1
let ra: ref int = ref x
let rb: ref int = ref y
print(ra == rb)`)
}

func TestExplicitDerefComparisonCompiles(t *testing.T) {
	requireStrictCompareOK(t, `let x: int = 1
let ra: ref int = ref x
print(*ra == 1)
print(*ra != 6)`)
}

// `any` e fronteira dinamica: o lado nao-ref pode conter um ref em runtime
// (comparacao de identidade legitima), entao a checagem estatica nao rejeita.
func TestRefAgainstAnyStillCompiles(t *testing.T) {
	requireStrictCompareOK(t, `let x: int = 1
let ra: ref int = ref x
let a: any = ra
print(a == ra)`)
}
