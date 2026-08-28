package vm

import "testing"

// Spec §2.4 (issue #105): o espelho de runtime de acceptsNull — um slot `T?`
// aceita null e T vindos por `any`.

func TestNullableRuntimeAcceptsNullAndValue(t *testing.T) {
	src := `struct P
    x: int
end
let a: any = null
let b: P? = a
let c: any = P(3)
let d: P? = c
let xs: int?[] = []
append(ref xs, null)
append(ref xs, 7)
test_report(to_str(b == null) + "|" + to_str(d != null) + "|" + to_str(length(xs)))
`
	got := captureVMSource(t, src)
	if got.String() != "true|true|2" {
		t.Fatalf("got %s", got.String())
	}
}

func TestNullableRuntimeTypeNameCarriesQuestionMark(t *testing.T) {
	// Array e validado na fronteira any -> tipado (struct sem composto e
	// primitivo nao sao, hoje nem com `?`): string em int[]? erra, null entra.
	src := `let a: any = "s"
let b: int[]? = a
`
	err := interpretOrCompileErr(t, New(), src)
	if err == nil {
		t.Fatalf("string into int[]? must fail at runtime")
	}
	ok := captureVMSource(t, `let a: any = null
let b: int[]? = a
let c: any = [1, 2]
let d: int[]? = c
test_report(to_str(b == null) + "|" + to_str(d != null))
`)
	if ok.String() != "true|true" {
		t.Fatalf("null and array into int[]?: %s", ok.String())
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
