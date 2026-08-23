package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Os fast paths de OP_RETURN e OP_CALL_STATIC (issue #66, item 3) so podem
// existir se o resultado for byte a byte o mesmo dos caminhos lentos: retorno
// com defer, closure com upvalue aberto no retorno, parametro composto por
// valor (CoW) e recursao continuam corretos.
func TestCallProtocolFastPathsKeepSemantics(t *testing.T) {
	src := `
func fib(n: int) -> int
    if n <= 1 then
        return n
    end
    return fib(n - 1) + fib(n - 2)
end
func withDefer(n: int) -> int
    let acc: int[] = []
    defer append(acc, 1)
    return n + 1
end
func makeAdder(base: int) -> func(int) -> int
    return func(x: int) -> int
        return base + x
    end
end
func sumArr(a: int[]) -> int
    let s: int = 0
    let i: int = 0
    while i < length(a) do
        s = s + a[i]
        i = i + 1
    end
    return s
end
let arr: int[] = [1, 2, 3]
let total: int = sumArr(arr)
append(arr, 4)
let add5: func(int) -> int = makeAdder(5)
test_report([fib(20), fib(1), fib(0), withDefer(41), add5(10), total, length(arr)])
`
	got := semArray(t, captureVMSource(t, src))
	want := []int64{6765, 1, 0, 42, 15, 6, 4}
	if len(got) != len(want) {
		t.Fatalf("got %d celulas, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Type != value.VAL_INT || got[i].Int() != w {
			t.Fatalf("celula %d: got %s, want %d", i, got[i].String(), w)
		}
	}
}

// Parametro composto por valor nao passa pelo fast path (ParamsUntracked e
// false): o frame retem o argumento e a mutacao dentro da funcao clona (CoW),
// deixando o array do chamador intacto.
func TestCallWithCompositeParamStillRetains(t *testing.T) {
	src := `
func touch(a: int[]) -> int
    a[0] = 99
    return a[0]
end
let base: int[] = [1, 2]
let r: int = touch(base)
test_report([r, base[0]])
`
	got := semArray(t, captureVMSource(t, src))
	if got[0].Int() != 99 || got[1].Int() != 1 {
		t.Fatalf("got %s/%s, want 99/1 (CoW: base nao muda)", got[0].String(), got[1].String())
	}
}

// OP_CLOSURE (e os sites que vinculam um function constant ao ambiente) copiam
// o ObjFunction campo a campo; se esquecerem ParamsUntracked o fast path de
// OP_CALL_STATIC nunca e tomado — silenciosamente, sem erro funcional. Este
// teste pergunta ao proprio valor de closure.
func TestClosureKeepsParamsUntracked(t *testing.T) {
	cases := []struct {
		name, source string
		want         bool
	}{
		{"int param", "func f(n: int) -> int\n    return n\nend\ntest_report(f)\n", true},
		{"array param", "func f(a: int[]) -> int\n    return 0\nend\ntest_report(f)\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captureVMSource(t, tc.source)
			closure, ok := got.Obj.(*value.ObjClosure)
			if got.Type != value.VAL_FUNCTION || !ok {
				t.Fatalf("test_report recebeu %s, esperava closure", got.String())
			}
			if closure.Function.ParamsUntracked != tc.want {
				t.Fatalf("closure.Function.ParamsUntracked = %v, want %v (copia de ObjFunction perdeu o flag?)", closure.Function.ParamsUntracked, tc.want)
			}
		})
	}
}

// As mensagens de erro do protocolo de chamada nao mudam: overflow de frames
// (growForCall continua o unico dono da mensagem) e aridade errada pela
// fronteira dinamica (callValue, caminho lento).
func TestCallProtocolErrorsUnchanged(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"deep recursion", "func down(n: int) -> int\n    return down(n + 1)\nend\ndown(0)\n", "stack overflow: call depth exceeds"},
		{"arity via any", "func f(a: int, b: int) -> int\n    return a + b\nend\nlet g: any = f\ng(1)\n", "expected 2 arguments but got 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretVMSource(t, New(), tc.source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want contendo %q", err, tc.want)
			}
		})
	}
}
