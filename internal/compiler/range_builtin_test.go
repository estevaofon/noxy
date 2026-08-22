package compiler

import (
	"strings"
	"testing"
)

// range e um builtin do runtime (sem import) tipado pelo compilador como as
// outras chamadas de compileBuiltinCall: aridade 1..3, argumentos int e
// retorno int[] — assim `for i in range(n)` tipa i como int, em vez de cair na
// brecha das natives sem assinatura (tipo de retorno desconhecido).

func TestRangeBuiltinReturnsIntArray(t *testing.T) {
	if _, err := compileFunctionSource(t, `
let one: int[] = range(3)
let two: int[] = range(1, 3)
let three: int[] = range(10, 0, -2)`); err != nil {
		t.Fatal(err)
	}
	_, err := compileFunctionSource(t, `let wrong: string = range(3)`)
	if err == nil || !strings.Contains(err.Error(), "expected string, got int[]") {
		t.Fatalf("error=%v", err)
	}
}

func TestRangeBuiltinRequiresOneToThreeArguments(t *testing.T) {
	for source, want := range map[string]string{
		`range()`:           "range expects 1 to 3 arguments, got 0",
		`range(1, 2, 3, 4)`: "range expects 1 to 3 arguments, got 4",
	} {
		_, err := compileFunctionSource(t, source)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error=%v, want %q", source, err, want)
		}
	}
}

func TestRangeBuiltinRejectsNonIntArguments(t *testing.T) {
	for source, want := range map[string]string{
		`range("a")`:        "argument 1 to 'range': expected int, got string",
		`range(0, 1.5)`:     "argument 2 to 'range': expected int, got float",
		`range(0, 5, true)`: "argument 3 to 'range': expected int, got bool",
	} {
		_, err := compileFunctionSource(t, source)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: error=%v, want %q", source, err, want)
		}
	}
}

func TestRangeBuiltinTypesLoopVariableAsInt(t *testing.T) {
	_, err := compileFunctionSource(t, `
for i in range(3) do
    let s: string = i
end`)
	if err == nil || !strings.Contains(err.Error(), "expected string, got int") {
		t.Fatalf("error=%v", err)
	}
}

func TestUserFunctionNamedRangeShadowsBuiltin(t *testing.T) {
	// Mesma regra de append/pop: um binding do programa com o nome do builtin
	// vence, e a chamada segue o caminho normal com a assinatura do usuario.
	if _, err := compileFunctionSource(t, `
func range(label: string) -> string
    return label
end
let s: string = range("x")`); err != nil {
		t.Fatal(err)
	}
}
