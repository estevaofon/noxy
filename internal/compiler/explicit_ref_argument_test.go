package compiler

// R5 (spec 2026-08-24-explicit-ref §3): um ref nunca e criado
// implicitamente — `f(x)` para parametro `ref T` e erro com hint `ref x`.
// R1: `ref` sobre algo que ja e ref e erro; `ref ref T` nao e um tipo.

import "testing"

const refParamPrelude = `func inc(v: ref int) -> void
    *v = *v + 1
end
`

func TestRefParameterPlainArgumentIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`let x: int = 1
inc(x)`, "argument 1 to 'inc': expected ref int, got int", "hint: use 'ref x'")
}

func TestRefParameterPlainFieldArgumentIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`struct P
    n: int
end
let p: P = P(1)
inc(p.n)`, "argument 1 to 'inc': expected ref int, got int", "hint: use 'ref p.n'")
}

func TestRefParameterLiteralArgumentIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`inc(41)`, "argument 1 to 'inc': expected ref int, got int", "hint: bind the value to a variable and pass 'ref <name>'")
}

func TestRefParameterAcceptsRefForms(t *testing.T) {
	requireCompiles(t, refParamPrelude+`struct Node
    valor: int
    next: ref Node
end
func avanca(n: ref Node) -> void
    return
end
func acha() -> ref int
    let z: int = 0
    return ref z
end
let x: int = 1
let r: ref int = ref x
let nd: Node = Node(1, null)
inc(ref x)
inc(r)
inc(null)
inc(acha())
avanca(nd.next)
avanca(ref nd)`)
}

func TestRefParameterViaTypedFuncValueRequiresRef(t *testing.T) {
	requireCompileError(t, refParamPrelude+`let f: func(ref int) -> void = inc
let x: int = 1
f(x)`, "expected ref int, got int", "hint: use 'ref x'")
	requireCompiles(t, refParamPrelude+`let f: func(ref int) -> void = inc
let x: int = 1
f(ref x)`)
}

func TestStructConstructorRefFieldRequiresRef(t *testing.T) {
	requireCompileError(t, `struct Obs
    alvo: ref int
end
let t: int = 20
let o: Obs = Obs(t)`, "argument 1 to 'Obs': expected ref int, got int", "hint: use 'ref t'")
	requireCompiles(t, `struct Obs
    alvo: ref int
end
let t: int = 20
let o: Obs = Obs(ref t)
let o2: Obs = Obs(null)`)
}

func TestGenericRefParameterRequiresRef(t *testing.T) {
	requireCompileError(t, `func bump<T>(v: ref T, by: T) -> void
    return
end
let x: int = 1
bump(x, 1)`, "expected ref int, got int", "hint: use 'ref x'")
	requireCompiles(t, `func bump<T>(v: ref T, by: T) -> void
    return
end
let x: int = 1
bump(ref x, 1)`)
}

func TestAnyArgumentForRefParameterIsRuntimeChecked(t *testing.T) {
	// Tipo any: o modo nao e provado em compilacao — validateParameterModes
	// decide em runtime. Compila.
	requireCompiles(t, refParamPrelude+`let a: any = 1
inc(a)`)
}

func TestRefOfRefIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`let x: int = 1
let r: ref int = ref x
inc(ref r)`, "'r' is already a reference", "hint: pass 'r' directly, without 'ref'")
	requireCompileError(t, `struct Node
    next: ref Node
end
func f(n: ref Node) -> void
    return
end
let nd: Node = Node(null)
f(ref nd.next)`, "'nd.next' is already a reference", "hint: pass 'nd.next' directly")
	requireCompileError(t, `func acha() -> ref int
    let z: int = 0
    return ref z
end
let q: ref int = ref acha()`, "'acha()' is already a reference")
	requireCompileError(t, refParamPrelude+`inc(ref null)`, "'null' is not addressable", "hint: pass null directly, without 'ref'")
}

func TestRefOfRefUpvalueAndGlobalIsError(t *testing.T) {
	requireCompileError(t, refParamPrelude+`let g: int = 1
let rg: ref int = ref g
func usa() -> void
    inc(ref rg)
end`, "'rg' is already a reference")
	requireCompileError(t, refParamPrelude+`func outer() -> void
    let x: int = 1
    let r: ref int = ref x
    let f: func() -> void = func() -> void
        inc(ref r)
    end
    f()
end`, "'r' is already a reference")
}

// Achado do review de Task 6 (2026-08-25): exprDisplay precisa preservar
// aspas em torno de um indice string literal — sem isso o hint sugeria
// `ref m[k]`, que nao compila de volta (StringLiteral.String() devolve o
// texto cru, sem aspas).
func TestRefArgumentHintQuotesStringIndex(t *testing.T) {
	requireCompileError(t, refParamPrelude+`let m: map[string, int] = {"k": 1}
inc(m["k"])`, "argument 1 to 'inc': expected ref int, got int", `hint: use 'ref m["k"]'`)
}

func TestBuiltinRefArgumentRequiresRef(t *testing.T) {
	requireCompileError(t, `let xs: int[] = []
append(xs, 1)`, "argument 1 to 'append': expected ref T[], got int[]", "hint: use 'ref xs'")
	requireCompileError(t, `let xs: int[] = [1]
let v: int = pop(xs)`, "argument 1 to 'pop': expected ref T[], got int[]", "hint: use 'ref xs'")
	requireCompileError(t, `let m: map[string, int] = {"a": 1}
delete(m, "a")`, "argument 1 to 'delete': expected ref map, got map[string, int]", "hint: use 'ref m'")
	requireCompileError(t, `let alvo: int = 0
let ok: bool = json_loads("1", alvo)`, "argument 2 to 'json_loads': expected ref T, got int", "hint: use 'ref alvo'")
	requireCompileError(t, `let xs: (ref int)[] = []
let n: int = 1
append(ref xs, n)`, "argument 2 to 'append': expected ref int, got int", "hint: use 'ref n'")
}

func TestBuiltinRefArgumentAcceptsRefForms(t *testing.T) {
	requireCompiles(t, `struct Bag
    itens: int[]
end
let xs: int[] = []
let m: map[string, int] = {"a": 1}
let alvo: int = 0
let b: Bag = Bag([])
append(ref xs, 1)
append(ref b.itens, 2)
let v: int = pop(ref xs)
delete(ref m, "a")
let ok: bool = json_loads("1", ref alvo)
let rxs: (ref int)[] = []
let n: int = 1
let rn: ref int = ref n
append(ref rxs, ref n)
append(ref rxs, rn)
append(ref rxs, null)
func enche(p: ref int[]) -> void
    append(p, 9)
end`)
}
