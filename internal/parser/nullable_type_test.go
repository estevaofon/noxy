package parser

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
)

// Spec §2.4 (issue #105): `T?` e o unico sufixo de nulidade; e o pos-fixo
// mais externo — `ref Node?` e referencia anulavel, `ref (Node?)` referencia
// a slot anulavel; sem ref, `?` e `[]` alternam livremente.

func parseTypeOf(t *testing.T, src string) (ast.NoxyType, []string) {
	t.Helper()
	p := New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return nil, p.Errors()
	}
	if len(program.Statements) == 0 {
		t.Fatalf("%s: no statements", src)
	}
	switch s := program.Statements[0].(type) {
	case *ast.LetStmt:
		return s.Type, nil
	case *ast.FunctionStatement:
		return s.ReturnType, nil
	}
	t.Fatalf("%s: unexpected statement %T", src, program.Statements[0])
	return nil, nil
}

func TestNullableTypeSyntax(t *testing.T) {
	cases := []struct{ src, want string }{
		{"let a: Node? = null", "Node?"},
		{"let b: ref Node? = null", "ref Node?"},
		{"let c: ref (Node?) = ref x", "ref (Node?)"},
		{"let d: Node?[] = []", "Node?[]"},
		{"let e: int[]? = null", "int[]?"},
		{"let f: (func(int) -> int)? = null", "(func(int) -> int)?"},
		{"let g: map[string, Node?] = {}", "map[string, Node?]"},
		{"let h: ref int[]? = null", "ref int[]?"},
		{"func busca(k: int) -> Node?\n    return null\nend", "Node?"},
	}
	for _, tc := range cases {
		got, errs := parseTypeOf(t, tc.src)
		if len(errs) > 0 {
			t.Errorf("%s: parse errors %v", tc.src, errs)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("%s: type %q, want %q", tc.src, got.String(), tc.want)
		}
	}
	// Estrutura, nao so texto: `ref Node?` e Nullable(Ref), `ref (Node?)` e Ref(Nullable).
	if typ, _ := parseTypeOf(t, "let b: ref Node? = null"); typ != nil {
		if _, ok := typ.(*ast.NullableType); !ok {
			t.Errorf("ref Node? must be NullableType at the top, got %T", typ)
		}
	}
	if typ, _ := parseTypeOf(t, "let c: ref (Node?) = ref x"); typ != nil {
		if _, ok := typ.(*ast.RefType); !ok {
			t.Errorf("ref (Node?) must be RefType at the top, got %T", typ)
		}
	}
}

func TestNullableTypeErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"let a: Node?? = null", "type is already nullable"},
		{"let b: any? = null", "'any' already admits null"},
		{"func f() -> void?\nend", "'void' cannot be nullable"},
	}
	for _, tc := range cases {
		_, errs := parseTypeOf(t, tc.src)
		if len(errs) == 0 || !strings.Contains(errs[0], tc.want) {
			t.Errorf("%s: want %q, got %v", tc.src, tc.want, errs)
		}
	}
}
