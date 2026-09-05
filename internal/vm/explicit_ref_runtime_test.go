package vm

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// print/to_str/f-string recebem o ref como valor e mostram a referencia
// (spec 2026-08-24-explicit-ref, decisao (a)); `*r` mostra o valor.
func TestToStrOfRefShowsReferenceNotValue(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let x: int = 42
    let r: ref int = ref x
    test_report([to_str(r), to_str(*r), f"{r}", f"{*r}"])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 4 {
		t.Fatalf("esperava 4 celulas, obtido %s", got.String())
	}
	asRef, _ := cells[0].Obj.(string)
	asVal, _ := cells[1].Obj.(string)
	fRef, _ := cells[2].Obj.(string)
	fVal, _ := cells[3].Obj.(string)
	if !strings.HasPrefix(asRef, "<ref") || !strings.HasPrefix(fRef, "<ref") {
		t.Fatalf("to_str(r)=%q f\"{r}\"=%q, want prefix <ref", asRef, fRef)
	}
	if asVal != "42" || fVal != "42" {
		t.Fatalf("to_str(*r)=%q f\"{*r}\"=%q, want 42", asVal, fVal)
	}
}

// `return *r` de composto continua devolvendo um valor independente
// (o OP_COPY do caminho antigo de return-deref e preservado).
func TestReturnDerefCompositeIsIndependentCopy(t *testing.T) {
	got := captureVMSource(t, `
func le(r: ref int[]) -> int[]
    return *r
end
func main()
    let xs: int[] = [1, 2]
    let copia: int[] = le(ref xs)
    append(ref copia, 3)
    test_report([length(xs), length(copia)])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 2 || cells[0].Int() != 2 || cells[1].Int() != 3 {
		t.Fatalf("[len xs, len copia] = %s, want [2, 3]", got.String())
	}
}

// R3 em runtime: `*x` sobre um valor que nao e ref (so alcancavel por tipo
// estatico desconhecido — membro dinamico de modulo/any nested) e erro.
func TestDerefOfNonRefAtRuntimeIsError(t *testing.T) {
	err := interpretVMSource(t, New(), `
struct Caixa
    v: any
end
func main()
    let c: Caixa = Caixa(7)
    let d: any = c
    let n: int = *d.v
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "cannot dereference int") {
		t.Fatalf("err = %v, want 'cannot dereference int'", err)
	}
}

// R1 em runtime: `ref` sobre um slot ref T alcancado por base any nao
// encaminha — e erro, espelhando o estatico 'is already a reference'.
func TestRefOfRefSlotThroughAnyIsError(t *testing.T) {
	err := interpretVMSource(t, New(), `
struct Node
    valor: int
    next: ref Node?
end
func toca(n: ref Node) -> void
    return
end
func main()
    let seg: Node = Node(2, null)
    let a: any = Node(1, ref seg)
    toca(ref a.next)
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "slot 'next' already holds a reference") || !strings.Contains(err.Error(), "pass it directly") {
		t.Fatalf("err = %v, want 'slot 'next' already holds a reference ... pass it directly'", err)
	}
}

func TestRefOfRefIndexSlotThroughAnyIsError(t *testing.T) {
	err := interpretVMSource(t, New(), `
func toca(n: ref int) -> void
    return
end
func main()
    let x: int = 1
    let xs: (ref int)[] = [ref x]
    let a: any = xs
    toca(ref a[0])
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "already holds a reference") {
		t.Fatalf("err = %v, want 'already holds a reference'", err)
	}
}

// Task 10a (issue #82): length/keys/slice/contains/has_key sao nativas sem
// assinatura. O compilador rejeita `ref T` estatico (explicit_read_test.go em
// internal/compiler); aqui a fronteira dinamica (base any) precisa da mesma
// recusa em runtime, ja que essas nativas nao tem NativeSignature para o
// validador de modos de parametro pegar.
func TestValueNativesRejectRefAtRuntimeThroughAny(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"length", "let xs: int[] = [1, 2]\nlet r: ref int[] = ref xs\nlet a: any = r\nlet n: int = length(a)", "length: argument 1 expected a value, got ref"},
		{"keys", "let m: map[string, int] = {\"a\": 1}\nlet r: ref map[string, int] = ref m\nlet a: any = r\nlet k: any = keys(a)", "keys: argument 1 expected a value, got ref"},
		{"slice", "let xs: int[] = [1, 2]\nlet r: ref int[] = ref xs\nlet a: any = r\nlet s: any = slice(a, 0, 1)", "slice: argument 1 expected a value, got ref"},
		{"contains", "let xs: int[] = [1, 2]\nlet r: ref int[] = ref xs\nlet a: any = r\nlet c: any = contains(a, 1)", "contains: argument 1 expected a value, got ref"},
		{"has_key", "let m: map[string, int] = {\"a\": 1}\nlet r: ref map[string, int] = ref m\nlet a: any = r\nlet h: any = has_key(a, \"a\")", "has_key: argument 1 expected a value, got ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretOrCompileErr(t, New(), tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "use '*r'") {
				t.Fatalf("%s: err = %v, want %q + hint", tc.name, err, tc.want)
			}
		})
	}
}

func TestLengthOfDerefRefArrayAtRuntime(t *testing.T) {
	got := captureVMSource(t, "func main()\n    let xs: int[] = [1, 2]\n    let r: ref int[] = ref xs\n    test_report(length(*r))\nend\nmain()\n")
	if got.Int() != 2 {
		t.Fatalf("length(*r) = %s, want 2", got.String())
	}
}

// R2 (spec 2026-08-24-explicit-ref, ultimo paragrafo): um parametro `any`
// recebe o ref COMO VALOR. validateParameterModes nao pode recusar VAL_REF
// quando o modo declarado do parametro e any — o compilador ja deixa passar
// (explicit_read_test.go), so o validador de runtime barrava.
func TestAnyParameterAcceptsRefAsValueAtRuntime(t *testing.T) {
	got := captureVMSource(t, `
func guarda(v: any) -> any
    return v
end
func main()
    let x: int = 2
    let r: ref int = ref x
    let kept: any = guarda(r)
    let v: any = r
    let kept2: any = guarda(v)
    test_report([to_str(kept), to_str(kept2)])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 2 {
		t.Fatalf("esperava 2 celulas, obtido %s", got.String())
	}
	direto, _ := cells[0].Obj.(string)
	viaAny, _ := cells[1].Obj.(string)
	if !strings.HasPrefix(direto, "<ref") {
		t.Fatalf("guarda(r) devolveu %q, want prefixo <ref", direto)
	}
	if !strings.HasPrefix(viaAny, "<ref") {
		t.Fatalf("guarda(v) com v: any = r devolveu %q, want prefixo <ref", viaAny)
	}
}

// Revisao final #82: as nativas de codificacao/serializacao tambem sao
// nativas sem assinatura — a fronteira dinamica (base any) precisa da mesma
// recusa em runtime que o compilador ja da no caso estatico. Sem ela o ref
// seria codificado como o texto "<ref ...>" em silencio.
func TestEncodingValueNativesRejectRefAtRuntimeThroughAny(t *testing.T) {
	prelude := "let x: int = 1\nlet r: ref int = ref x\nlet a: any = r\n"
	cases := []struct{ name, src, want string }{
		{"json_dumps", prelude + "let d: any = json_dumps(a)", "json_dumps: argument 1 expected a value, got ref"},
		{"base64_encode", prelude + "let d: any = base64_encode(a)", "base64_encode: argument 1 expected a value, got ref"},
		{"hex_encode", prelude + "let d: any = hex_encode(a)", "hex_encode: argument 1 expected a value, got ref"},
		{"fmt", prelude + "let d: any = fmt(\"%d\", a)", "fmt: argument 2 expected a value, got ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretOrCompileErr(t, New(), tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "use '*r'") {
				t.Fatalf("%s: err = %v, want %q + hint", tc.name, err, tc.want)
			}
		})
	}
}

// M1: o argumento 2 de contains e um valor de busca — um ref ali procura por
// IDENTIDADE dentro de um `(ref int)[]`, e nao e leitura implicita nenhuma.
func TestContainsFindsRefElementByIdentity(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let x: int = 1
    let y: int = 1
    let ys: (ref int)[] = [ref x]
    let r: ref int = ref x
    let outro: ref int = ref y
    test_report([contains(ys, r), contains(ys, outro)])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 2 || !cells[0].Bool() || cells[1].Bool() {
		t.Fatalf("[contains(ys, r), contains(ys, outro)] = %s, want [true, false]", got.String())
	}
}

// M1 pelo caminho dinamico: has_key com a chave chegando como ref via any
// nao e barrado (so o argumento 1 e checado); o mapa nao tem essa chave, mas
// a chamada TEM de rodar em vez de erro.
func TestHasKeySecondArgumentRefAtRuntimeIsNotRejected(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let k: string = "a"
    let rk: ref string = ref k
    let a: any = rk
    let m: map[string, int] = {"a": 1}
    test_report(has_key(m, a))
end
main()
`)
	if got.Bool() {
		t.Fatalf("has_key(m, <ref>) = %s, want false (a chave e o ref, nao o texto)", got.String())
	}
}

// I5 (revisao final #82): ler atraves de um ref nulo e erro em runtime — a
// mesma frase que resolveReferenceValue ja usa. Antes o OP_DEREF passava o
// null adiante e o programa seguia com um valor nulo silencioso onde o tipo
// estatico prometia um T.
func TestDerefOfNullRefIsRuntimeError(t *testing.T) {
	// Spec §2.4: no caminho tipado o null so cabe num `ref int?`, e a
	// leitura sem teste e erro de COMPILACAO; o null so chega ao OP_DEREF
	// pela fronteira dinamica (`any`).
	compileErr := interpretOrCompileErr(t, New(), `
func main()
    let r: ref int? = null
    let n: int = *r
end
main()
`)
	if compileErr == nil || !strings.Contains(compileErr.Error(), "'r' may be null; test it first") {
		t.Fatalf("err = %v, want \"'r' may be null; test it first\"", compileErr)
	}
	err := interpretVMSource(t, New(), `
func main()
    let a: any = null
    let n: int = *a
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "cannot dereference null reference") {
		t.Fatalf("err = %v, want 'cannot dereference null reference'", err)
	}
}

// I5, caminho do parametro: um null vindo por `any` ainda entra num `ref T`
// na fronteira dinamica; so a LEITURA erra. (No caminho tipado `le(null)` e
// erro de compilacao — spec §2.4.)
func TestDerefOfNullRefParameterIsRuntimeError(t *testing.T) {
	err := interpretVMSource(t, New(), `
func le(r: ref int) -> int
    return *r
end
func main()
    let a: any = null
    let n: int = le(a)
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "cannot dereference null reference") {
		t.Fatalf("err = %v, want 'cannot dereference null reference'", err)
	}
}

// A comparacao `r == null` nao emite OP_DEREF: continua respondendo true sem
// erro nenhum (e o teste de sentinela que todo no de lista encadeada faz).
func TestNullRefComparisonStillWorks(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let r: ref int? = null
    let x: int = 1
    let s: ref int = ref x
    test_report([r == null, s == null])
end
main()
`)
	cells := semArray(t, got)
	if len(cells) != 2 || !cells[0].Bool() || cells[1].Bool() {
		t.Fatalf("[r == null, s == null] = %s, want [true, false]", got.String())
	}
}

// I6 em runtime: `*a` com a: any guardando um ref le o valor apontado; com
// a: any guardando um valor comum, erra ("cannot dereference int").
func TestDerefOfAnyHoldingRefReadsPointedValue(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let x: int = 41
    let r: ref int = ref x
    let a: any = r
    test_report(*a)
end
main()
`)
	if got.Type != value.VAL_INT || got.Int() != 41 {
		t.Fatalf("*a = %s, want 41", got.String())
	}
}

func TestDerefOfAnyHoldingPlainValueIsRuntimeError(t *testing.T) {
	err := interpretVMSource(t, New(), `
func main()
    let a: any = 7
    let n: int = *a
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "cannot dereference int") {
		t.Fatalf("err = %v, want 'cannot dereference int'", err)
	}
}

// I7 (revisao final #82): `for v in a` com a: any guardando um ref caia no
// OP_LEN, que devolvia 0 para tipo nao reconhecido — o laco simplesmente nao
// iterava, em silencio. R2 diz que um ref nunca e lido implicitamente: aqui
// o pedido de iteracao e o erro.
func TestForOverRefReachedThroughAnyIsRuntimeError(t *testing.T) {
	err := interpretVMSource(t, New(), `
func main()
    let xs: int[] = [1, 2]
    let r: ref int[] = ref xs
    let a: any = r
    for v in a do
        print(v)
    end
end
main()
`)
	if err == nil || !strings.Contains(err.Error(), "cannot iterate over a ref") || !strings.Contains(err.Error(), "use '*r'") {
		t.Fatalf("err = %v, want 'cannot iterate over a ref' + hint", err)
	}
}

// O laco sobre `*r` (leitura explicita) continua iterando normalmente pela
// mesma fronteira dinamica.
func TestForOverDerefThroughAnyIterates(t *testing.T) {
	got := captureVMSource(t, `
func main()
    let xs: int[] = [1, 2, 3]
    let r: ref int[] = ref xs
    let a: any = *r
    let soma: int = 0
    for v in a do
        soma = soma + v
    end
    test_report(soma)
end
main()
`)
	if got.Type != value.VAL_INT || got.Int() != 6 {
		t.Fatalf("soma = %s, want 6", got.String())
	}
}
