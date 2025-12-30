package vm

import (
	"fmt"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"testing"
)

type vmTestCase struct {
	input    string
	expected interface{}
}

func TestIntegerArithmetic(t *testing.T) {
	tests := []vmTestCase{
		{"1", 1},
		{"2", 2},
		{"1 + 2", 3},
		{"1 - 2", -1},
		{"1 * 2", 2},
		{"4 / 2", 2},
		{"50 / 2 * 2 + 10", 60},
		{"2 * (5 + 10)", 30},
		{"3 * 3 * 3 + 10", 37},
		{"(5 + 10 * 2 + 15 / 3) * 2 + -10", 50},
	}

	runVmTests(t, tests)
}

func TestBooleanLogic(t *testing.T) {
	tests := []vmTestCase{
		{"true", true},
		{"false", false},
		{"1 < 2", true},
		{"1 > 2", false},
		{"1 < 1", false},
		{"1 > 1", false},
		{"1 == 1", true},
		{"1 != 1", false},
		{"1 == 2", false},
		{"1 != 2", true},
		{"true == true", true},
		{"false == false", true},
		{"true == false", false},
		{"true != false", true},
		{"(1 < 2) == true", true},
		{"(1 < 2) == false", false},
		{"(1 > 2) == true", false},
		{"(1 > 2) == false", true},
	}

	runVmTests(t, tests)
}

func runVmTests(t *testing.T, tests []vmTestCase) {
	for _, tt := range tests {
		// Wrap input in test_report call
		input := fmt.Sprintf("test_report(%s)", tt.input)

		l := lexer.New(input)
		p := parser.New(l)
		program := p.ParseProgram()
		if len(p.Errors()) > 0 {
			t.Fatalf("parser errors: %v", p.Errors())
		}

		c := compiler.New()
		bytecode, err := c.Compile(program)
		if err != nil {
			t.Fatalf("compiler error: %s", err)
		}

		vm := New()

		// Capture result
		var captured value.Value = value.NewNull()

		// Define native before running
		vm.defineNative("test_report", func(args []value.Value) value.Value {
			if len(args) > 0 {
				captured = args[0]
			}
			return value.NewNull()
		})

		err = vm.Interpret(bytecode)
		if err != nil {
			t.Fatalf("vm error: %s", err)
		}

		testExpectedObject(t, tt.expected, captured)
	}
}

func testExpectedObject(t *testing.T, expected interface{}, actual value.Value) {
	switch expectedVal := expected.(type) {
	case int:
		if actual.Type != value.VAL_INT {
			t.Errorf("object is not Integer. got=%v (%+v)", actual.Type, actual)
			return
		}
		if int(actual.AsInt) != expectedVal {
			t.Errorf("object has wrong value. got=%d, want=%d", actual.AsInt, expectedVal)
		}
	case bool:
		if actual.Type != value.VAL_BOOL {
			t.Errorf("object is not Boolean. got=%v (%+v)", actual.Type, actual)
			return
		}
		if actual.AsBool != expectedVal {
			t.Errorf("object has wrong value. got=%t, want=%t", actual.AsBool, expectedVal)
		}
	}
}
