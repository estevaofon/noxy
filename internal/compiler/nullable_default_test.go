package compiler

import (
	"strings"
	"testing"
)

// Spec §2.4 fase 2 (issue #105 item 1): struct e ref nus nunca sao null.

func TestStructWithoutInitializerNeedsNullable(t *testing.T) {
	src := "struct P\n    x: int\nend\nlet p: P\n"
	_, err := compileFunctionSource(t, src)
	want := "variable 'p' needs an initializer: P has no default value; hint: write 'let p: P = ...' or declare it as 'P?'"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestRefWithoutInitializerNeedsNullable(t *testing.T) {
	src := "let r: ref int\n"
	_, err := compileFunctionSource(t, src)
	want := "variable 'r' needs an initializer: ref int has no default value; hint: write 'let r: ref int = ...' or declare it as 'ref int?'"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNullableWithoutInitializerIsNull(t *testing.T) {
	if _, err := compileFunctionSource(t, "struct P\n    x: int\nend\nlet p: P?\nlet r: ref int?\n"); err != nil {
		t.Fatal(err)
	}
}

func TestNullIntoStructIsRejected(t *testing.T) {
	src := "struct P\n    x: int\nend\nlet p: P = null\n"
	_, err := compileFunctionSource(t, src)
	want := "expected P, got null\n  hint: declare it as 'P?' to allow null"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNullIntoRefLetIsRejected(t *testing.T) {
	_, err := compileFunctionSource(t, "let r: ref int = null\n")
	want := "expected ref int, got null\n  hint: declare it as 'ref int?' to allow null"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNullIntoRefParamIsRejected(t *testing.T) {
	src := "struct Node\n    v: int\nend\nfunc soma(n: ref Node) -> int\n    return n.v\nend\nlet x: int = soma(null)\n"
	_, err := compileFunctionSource(t, src)
	want := "argument 1 to 'soma': expected ref Node, got null\n  hint: declare the parameter as 'ref Node?' to accept null"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNullIntoValueParamIsRejected(t *testing.T) {
	src := "struct Node\n    v: int\nend\nfunc soma(n: Node) -> int\n    return n.v\nend\nlet x: int = soma(null)\n"
	_, err := compileFunctionSource(t, src)
	want := "argument 1 to 'soma': expected Node, got null\n  hint: declare it as 'Node?' to allow null"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNullIntoConstructorFieldIsRejected(t *testing.T) {
	src := "struct Node\n    v: int\n    prox: Node\nend\nlet n: Node = Node(1, null)\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "expected Node, got null\n  hint: declare it as 'Node?' to allow null") {
		t.Fatalf("got %v", err)
	}
}

func TestNullableFieldStillAcceptsNull(t *testing.T) {
	src := "struct Node\n    v: int\n    prox: Node?\nend\nlet n: Node = Node(1, null)\nlet r: ref Node? = null\nn.prox = null\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNullAssignedToStructVariableIsRejected(t *testing.T) {
	src := "struct P\n    x: int\nend\nlet p: P = P(1)\np = null\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "got null\n  hint: declare it as 'P?' to allow null") {
		t.Fatalf("got %v", err)
	}
}

func TestNullReturnFromNonNullableIsRejected(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc f() -> P\n    return null\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "expected P, got null\n  hint: declare it as 'P?' to allow null") {
		t.Fatalf("got %v", err)
	}
}
