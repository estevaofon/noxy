package compiler

import (
	"strings"
	"testing"
)

// Issue #120 item 3: em `a && b` / `a || b`, uma chamada nao-pura no operando
// direito pode derrubar a raiz compartilhada DEPOIS do teste do esquerdo —
// os fatos exportados para o ramo nao podem incluir essa raiz.

const narrowingCondPrelude = "func busca() -> map[string, any]?\n    return {\"nome\": \"Ana\"}\nend\nfunc toca() -> bool\n    m = null\n    return true\nend\nlet m: map[string, any]? = busca()\n"

func TestNarrowingAndOperandCallDropsSharedRootFacts(t *testing.T) {
	src := narrowingCondPrelude + "if m != null && toca() then\n    print(m[\"nome\"])\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "[line 10] 'm' may be null: it was tested, but 'm' is a global and a call in the condition ran after the test\n  hint: put the call before the test ('toca() && m != null'), bind it first ('let v = m' before the 'if') and use 'v', or move the code into a function"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNarrowingOrOperandCallDropsElseFacts(t *testing.T) {
	src := narrowingCondPrelude + "if m == null || toca() then\n    print(\"vazio\")\nelse\n    print(m[\"nome\"])\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'m' may be null: it was tested, but 'm' is a global and a call in the condition ran after the test") {
		t.Fatalf("want lost-in-condition diagnostic, got %v", err)
	}
}

func TestNarrowingWhileConditionCallDropsSharedRootFacts(t *testing.T) {
	src := narrowingCondPrelude + "while m != null && toca() do\n    print(m[\"nome\"])\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'m' may be null: it was tested, but 'm' is a global and a call in the condition ran after the test") {
		t.Fatalf("want lost-in-condition diagnostic, got %v", err)
	}
}

func TestNarrowingAndOperandPureBuiltinKeepsFacts(t *testing.T) {
	src := narrowingCondPrelude + "if m != null && length(m) > 0 then\n    print(m[\"nome\"])\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingAndOperandCallKeepsValueLocalFacts(t *testing.T) {
	// Local de valor: ninguem fora do frame alcanca `p` durante toca().
	src := "struct P\n    x: int\nend\nfunc toca() -> bool\n    return true\nend\nfunc f(p: P?) -> int\n    if p != null && toca() then\n        return p.x\n    end\n    return 0\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingCallBeforeTestInConditionKeepsFacts(t *testing.T) {
	src := narrowingCondPrelude + "if toca() && m != null then\n    print(m[\"nome\"])\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}
