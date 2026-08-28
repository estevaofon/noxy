package compiler

import (
	"strings"
	"testing"
)

// Spec §2.4 (issue #105 item 1): narrowing por expressoes estaveis.

func TestNarrowingIfNotNull(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?) -> int\n    if p != null then\n        return p.x\n    end\n    return 0\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingEarlyReturn(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?) -> int\n    if p == null then\n        return -1\n    end\n    return p.x\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingElseBranchOfEqualsNull(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?) -> int\n    if p == null then\n        return -1\n    else\n        return p.x\n    end\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingAndOperator(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?) -> bool\n    return p != null && p.x > 0\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingOrOperator(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?) -> bool\n    return p == null || p.x > 0\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingWhileTraversal(t *testing.T) {
	src := "struct Node\n    valor: int\n    prox: Node?\nend\nfunc soma(inicio: Node?) -> int\n    let total: int = 0\n    let atual: Node? = inicio\n    while atual != null do\n        total = total + atual.valor\n        atual = atual.prox\n    end\n    return total\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingFieldPathThroughRef(t *testing.T) {
	src := "struct Node\n    valor: int\n    prox: Node?\nend\nfunc insere(no: ref Node, v: int) -> void\n    if no.prox == null then\n        no.prox = Node(v, null)\n    else\n        insere(ref no.prox, v)\n    end\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingLostAfterAssignment(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?, q: P?) -> int\n    if p != null then\n        p = q\n        return p.x\n    end\n    return 0\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "'p' may be null; test it first\n  hint: use 'if p != null then ... end'"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNarrowingPathLostAfterCallThroughRef(t *testing.T) {
	src := "struct Node\n    valor: int\n    prox: Node?\nend\nfunc toca(n: ref Node) -> void\n    n.prox = null\nend\nfunc f(no: ref Node) -> int\n    if no.prox != null then\n        toca(no)\n        return no.prox.valor\n    end\n    return 0\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'no.prox' may be null") {
		t.Fatalf("got %v", err)
	}
}

func TestNarrowingPathSurvivesCallOnValueLocal(t *testing.T) {
	src := "struct Node\n    valor: int\n    prox: Node?\nend\nfunc f(no: Node) -> int\n    if no.prox != null then\n        print(no.prox.valor)\n        return no.prox.valor\n    end\n    return 0\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingLostInLoopThatReassigns(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?, q: P?) -> int\n    let total: int = 0\n    if p != null then\n        let i: int = 0\n        while i < 3 do\n            total = total + p.x\n            p = q\n            i = i + 1\n        end\n    end\n    return total\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'p' may be null") {
		t.Fatalf("got %v", err)
	}
}

func TestNarrowingSurvivesLoopThatDoesNotReassign(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?) -> int\n    let total: int = 0\n    if p != null then\n        let i: int = 0\n        while i < 3 do\n            total = total + p.x\n            i = i + 1\n        end\n    end\n    return total\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestDerefNullableRefNeedsTest(t *testing.T) {
	src := "func f(r: ref int?) -> int\n    return *r\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'r' may be null; test it first") {
		t.Fatalf("got %v", err)
	}
}

func TestDerefNullableRefAfterTest(t *testing.T) {
	src := "func f(r: ref int?) -> int\n    if r != null then\n        return *r\n    end\n    return 0\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestShortcutOnNullableRefNeedsTest(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(r: ref P?) -> int\n    return r.x\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'r' may be null; test it first") {
		t.Fatalf("got %v", err)
	}
}

func TestUnstableNullableNeedsLet(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc g() -> P?\n    return null\nend\nlet v: int = g().x\n"
	_, err := compileFunctionSource(t, src)
	want := "value of type P? may be null; test it first\n  hint: bind it with 'let' and test for null"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNullableConditionNeedsTest(t *testing.T) {
	src := "func f(b: bool?) -> int\n    if b then\n        return 1\n    end\n    return 0\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'b' may be null") {
		t.Fatalf("got %v", err)
	}
}

func TestNullableArgumentHint(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc g(p: P) -> int\n    return p.x\nend\nfunc f(p: P?) -> int\n    return g(p)\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "argument 1 to 'g': expected P, got P?\n  hint: 'p' may be null; test it first"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNullableReturnHint(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?) -> P\n    return p\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "return type mismatch in 'f': expected P, got P?\n  hint: 'p' may be null; test it first"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNewLocalWithSameNameIsNotNarrowed(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(p: P?) -> int\n    if p != null then\n        let q: P? = null\n        if true then\n            let p: P? = q\n            return p.x\n        end\n    end\n    return 0\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'p' may be null") {
		t.Fatalf("got %v", err)
	}
}
