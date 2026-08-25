package compiler

import (
	"strings"
	"testing"
)

// Spec §6.3: uma função genérica passada como ARGUMENTO de outra função
// genérica é instanciada pelo tipo do parâmetro (unificação bidirecional).
// Os casos aqui usam parâmetros compostos — array, map, ref, chan, func e
// instância genérica — que o perfil mostrou sem teste em unifyBidirectional.
func TestGenericFunctionArgumentUnifiesThroughCompositeParameterTypes(t *testing.T) {
	prelude := `
struct Caixa<T>
    v: T
end
func primeiro<U>(xs: U[]) -> U
    return xs[0]
end
func chaves<K, V>(m: map[K, V]) -> K[]
    let ks: K[] = []
    for k in m do
        append(ref ks, k)
    end
    return ks
end
func le<T>(r: ref T) -> T
    return *r
end
func recebe<T>(c: chan T) -> T
    return chan_recv(c)
end
func abre<T>(c: Caixa<T>) -> T
    return c.v
end
func usa_arr<T>(f: func(T[]) -> T, xs: T[]) -> T
    return f(xs)
end
func usa_map<K, V>(f: func(map[K, V]) -> K[], m: map[K, V]) -> K[]
    return f(m)
end
func usa_ref<T>(f: func(ref T) -> T, r: ref T) -> T
    return f(r)
end
func usa_chan<T>(f: func(chan T) -> T, c: chan T) -> T
    return f(c)
end
func usa_caixa<T>(f: func(Caixa<T>) -> T, c: Caixa<T>) -> T
    return f(c)
end
`
	good := []struct{ name, source string }{
		{"array parameter", "let v: int = usa_arr(primeiro, [1, 2])\n"},
		{"map parameter", "let m: map[string, int] = {\"a\": 1}\nlet ks: string[] = usa_map(chaves, m)\n"},
		{"ref parameter", "let x: int = 3\nlet v: int = usa_ref(le, ref x)\n"},
		{"chan parameter", "let c: chan int = make_chan(1)\nchan_send(c, 4)\nlet v: int = usa_chan(recebe, c)\n"},
		{"generic instance parameter", "let cx: Caixa<string> = Caixa(\"s\")\nlet v: string = usa_caixa(abre, cx)\n"},
	}
	for _, tc := range good {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := New().Compile(parse(prelude + tc.source)); err != nil {
				t.Fatalf("deveria compilar: %v", err)
			}
		})
	}
	bad := []struct{ name, source, want string }{
		{"array parameter given a scalar function", "func dobra<T>(x: T) -> T\n    return x\nend\nlet v: int = usa_arr(dobra, [1])\n", "esperava"},
		{"ref parameter given an array function", "let x: int = 3\nlet v: int = usa_ref(primeiro, ref x)\n", "esperava"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := New().Compile(parse(prelude + tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}
