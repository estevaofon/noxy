package compiler

import (
	"strings"
	"testing"
)

// Spec §2.4 (issue #105 item 1, fase 1): `T?` entra em todo walker de tipo;
// T? aceita T, T? e null; T nao aceita T?.

func TestNullableAssignmentsCompile(t *testing.T) {
	src := `struct Node
    valor: int
    prox: Node?
end
let a: Node? = null
let b: Node? = Node(1, null)
let c: Node = Node(2, null)
a = c
let r: ref Node? = null
r = ref c
let xs: Node?[] = [null, c]
func busca(k: int) -> Node?
    return null
end
let d = busca(1)
let e: Node? = d
`
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestNullableIntoNonNullIsRejected(t *testing.T) {
	src := "struct P\n    x: int\nend\nlet a: P? = null\nlet b: P = a\n"
	_, err := compileFunctionSource(t, src)
	want := "[line 5] type mismatch in 'b' declaration: expected P, got P?\n  hint: 'a' may be null; test it first"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNullableIntoAnyIsAccepted(t *testing.T) {
	src := "struct P\n    x: int\nend\nlet a: P? = null\nlet b: any = a\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestGenericBindsNullable(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc first<T>(xs: T[]) -> T\n    return xs[0]\nend\nlet ps: P?[] = [null]\nlet p: P? = first(ps)\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatalf("T must bind P?: %v", err)
	}
}

func TestGenericNullableFieldNormalizes(t *testing.T) {
	// value: T? com T = P? e P? (nao P??); com T = int e int?.
	src := "struct P\n    x: int\nend\nstruct Caixa<T>\n    valor: T?\nend\nlet a: Caixa<P?> = Caixa(null)\nlet b: Caixa<int> = Caixa(null)\nlet c: Caixa<int> = Caixa(3)\n"
	c, err := compileFunctionSource(t, src)
	if err != nil {
		t.Fatalf("%v", err)
	}
	decl := c.structDeclaration("main::Caixa<P?>")
	if decl == nil {
		t.Fatalf("instance main::Caixa<P?> not registered")
	}
	if got := decl.Fields["valor"].String(); got != "P?" {
		t.Fatalf("valor: %s, want P?", got)
	}
}

func TestNullableStructNameResolvesInstances(t *testing.T) {
	src := "struct Caixa<T>\n    valor: T\nend\nlet a: Caixa<int>? = null\nlet b: Caixa<int>? = Caixa(1)\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatalf("%v", err)
	}
}
