package compiler

import (
	"strings"
	"testing"
)

// Issue #118 item 1: `any` entra num slot tipado em qualquer posicao — let
// anotado, argumento de assinatura exata, return — com a mesma checagem de
// runtime. Antes so o `let` aceitava, e o wrapper de extensao precisava de
// um `let` temporario por funcao.

func TestExactCallAcceptsAnyArgument(t *testing.T) {
	src := "func add(a: int) -> int\n    return a\nend\nlet x: any = 1\nadd(x)\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestReturnAcceptsAnyValue(t *testing.T) {
	src := "func nativo() -> any\n    return 1\nend\nfunc tipado() -> int\n    return nativo()\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionWrapperIdiomWithoutTemporaryLet(t *testing.T) {
	src := "func nativo() -> any\n    return [{\"a\": 1}]\nend\nfunc scan() -> map[string, any][]\n    return nativo()\nend\nfunc conta(xs: map[string, any][]) -> int\n    return length(xs)\nend\nconta(nativo())\nconta(scan())\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestExactCallRejectsAnyForExactCallableParameter(t *testing.T) {
	src := "func apply(f: func(int) -> int) -> int\n    return f(1)\nend\nlet g: any = 1\napply(g)\n"
	_, err := compileFunctionSource(t, src)
	want := "argument 1 to 'apply': expected func(int) -> int, got any"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestReturnRejectsAnyForExactCallableType(t *testing.T) {
	src := "func nativo() -> any\n    return 1\nend\nfunc pick() -> func(int) -> int\n    return nativo()\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "return type mismatch in 'pick': expected func(int) -> int, got any"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}
