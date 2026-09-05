package compiler

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/chunk"
)

func TestGenericFunctionDeclarationEmitsNothing(t *testing.T) {
	code, _, err := New().Compile(parse("func first<T>(arr: T[]) -> T\n    return arr[0]\nend"))
	if err != nil {
		t.Fatal(err)
	}
	if containsOpcode(code.Code, chunk.OP_CLOSURE) {
		t.Fatal("template de funcao nao deve emitir OP_CLOSURE")
	}
	if containsOpcode(code.Code, chunk.OP_SET_GLOBAL) {
		t.Fatal("template de funcao nao deve emitir OP_SET_GLOBAL")
	}
}

func TestGenericStructDeclarationEmitsNothing(t *testing.T) {
	code, _, err := New().Compile(parse("struct Stack<T>\n    items: T[]\nend"))
	if err != nil {
		t.Fatal(err)
	}
	if containsOpcode(code.Code, chunk.OP_SET_GLOBAL) {
		t.Fatal("template de struct nao deve emitir OP_SET_GLOBAL")
	}
}

func TestTemplateRegisteredInRegistry(t *testing.T) {
	c := New()
	if _, _, err := c.Compile(parse("func first<T>(arr: T[]) -> T\n    return arr[0]\nend")); err != nil {
		t.Fatal(err)
	}
	tpl, ok := c.registryOrInit().Funcs["first"]
	if !ok {
		t.Fatal("template 'first' nao registrado")
	}
	if tpl.Module != "main" {
		t.Fatalf("Module = %q, quer main", tpl.Module)
	}
}

func TestNestedGenericDeclarationIsError(t *testing.T) {
	_, _, err := New().Compile(parse("func outer()\n    func inner<T>(x: T) -> T\n        return x\n    end\nend"))
	if err == nil || !strings.Contains(err.Error(), "top level") {
		t.Fatalf("esperava erro de genericos aninhados, veio %v", err)
	}
}
