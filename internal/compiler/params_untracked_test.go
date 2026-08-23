package compiler

import "testing"

// ParamsUntracked (issue #66, item 3) e o que autoriza o fast path de
// OP_CALL_STATIC a pular o laco ownSlot: so pode ser true quando NENHUM
// parametro pode carregar contador RC. Conservador para tudo que nao e
// primitivo escalar/string/bytes sem ref.
func TestParamsUntrackedFlag(t *testing.T) {
	cases := []struct {
		name, source string
		want         bool
	}{
		{"int", "func f(n: int) -> int\n    return n\nend\n", true},
		{"scalars and string", "func f(s: string, b: bytes, x: float, ok: bool) -> int\n    return 0\nend\n", true},
		{"no params", "func f() -> int\n    return 1\nend\n", true},
		{"array", "func f(a: int[]) -> int\n    return 0\nend\n", false},
		{"any", "func f(x: any) -> int\n    return 0\nend\n", false},
		{"ref", "func f(r: ref int) -> int\n    return 0\nend\n", false},
		{"struct", "struct P\n    x: int\nend\nfunc f(p: P) -> int\n    return 0\nend\n", false},
		{"func type", "func f(g: func(int) -> int) -> int\n    return 0\nend\n", false},
		{"mixed", "func f(n: int, a: int[]) -> int\n    return 0\nend\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fn := compiledFunction(t, tc.source, "f")
			if fn.ParamsUntracked != tc.want {
				t.Fatalf("ParamsUntracked = %v, want %v", fn.ParamsUntracked, tc.want)
			}
		})
	}
}
