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
		{"invalid ref argument", "typed_function_invalid_ref_argument.nx", "argument 1 to 'increment': expected ref int, got int\n  hint: bind the value to a variable and pass 'ref <name>'"},
		{"ref read without star", "ref_read_without_star.nx", "operand of '+' cannot be ref int: a ref is never read implicitly\n  hint: use '*r' to read the referenced value"},
		{"ref for without star", "ref_for_without_star.nx", "cannot iterate over ref int[]: a ref is never read implicitly\n  hint: use 'for x in *r'"},
		{"ref builtin without ref", "ref_builtin_without_ref.nx", "argument 1 to 'append': expected ref T[], got int[]\n  hint: use 'ref xs'"},
		{"ref of ref", "ref_of_ref.nx", "'r' is already a reference\n  hint: pass 'r' directly, without 'ref'"},
		{"deref assign ref prefix", "deref_assign_ref_prefix.nx", "cannot assign ref int to int through '*r'\n  hint: use 'r = ref z' to rebind the reference, or '*r = z' to write the value"},
		{"nullable member without test", "nullable_member_without_test.nx", "'p' may be null; test it first\n  hint: use 'if p != null then ... end'"},
		{"nullable deref without test", "nullable_deref_without_test.nx", "'r' may be null; test it first\n  hint: use 'if r != null then ... end'"},
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
