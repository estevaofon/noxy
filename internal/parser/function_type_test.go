package parser

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"testing"
)

func TestParseFunctionTypes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"let f: func(int, int) -> int", "func(int, int) -> int"},
		{"let f: func(ref int) -> void", "func(ref int) -> void"},
		{"let f: (func(int) -> int)[]", "(func(int) -> int)[]"},
		{"let f: map[string, func(string) -> bool]", "map[string, func(string) -> bool]"},
		{"let f: chan func(int) -> int", "chan func(int) -> int"},
		{"let f: ref func(int) -> int", "ref func(int) -> int"},
		{"let f: func() -> func(int) -> int", "func() -> func(int) -> int"},
		{"let f: func", "func"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			l := lexer.New(tt.input)
			p := New(l)
			program := p.ParseProgram()
			checkParserErrors(t, p)
			stmt := program.Statements[0].(*ast.LetStmt)
			if got := stmt.Type.String(); got != tt.want {
				t.Fatalf("type=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestRejectMalformedFunctionTypes(t *testing.T) {
	inputs := []string{
		"let f: func(int)",
		"let f: func(unknown-token) -> int",
		"let f: func(int,) -> int",
	}
	for _, input := range inputs {
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		if len(p.Errors()) == 0 {
			t.Fatalf("expected parser error for %q", input)
		}
	}
}

func TestVoidIsRestrictedToFunctionReturns(t *testing.T) {
	invalid := []string{
		"let value: void = null",
		"func invalid(value: void) end",
		"let callback: func(void) -> int",
		"let values: void[] = []",
		"func invalid() -> void[] end",
		"struct Invalid\n    value: void\nend",
	}
	for _, input := range invalid {
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		if len(p.Errors()) == 0 {
			t.Fatalf("expected parser error for %q", input)
		}
	}

	valid := []string{
		"func valid() -> void\nend",
		"let callback: func(int) -> void",
		"func factory() -> func(int) -> void\n    return func(value: int) -> void\n    end\nend",
	}
	for _, input := range valid {
		l := lexer.New(input)
		p := New(l)
		p.ParseProgram()
		checkParserErrors(t, p)
	}
}
