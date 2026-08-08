package vm

import (
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"testing"
)

func runTypedFunctionProgram(t *testing.T, input string) value.Value {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	bytecode, _, err := compiler.New().Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}
	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if err := machine.Interpret(bytecode); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	return captured
}

func TestExecutesExactHigherOrderFunction(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func add(a: int, b: int) -> int
    return a + b
end
func apply(f: func(int, int) -> int, a: int, b: int) -> int
    return f(a, b)
end
test_report(apply(add, 20, 22))`)
	testExpectedObject(t, 42, got)
}

func TestExecutesExactClosureReturn(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func makeAdder(base: int) -> func(int) -> int
    return func(value: int) -> int
        return base + value
    end
end
let add10: func(int) -> int = makeAdder(10)
test_report(add10(5))`)
	testExpectedObject(t, 15, got)
}

func TestExecutesExactReferenceArgument(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func increment(value: ref int) -> void
    *value = value + 1
end
let answer: int = 41
increment(answer)
test_report(answer)`)
	testExpectedObject(t, 42, got)
}
