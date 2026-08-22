package vm

import (
	"noxy-vm/internal/value"
	"strings"
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

func TestRuntimeConditionOnAnyMustBeBool(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "let a: any = 0\nif a then print(1) end\n")
	if err == nil || !strings.Contains(err.Error(), "condition must be bool, got int") {
		t.Fatalf("error=%v, want runtime bool check", err)
	}
	reported := captureVMSource(t, "let a: any = true\nlet n: int = 0\nif a then n = 1 end\ntest_report(n)\n")
	if reported.Int() != 1 {
		t.Fatalf("any bool condition should be taken, got %v", reported)
	}
}

func TestFStringBraceEscapesRender(t *testing.T) {
	// f-string de aspas simples porque o segmento `{"a": 2}` tem aspas duplas
	// (lexer nao e brace-aware; ver mesma nota em parser_test.go).
	reported := captureVMSource(t, "let x: int = 1\ntest_report(f'{{x}}|{{{x}}}|{ {\"a\": 2}[\"a\"] }|' + f'{\"a\"}')\n")
	if got := reported.Obj.(string); got != "{x}|{1}|2|a" {
		t.Fatalf("got %q", got)
	}
}

func runVmTests(t *testing.T, tests []vmTestCase) {
	for _, tt := range tests {
		testExpectedObject(t, tt.expected, captureVMSource(t, "test_report("+tt.input+")"))
	}
}

func testExpectedObject(t *testing.T, expected interface{}, actual value.Value) {
	switch expectedVal := expected.(type) {
	case int:
		if actual.Type != value.VAL_INT {
			t.Errorf("object is not Integer. got=%v (%+v)", actual.Type, actual)
			return
		}
		if int(actual.Int()) != expectedVal {
			t.Errorf("object has wrong value. got=%d, want=%d", actual.Int(), expectedVal)
		}
	case bool:
		if actual.Type != value.VAL_BOOL {
			t.Errorf("object is not Boolean. got=%v (%+v)", actual.Type, actual)
			return
		}
		if actual.Bool() != expectedVal {
			t.Errorf("object has wrong value. got=%t, want=%t", actual.Bool(), expectedVal)
		}
	}
}
