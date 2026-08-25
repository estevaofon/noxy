package compiler

// R2 (spec 2026-08-24-explicit-ref §3): um `ref T` nunca e lido
// implicitamente. Cada teste fixa o erro E o hint em uma posicao.
//
// requireCompileError e requireCompiles ja existem no pacote (definidos em
// let_inference_test.go e let_redeclaration_test.go, respectivamente) e sao
// reutilizados aqui.

import (
	"testing"
)

func TestLetAnnotatedFromRefIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let m: int = r`, "type mismatch in 'm' declaration: expected int, got ref int", "hint: use '*r' to read the referenced value")
}

func TestLetAnnotatedFromDerefCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let r: ref int = ref x
let m: int = *r`)
}

func TestLetInferredFromRefKeepsRefType(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let v = r
let n: int = v`, "expected int, got ref int", "hint: use '*v'")
}

func TestDerefAssignmentFromRefRHSIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
let s: ref int = ref z
*r = s`, "type mismatch in assignment: expected int, got ref int", "hint: use '*s' to read the referenced value")
}

func TestDerefAssignmentFromRefPrefixIsRebindOrValueHint(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
*r = ref z`, "cannot assign ref int to int through '*r'", "hint: use 'r = ref z' to rebind the reference, or '*r = z' to write the value")
}

func TestDerefAssignmentFromDerefCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let z: int = 99
let r: ref int = ref x
let s: ref int = ref z
*r = *s`)
}

func TestInfixOperandRefIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let y: int = r + 1`, "operand of '+' cannot be ref int: a ref is never read implicitly", "hint: use '*r' to read the referenced value")
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let y: int = 1 + r`, "operand of '+' cannot be ref int", "hint: use '*r'")
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let b: bool = r < 5`, "operand of '<' cannot be ref int", "hint: use '*r'")
}

func TestUnaryOperandRefIsError(t *testing.T) {
	requireCompileError(t, `let x: int = 10
let r: ref int = ref x
let y: int = -r`, "operand of '-' cannot be ref int", "hint: use '*r'")
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
let g: bool = !rb`, "operand of '!' cannot be ref bool", "hint: use '*rb'")
}

func TestLogicalOperandRefIsError(t *testing.T) {
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
let g: bool = rb && true`, "operand of '&&' cannot be ref bool", "hint: use '*rb'")
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
let g: bool = false || rb`, "operand of '||' cannot be ref bool", "hint: use '*rb'")
}

func TestConditionRefIsError(t *testing.T) {
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
if rb then
    print(1)
end`, "condition cannot be ref bool: a ref is never read implicitly", "hint: use '*rb'")
	requireCompileError(t, `let f: bool = true
let rb: ref bool = ref f
while rb do
    f = false
end`, "condition cannot be ref bool", "hint: use '*rb'")
}

func TestEqualityBetweenRefsStillCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let r: ref int = ref x
let r2: ref int = ref x
let same: bool = r == r2
let isnull: bool = r == null
let val: bool = *r == 10`)
}

func TestExplicitReadsInOperatorsCompile(t *testing.T) {
	requireCompiles(t, `let x: int = 10
let f: bool = true
let r: ref int = ref x
let rb: ref bool = ref f
let a: int = *r + 1
let b: int = -*r
let c: bool = !*rb && *rb
if *rb then
    x = 1
end
while *rb do
    f = false
end`)
}

func TestIndexOperandRefIsError(t *testing.T) {
	base := `let i: int = 0
let ri: ref int = ref i
let xs: int[] = [1, 2]
`
	requireCompileError(t, base+`let v: int = xs[ri]`, "index cannot be ref int: a ref is never read implicitly", "hint: use '*ri'")
	requireCompileError(t, base+`xs[ri] = 5`, "index cannot be ref int", "hint: use '*ri'")
	requireCompileError(t, base+`func f(t: ref int)
    *t = 1
end
f(ref xs[ri])`, "index cannot be ref int", "hint: use '*ri'")
	requireCompileError(t, `func g()
    let i: int = 0
    let ri: ref int = ref i
    let xs: int[] = [1, 2]
    let v: int = xs[ri]
end`, "index cannot be ref int", "hint: use '*ri'")
	requireCompileError(t, `func h()
    let i: int = 0
    let ri: ref int = ref i
    let xs: int[] = [1, 2]
    xs[ri] = 5
end`, "index cannot be ref int", "hint: use '*ri'")
}

func TestIndexOperandDerefCompiles(t *testing.T) {
	requireCompiles(t, `func g()
    let i: int = 0
    let ri: ref int = ref i
    let xs: int[] = [1, 2]
    let v: int = xs[*ri]
    xs[*ri] = 5
end
g()`)
}

func TestForOverRefCollectionIsError(t *testing.T) {
	requireCompileError(t, `let xs: int[] = [1, 2]
let r: ref int[] = ref xs
for x in r do
    print(x)
end`, "cannot iterate over ref int[]: a ref is never read implicitly", "hint: use 'for x in *r'")
}

func TestForOverDerefCollectionTypesLoopVariable(t *testing.T) {
	requireCompileError(t, `let xs: int[] = [1, 2]
let r: ref int[] = ref xs
for x in *r do
    let s: string = x
end`, "type mismatch in 's' declaration: expected string, got int")
}

func TestReturnRefForValueTypeIsError(t *testing.T) {
	requireCompileError(t, `func f(r: ref int) -> int
    return r
end`, "return type mismatch in 'f': expected int, got ref int", "hint: use '*r'")
}

func TestReturnDerefCompiles(t *testing.T) {
	requireCompiles(t, `func f(r: ref int) -> int
    return *r
end
func g(r: ref int) -> ref int
    return r
end`)
}

func TestValueParameterRefArgumentIsError(t *testing.T) {
	requireCompileError(t, `func dobro(n: int) -> int
    return n * 2
end
let x: int = 2
let r: ref int = ref x
let y: int = dobro(r)`, "argument 1 to 'dobro': expected int, got ref int", "hint: use '*r'")
	requireCompileError(t, `func dobro(n: int) -> int
    return n * 2
end
let x: int = 2
let y: int = dobro(ref x)`, "argument 1 to 'dobro': expected int, got ref int", "hint: use '*'")
}

func TestValueParameterDerefArgumentCompiles(t *testing.T) {
	requireCompiles(t, `func dobro(n: int) -> int
    return n * 2
end
let x: int = 2
let r: ref int = ref x
let y: int = dobro(*r)`)
}

// Parametro any e nativo sem assinatura recebem o ref como valor (R2, ultimo
// paragrafo): compila, e print/to_str mostram a referencia.
func TestAnyAndUnsignedNativeAcceptRefAsValue(t *testing.T) {
	requireCompiles(t, `func guarda(v: any) -> any
    return v
end
let x: int = 2
let r: ref int = ref x
let kept: any = guarda(r)
print(r)
let s: string = to_str(r)
let f: string = f"{r}"`)
}

func TestAppendValueArgumentRefIsError(t *testing.T) {
	requireCompileError(t, `let xs: int[] = []
let x: int = 1
let r: ref int = ref x
append(ref xs, r)`, "argument 2 to 'append': expected int, got ref int", "hint: use '*r'")
}

// Task 10a (issue #82): length/keys/slice/contains/has_key sao nativas sem
// assinatura (DefineNative/DefineContextualNative, sem NativeSignature) —
// nao passam por compileBuiltinRefArgument nem tem tipo de parametro
// estatico, entao a rejeicao de `ref T` estatico (R2) precisa de checagem
// dedicada em vez de vir de areStrictTypesCompatible/compileRefArgument.
func TestValueNativesRejectRefArgument(t *testing.T) {
	prelude := "let xs: int[] = [1, 2]\nlet m: map[string, int] = {\"a\": 1}\nlet rx: ref int[] = ref xs\nlet rm: ref map[string, int] = ref m\n"
	requireCompileError(t, prelude+"let n: int = length(rx)", "argument 1 to 'length': expected a value, got ref int[]", "hint: use '*rx' to read the referenced value")
	requireCompileError(t, prelude+"let ks: string[] = keys(rm)", "argument 1 to 'keys': expected a value, got ref map[string, int]", "hint: use '*rm'")
	requireCompileError(t, prelude+"let s: int[] = slice(rx, 0, 1)", "argument 1 to 'slice': expected a value, got ref int[]", "hint: use '*rx'")
	requireCompileError(t, prelude+"let c: bool = contains(rx, 1)", "argument 1 to 'contains': expected a value, got ref int[]", "hint: use '*rx'")
	requireCompileError(t, prelude+"let h: bool = has_key(rm, \"a\")", "argument 1 to 'has_key': expected a value, got ref map[string, int]", "hint: use '*rm'")
}

func TestValueNativesAcceptDerefAndShadowing(t *testing.T) {
	requireCompiles(t, "let xs: int[] = [1, 2]\nlet m: map[string, int] = {\"a\": 1}\nlet rx: ref int[] = ref xs\nlet rm: ref map[string, int] = ref m\nlet n: int = length(*rx)\nlet ks: string[] = keys(*rm)\nlet s: int[] = slice(*rx, 0, 1)\nlet c: bool = contains(*rx, 1)\nlet h: bool = has_key(*rm, \"a\")")
	// Sombreamento: um `length` do usuario nao e o native — a regra nao se aplica.
	requireCompiles(t, "func length(r: ref int[]) -> int\n    return 7\nend\nlet xs: int[] = [1]\nlet rx: ref int[] = ref xs\nlet n: int = length(rx)")
}

// M1 (revisao final #82): nas cinco nativas de colecao a regra R2 vale para
// o argumento 1 — a COLECAO. O argumento 2 de contains/has_key e um elemento
// ou uma chave quaisquer: um ref ali e um valor de busca legitimo (array de
// `ref int` procurado por identidade), nao uma leitura implicita.
func TestContainsAndHasKeySecondArgumentAcceptsRef(t *testing.T) {
	requireCompiles(t, `let i: int = 0
let ri: ref int = ref i
let ys: (ref int)[] = [ref i]
let c: bool = contains(ys, ri)`)
	requireCompiles(t, `let k: string = "a"
let rk: ref string = ref k
let m: map[string, int] = {"a": 1}
let h: bool = has_key(m, rk)`)
	// A colecao (argumento 1) continua barrada.
	requireCompileError(t, `let xs: int[] = [1, 2]
let rx: ref int[] = ref xs
let c: bool = contains(rx, 1)`, "argument 1 to 'contains': expected a value, got ref int[]", "hint: use '*rx'")
}

// Revisao final #82: as nativas de codificacao/serializacao tambem consomem
// seus argumentos como VALOR, em TODAS as posicoes. Sem esta lista um `ref T`
// chegaria ao native e ele codificaria o String() da referencia
// ("<ref ...>") em silencio.
func TestEncodingValueNativesRejectRefArgument(t *testing.T) {
	prelude := `let s: string = "a"
let xs: int[] = [1, 2]
let rs: ref string = ref s
let rx: ref int[] = ref xs
`
	cases := []struct{ name, call, got, hint string }{
		{"json_dumps", `let d: string = json_dumps(rx)`, "ref int[]", "*rx"},
		{"json_dumps_result", `let d: any = json_dumps_result(rx, 0)`, "ref int[]", "*rx"},
		{"json_parse", `let d: any = json_parse(rs)`, "ref string", "*rs"},
		{"base64_encode", `let d: string = base64_encode(rs)`, "ref string", "*rs"},
		{"base64_decode", `let d: bytes = base64_decode(rs)`, "ref string", "*rs"},
		{"hex", `let d: string = hex(rs)`, "ref string", "*rs"},
		{"hex_encode", `let d: string = hex_encode(rs)`, "ref string", "*rs"},
		{"hex_decode", `let d: bytes = hex_decode(rs)`, "ref string", "*rs"},
		{"base62_encode", `let d: any = base62_encode(rx)`, "ref int[]", "*rx"},
		{"base62_decode", `let d: any = base62_decode(rs)`, "ref string", "*rs"},
		{"to_bytes", `let d: bytes = to_bytes(rs)`, "ref string", "*rs"},
		{"fmt", `let d: string = fmt(rs)`, "ref string", "*rs"},
		{"crypto_pbkdf2_sha256", `let d: any = crypto_pbkdf2_sha256(rs, rs, 1, 1)`, "ref string", "*rs"},
		{"crypto_aes256_gcm_encrypt", `let d: any = crypto_aes256_gcm_encrypt(rs, rs)`, "ref string", "*rs"},
		{"crypto_aes256_gcm_decrypt", `let d: any = crypto_aes256_gcm_decrypt(rs, rs)`, "ref string", "*rs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requireCompileError(t, prelude+tc.call,
				"argument 1 to '"+tc.name+"': expected a value, got "+tc.got,
				"hint: use '"+tc.hint+"' to read the referenced value")
		})
	}
}

// fmt checa TODAS as posicoes (o formato e cada valor formatado).
func TestFmtRejectsRefInAnyArgument(t *testing.T) {
	requireCompileError(t, `let x: int = 1
let r: ref int = ref x
let d: string = fmt("%d", r)`, "argument 2 to 'fmt': expected a value, got ref int", "hint: use '*r'")
	requireCompiles(t, `let x: int = 1
let r: ref int = ref x
let d: string = fmt("%d", *r)`)
}

// I6 (revisao final #82): `any` PODE guardar um ref (R2 deixa o ref passar
// como valor para toda posicao any), entao `*a` com a: any nao e erro de
// compilacao — e um deref de tipo desconhecido, checado em runtime.
func TestDerefOfAnyCompiles(t *testing.T) {
	requireCompiles(t, `let x: int = 1
let r: ref int = ref x
let a: any = r
let n: int = *a`)
}

// O erro estatico continua para tipo conhecido e NAO-any.
func TestDerefOfKnownNonRefIsStaticError(t *testing.T) {
	requireCompileError(t, `let n: int = 7
let m: int = *n`, "cannot dereference non-reference value of type int")
}
