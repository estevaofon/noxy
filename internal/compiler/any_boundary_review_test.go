package compiler

import (
	"strings"
	"testing"
)

// Revisao do PR #119: `any` atravessa a fronteira so no TOPO do tipo. Um
// `ref any` nao e um `ref int` (o slot apontado pode mudar de tipo depois), e
// `any[]`/`map[string, any]` sao tipos concretos e invariantes — a recusa de
// compilacao que develop tinha volta.

func TestRefToAnySlotIsNotARefToInt(t *testing.T) {
	src := "func bump(r: ref int) -> int\n    return *r + 1\nend\nlet a: any = \"texto\"\nbump(ref a)\n"
	_, err := compileFunctionSource(t, src)
	want := "argument 1 to 'bump': expected ref int, got ref any"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestExactCallRejectsAnyArrayForIntArrayParameter(t *testing.T) {
	src := "func f(xs: int[]) -> int\n    return length(xs)\nend\nlet a: any[] = [1]\nf(a)\n"
	_, err := compileFunctionSource(t, src)
	want := "argument 1 to 'f': expected int[], got any[]"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestReturnRejectsAnyMapForTypedMap(t *testing.T) {
	src := "func g() -> map[string, int]\n    let m: map[string, any] = {\"a\": 1}\n    return m\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "return type mismatch in 'g': expected map[string, int], got map[string, any]"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestExactCallAcceptsAnyElementArrayWhereDeclared(t *testing.T) {
	// map[string, any][] e o tipo declarado dos dois lados: nao ha fronteira.
	src := "func conta(xs: map[string, any][]) -> int\n    return length(xs)\nend\nlet xs: map[string, any][] = [{\"a\": 1}]\nconta(xs)\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}
