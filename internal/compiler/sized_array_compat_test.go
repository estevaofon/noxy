package compiler

import (
	"testing"

	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

// Regressao da #133: a identidade de struct por Decl removeu de
// areTypesCompatible o atalho `expected.String() == actual.String()`, e com
// ele sumiu a unica coisa que fazia um array DIMENSIONADO aceitar um valor —
// ArrayType.String omite Size, entao `int[15]` e `int[]` tem a MESMA string.
// O sintoma era o erro impossivel `expected int[], got int[]`, que quebrou
// quicksort_in_place.nx e use_quicksort.nx no runner.
//
// Estes testes caracterizam o comportamento PRE-#133 (verificado rodando os
// mesmos programas no `main`): o tamanho de array NUNCA foi checado
// estaticamente na atribuicao, em nenhuma direcao. A spec §2 ("Fixed Size
// Arrays") exige `let fixed: int[5] = [1, 2, 3, 4, 5]` e
// `let zeroed: int[100] = zeros(100)`; e o proprio quicksort_in_place.nx
// guarda 16 elementos num `int[15]`. Checar dimensao seria mudanca de
// linguagem, com spec — nao efeito colateral da identidade por Decl.

func compileProgramSource(t *testing.T, source string) error {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	_, _, err := New().Compile(program)
	return err
}

func TestSizedArrayAcceptsLiteralAndDynamicValue(t *testing.T) {
	cases := map[string]string{
		// A forma da spec §2.
		"exact literal":   "let fixed: int[5] = [1, 2, 3, 4, 5]\n",
		"shorter literal": "let big: int[15] = [1, 2, 3]\n",
		// O caso que quebrava o runner: 16 elementos num int[15].
		"longer literal": "let big: int[15] = [10, 7, 8, 9, 1, 5, 2, 6, 3, 4, 15, 12, 11, 14, 13, 0]\n",
		// A segunda forma da spec §2: valor dinamico (int[], Size 0).
		"dynamic value": "let zeroed: int[100] = zeros(100)\n",
		// Sentido inverso: slot dinamico recebe array dimensionado.
		"sized into dynamic": "let fixed: int[5] = [1, 2, 3, 4, 5]\nlet loose: int[] = fixed\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			if err := compileProgramSource(t, source); err != nil {
				t.Fatalf("must compile (pre-#133 behavior): %v", err)
			}
		})
	}
}

func TestSizedArrayPassesToDynamicArrayParameter(t *testing.T) {
	// `areStrictTypesCompatible` (function_types.go) ja aceitava isto pelo
	// ramo `e.Size == 0` e NAO foi tocado pela #133 — o teste fixa que o
	// conserto de areTypesCompatible nao mexeu no site de chamada.
	if err := compileProgramSource(t, `func total(xs: int[]) -> int
    return length(xs)
end
let fixed: int[5] = [1, 2, 3, 4, 5]
let n: int = total(fixed)
`); err != nil {
		t.Fatalf("sized array into a dynamic parameter must compile: %v", err)
	}
}

func TestSizedArrayStillChecksTheElementType(t *testing.T) {
	// Controle: ignorar o TAMANHO nao afrouxa o ELEMENTO.
	err := compileProgramSource(t, "let bad: int[5] = [\"a\"]\n")
	requireErrorMentions(t, err, "expected int[], got string[]")
}

func TestRefToArrayIgnoresSizeInBothDirections(t *testing.T) {
	// Regressao da #133 (segunda onda): o ramo de *ast.ArrayType ficou
	// insensivel ao tamanho, mas *ast.RefType nao tinha ramo nenhum em
	// areTypesCompatible e caia em typesEquivalent, que compara Size. Como
	// ArrayType.String omite Size, o erro saia contraditorio:
	// "expected ref int[], got ref int[]". No develop `ref int[] = ref a`
	// com `a: int[5]` compilava e imprimia 5.
	cases := map[string]string{
		"sized into dynamic": "let a: int[5] = [1, 2, 3, 4, 5]\nlet r: ref int[] = ref a\n",
		"dynamic into sized": "let b: int[] = [1, 2, 3]\nlet r: ref int[5] = ref b\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			if err := compileProgramSource(t, source); err != nil {
				t.Fatalf("must compile (pre-#133 behavior): %v", err)
			}
		})
	}
}

func TestRefStaysInvariantOutsideArraySize(t *testing.T) {
	// Controle: ignorar o TAMANHO do array nao afrouxa `ref` em nada mais.
	t.Run("primitive alvo", func(t *testing.T) {
		err := compileProgramSource(t, "let n: int = 1\nlet r: ref int = ref n\nlet s: ref string = r\n")
		requireErrorMentions(t, err, "expected ref string, got ref int")
	})
	t.Run("struct distinto", func(t *testing.T) {
		err := compileProgramSource(t, `struct P
    x: int
end
struct Q
    x: int
end
let p: P = P(1)
let r: ref P = ref p
let s: ref Q = r
`)
		requireErrorMentions(t, err, "expected ref Q, got ref P")
	})
	t.Run("elemento de array do alvo", func(t *testing.T) {
		err := compileProgramSource(t, "let a: int[5] = [1, 2, 3, 4, 5]\nlet r: ref int[] = ref a\nlet s: ref string[] = r\n")
		requireErrorMentions(t, err, "expected ref string[], got ref int[]")
	})
}

func TestRefToSizedArrayPassesToDynamicRefParameter(t *testing.T) {
	// Controle do site de chamada: areStrictTypesCompatible ja aceitava isto
	// pelo ramo `e.Size == 0` e NAO foi tocado pela #133 (verificado contra um
	// binario de develop: aceito nos dois).
	if err := compileProgramSource(t, `func total(xs: ref int[]) -> int
    return length(*xs)
end
let a: int[5] = [1, 2, 3, 4, 5]
let n: int = total(ref a)
`); err != nil {
		t.Fatalf("ref para array dimensionado em parametro ref dinamico deve compilar: %v", err)
	}
}

func TestRefToArrayStaysInvariantInTheElement(t *testing.T) {
	// Regressao da primeira onda: o ramo novo de *ast.RefType comparava os
	// elementos com areTypesCompatible — a regra de ATRIBUICAO, que alarga.
	// Como `int?` aceita `int` e `any` aceita tudo, `ref (int?)[] = r` e
	// `ref any[] = r` com `r: ref int[]` passavam, e `s[0] = null` /
	// `s[0] = "boom"` gravavam null e string dentro do `int[]` do dono
	// (develop recusava os dois em compilacao). So o TAMANHO e ignorado.
	t.Run("elemento anulavel", func(t *testing.T) {
		err := compileProgramSource(t, "let a: int[] = [1, 2, 3]\nlet r: ref int[] = ref a\nlet s: ref (int?)[] = r\n")
		requireErrorMentions(t, err, "expected ref int?[], got ref int[]")
	})
	t.Run("elemento any", func(t *testing.T) {
		err := compileProgramSource(t, "let a: int[] = [1, 2, 3]\nlet r: ref int[] = ref a\nlet s: ref any[] = r\n")
		requireErrorMentions(t, err, "expected ref any[], got ref int[]")
	})
	t.Run("struct distinto no elemento", func(t *testing.T) {
		err := compileProgramSource(t, `struct P
    x: int
end
struct Q
    x: int
end
let a: P[] = []
let r: ref P[] = ref a
let s: ref Q[] = r
`)
		requireErrorMentions(t, err, "expected ref Q[], got ref P[]")
	})
}

func TestRefToNestedArrayIgnoresSizeAtEveryLevel(t *testing.T) {
	// O tamanho e ignorado em todo nivel de aninhamento, nas duas direcoes.
	cases := map[string]string{
		"dimensionado para dinamico": "let a: int[2][3] = [[1, 2, 3], [4, 5, 6]]\nlet r: ref int[][] = ref a\n",
		"dinamico para dimensionado": "let a: int[][] = [[1, 2, 3]]\nlet r: ref int[2][3] = ref a\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			requireNoError(t, compileProgramSource(t, source))
		})
	}
}

func TestRefToArrayOfModuleStructFollowsTheDeclaration(t *testing.T) {
	// Identidade por Decl atravessa o `ref`: `geometry.Point` (namespace) e
	// `Point` (select) sao a MESMA declaracao, entao um ref a `Point[]` entra
	// num slot `ref geometry.Point[]`.
	root := t.TempDir()
	writeModuleFile(t, root, "geometry.nx", "struct Point\n    x: int\nend\n")
	requireNoError(t, compileSourceAtRoot(t, root, `use geometry select Point
use geometry
let a: Point[] = []
let r: ref Point[] = ref a
let s: ref geometry.Point[] = r
`))
}
