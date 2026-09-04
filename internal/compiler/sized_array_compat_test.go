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
