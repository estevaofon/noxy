package compiler

import (
	"os"
	"path/filepath"
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
