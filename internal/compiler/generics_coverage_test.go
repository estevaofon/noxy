package compiler

import (
	"strings"
	"testing"
)

// Genéricos (spec §6): os walkers de substituição/nomes-livres precisam
// aceitar TODA construção da linguagem dentro de um corpo genérico, a
// unificação precisa descer em map/ref/chan/func/instância genérica, e os
// diagnósticos de aridade, conflito e inferência têm texto próprio. O perfil
// mostrou esses ramos de generics*.go sem teste.

// Corpo genérico que usa cada statement/expressão: while, if/else, for, when,
// defer, break/continue, return, assign, struct aninhado, function literal,
// array/map literal, zeros, prefix, member access, index.
const richGenericBody = `
struct Par
    a: int
    b: int
end
func processa<T>(x: T, n: int) -> T[]
    let out: T[] = []
    let i: int = 0
    while i < n do
        i = i + 1
        if i % 2 == 0 then
            continue
        else
            append(out, x)
        end
        if i > 5 then
            break
        end
    end
    for item in out do
        let copia: T = item
    end
    let zs: int[] = zeros(n)
    let neg: int = -n
    let par: Par = Par(1, 2)
    let soma: int = par.a + zs[0] + neg
    let m: map[string, int] = {"k": soma}
    let lit: func(int) -> int = func(v: int) -> int
        return v + m["k"]
    end
    let c: chan int = make_chan(1)
    when
        case chan_recv(c) then
            print(lit(1))
        default
            print(0)
    end
    defer print(n)
    return out
end
`

func TestGenericBodyMayUseEveryConstructAndStillInstantiates(t *testing.T) {
	for _, call := range []string{
		"let r: int[] = processa(7, 3)\n",
		"let r: string[] = processa(\"s\", 3)\n",
	} {
		if _, _, err := New().Compile(parse(richGenericBody + call)); err != nil {
			t.Fatalf("corpo genérico rico deveria instanciar para %q: %v", call, err)
		}
	}
}

func TestGenericUnificationDescendsIntoCompositeTypes(t *testing.T) {
	prelude := `
struct Caixa<T>
    v: T
end
func chave<K, V>(m: map[K, V]) -> K[]
    let ks: K[] = []
    for k in m do
        append(ks, k)
    end
    return ks
end
func le<T>(r: ref T) -> T
    return *r
end
func canal<T>(c: chan T) -> T
    return chan_recv(c)
end
func aplica<T, U>(f: func(T) -> U, x: T) -> U
    return f(x)
end
func desembrulha<T>(c: Caixa<T>) -> T
    return c.v
end
func dobra(n: int) -> int
    return n * 2
end
`
	good := prelude + `
let m: map[string, int] = {"a": 1}
let ks: string[] = chave(m)
let x: int = 5
let lido: int = le(ref x)
let c: chan float = make_chan(1)
chan_send(c, 1.5)
let recebido: float = canal(c)
let dobrado: int = aplica(dobra, 21)
let caixa: Caixa<string> = Caixa("s")
let dentro: string = desembrulha(caixa)
`
	if _, _, err := New().Compile(parse(good)); err != nil {
		t.Fatalf("chamadas genéricas com map/ref/chan/func/instância deveriam compilar: %v", err)
	}
	// Anotações compostas que carregam uma instância genérica também resolvem.
	annotated := prelude + `
let a: Caixa<int> = Caixa(1)
let mm: map[string, Caixa<int>] = {"a": a}
let rr: ref Caixa<int> = ref a
let cc: chan Caixa<int> = make_chan(1)
let ff: func(Caixa<int>) -> int = desembrulha
let arr: Caixa<int>[] = [a]
`
	if _, _, err := New().Compile(parse(annotated)); err != nil {
		t.Fatalf("anotações compostas com instância genérica deveriam compilar: %v", err)
	}
	bad := []struct{ name, source, want string }{
		{"map key mismatch", "func f<K>(m: map[K, int], k: K) -> int\n    return m[k]\nend\nlet m: map[string, int] = {\"a\": 1}\nlet v: int = f(m, 2)\n", "inferido como"},
		{"function param mismatch", "let s: string = aplica(dobra, \"x\")\n", "inferido como"},
		{"wrong arity on generic function", "let r: int = le(ref 1, 2)\n", "expects 1 arguments, got 2"},
		{"wrong arity on generic constructor", "let c: Caixa<int> = Caixa(1, 2)\n", "expects 1 arguments, got 2"},
		{"conflicting inference", "func par<T>(a: T, b: T) -> T\n    return a\nend\nlet v: int = par(1, \"s\")\n", "inferido como int (argumento 1) e string (argumento 2)"},
		{"uninferable struct type argument", "let c: any = Caixa(null)\n", "inferir"},
		{"type argument arity", "let c: Caixa<int, string> = Caixa(1)\n", "Caixa"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := New().Compile(parse(prelude + tc.source))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("source %q: error=%v, want %q", tc.source, err, tc.want)
			}
		})
	}
}

// Dois tipos de função cujos parâmetros são instâncias genéricas iguais são o
// mesmo tipo exato (typesEquivalent desce em GenericType).
func TestFunctionTypesWithGenericInstanceParametersAreEquivalent(t *testing.T) {
	source := `
struct Caixa<T>
    v: T
end
func g(c: Caixa<int>) -> int
    return c.v
end
let f: func(Caixa<int>) -> int = g
let c: Caixa<int> = Caixa(3)
let v: int = f(c)
`
	if _, _, err := New().Compile(parse(source)); err != nil {
		t.Fatalf("func(Caixa<int>) -> int deveria aceitar g: %v", err)
	}
	wrong := `
struct Caixa<T>
    v: T
end
func g(c: Caixa<string>) -> int
    return 1
end
let f: func(Caixa<int>) -> int = g
`
	if _, _, err := New().Compile(parse(wrong)); err == nil {
		t.Fatal("func(Caixa<int>) -> int não deveria aceitar uma função sobre Caixa<string>")
	}
}
