package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readConformanceFixture(t *testing.T, parts ...string) string {
	t.Helper()
	pathParts := append([]string{"..", "..", "noxy_examples"}, parts...)
	content, err := os.ReadFile(filepath.Join(pathParts...))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestTypedFunctionValidConformanceExampleCompiles(t *testing.T) {
	input := readConformanceFixture(t, "typed_function_conformance.nx")
	if _, err := compileFunctionSource(t, input); err != nil {
		t.Fatal(err)
	}
}

func TestConformanceDiagnosticMatcherRejectsLongerFunctionType(t *testing.T) {
	err := fmt.Errorf("[line 7] expected func(int) -> int, got func(string) -> int")
	if conformanceDiagnosticMatches(err, "expected func(int) -> int, got func") {
		t.Fatal("bare func diagnostic must not match a longer exact function type")
	}
}

func conformanceDiagnosticMatches(err error, want string) bool {
	return err != nil && strings.HasSuffix(err.Error(), want)
}

func TestTypedFunctionInvalidConformanceExamplesFail(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{"wrong arity", "typed_function_wrong_arity.nx", "expects 2 arguments, got 1"},
		{"wrong argument", "typed_function_wrong_argument.nx", "argument 1 to 'add': expected int, got string"},
		{"wrong return", "typed_function_wrong_return.nx", "return type mismatch in 'invalid': expected int, got string"},
		{"incompatible assignment", "typed_function_incompatible_assignment.nx", "expected func(int) -> int, got func(string) -> int"},
		{"dynamic narrowing", "typed_function_dynamic_narrowing.nx", "expected func(int) -> int, got func"},
		{"invalid ref argument", "typed_function_invalid_ref_argument.nx", "reference argument '41' is not addressable\n  hint: use a variable, property, index, or null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := readConformanceFixture(t, "type_errors", tt.file)
			_, err := compileFunctionSource(t, input)
			if !conformanceDiagnosticMatches(err, tt.want) {
				t.Fatalf("error=%v, want diagnostic containing %q", err, tt.want)
			}
		})
	}
}
