package compiler

import (
	"strings"
	"testing"
)

// Spec §2.4 (issue #105): `ref T?` e `ref (T?)` — modo de referencia
// desembrulha `?`; ref de local estreitado e `ref T`.

func TestRefFieldNullableIsRefSlot(t *testing.T) {
	src := "struct Cell\n    v: int\n    prox: ref Cell?\nend\nlet a: Cell = Cell(1, null)\nlet b: Cell = Cell(2, ref a)\nb.prox = null\nb.prox = ref a\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestRefParamNullableIsRefMode(t *testing.T) {
	src := "func bump(n: ref int?) -> void\n    if n != null then\n        *n = *n + 1\n    end\nend\nlet x: int = 1\nbump(ref x)\nbump(null)\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestRefOfNarrowedNullableIsRefT(t *testing.T) {
	src := "struct Node\n    v: int\nend\nfunc bump(n: ref Node) -> void\n    n.v = n.v + 1\nend\nfunc f(raiz: Node?) -> void\n    if raiz != null then\n        bump(ref raiz)\n    end\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestRefOfNullableWithoutTestIsRefNullable(t *testing.T) {
	src := "struct Node\n    v: int\nend\nfunc bump(n: ref Node) -> void\n    n.v = n.v + 1\nend\nfunc f(raiz: Node?) -> void\n    bump(ref raiz)\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "expected ref Node, got ref (Node?)") {
		t.Fatalf("got %v", err)
	}
}

func TestNullableRefIntoNonNullRefParamIsRejected(t *testing.T) {
	src := "func bump(n: ref int) -> void\n    *n = *n + 1\nend\nfunc f(r: ref int?) -> void\n    bump(r)\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "argument 1 to 'bump': expected ref int, got ref int?\n  hint: 'r' may be null; test it first"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNullableRefAfterTestIntoNonNullRefParam(t *testing.T) {
	src := "func bump(n: ref int) -> void\n    *n = *n + 1\nend\nfunc f(r: ref int?) -> void\n    if r != null then\n        bump(r)\n    end\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestWriteThroughNullableRefNeedsTest(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f(r: ref P?) -> void\n    r.x = 1\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'r' may be null; test it first") {
		t.Fatalf("got %v", err)
	}
}
