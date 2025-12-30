package compiler

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"testing"
)

type compilerTestCase struct {
	input string
}

func TestCompilerSmoke(t *testing.T) {
	tests := []compilerTestCase{
		{"1 + 2"},
		// Note: More complex constructs are tested via vm_test.go which ensures
		// both compilation and execution are correct. This test acts as a basic
		// smoke test for the compiler infrastructure.
	}

	runCompilerTests(t, tests)
}

func parse(input string) *ast.Program {
	l := lexer.New(input)
	p := parser.New(l)
	return p.ParseProgram()
}

func runCompilerTests(t *testing.T, tests []compilerTestCase) {
	for _, tt := range tests {
		t.Logf("Compiling: %s", tt.input)
		program := parse(tt.input)
		if len(program.Statements) > 0 {
			stmt := program.Statements[0]
			if letStmt, ok := stmt.(*ast.LetStmt); ok {
				t.Logf("LetStmt Name: %v", letStmt.Name)
			}
		}
		c := New()
		_, err := c.Compile(program)
		if err != nil {
			t.Fatalf("compiler error for input %q: %s", tt.input, err)
		}
	}
}
