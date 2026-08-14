package compiler

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"strings"
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

func compiledFunction(t *testing.T, source, name string) *value.ObjFunction {
	t.Helper()
	code, _, err := New().Compile(parse(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, constant := range code.Constants {
		if constant.Type == value.VAL_FUNCTION {
			fn := constant.Obj.(*value.ObjFunction)
			if fn.Name == name {
				return fn
			}
		}
	}
	t.Fatalf("function %q not found", name)
	return nil
}

func containsOpcode(code []byte, opcode chunk.OpCode) bool {
	for _, instruction := range code {
		if chunk.OpCode(instruction) == opcode {
			return true
		}
	}
	return false
}

func TestCompileDeferEmitsArgCount(t *testing.T) {
	fn := compiledFunction(t, `
func cleanup(value: int) -> void
end
func run() -> void
    defer cleanup(7)
end`, "run")
	code := fn.Chunk.(*chunk.Chunk).Code
	for index, instruction := range code {
		if chunk.OpCode(instruction) == chunk.OP_DEFER {
			if index+1 >= len(code) || code[index+1] != 1 {
				t.Fatalf("bad OP_DEFER operand")
			}
			return
		}
	}
	t.Fatal("compiled function omitted OP_DEFER")
}

func TestCompileDeferRejectsAddrPseudoCall(t *testing.T) {
	_, _, err := New().Compile(parse("let value: int = 1\ndefer addr(ref value)\n"))
	if err == nil || !strings.Contains(err.Error(), "cannot defer addr") {
		t.Fatalf("error=%v", err)
	}
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
		_, _, err := c.Compile(program)
		if err != nil {
			t.Fatalf("compiler error for input %q: %s", tt.input, err)
		}
	}
}
