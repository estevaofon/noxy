package compiler

import (
	"strings"
	"testing"
)

// Um campo tipado `ns.T` que NAO resolve e erro de COMPILACAO no struct, com
// hint — nunca o "struct constructor has incomplete runtime type metadata"
// incondicional de runtime, que nao diz o que fazer.
func TestQualifiedStructFieldNamingUnknownModuleStructIsCompileError(t *testing.T) {
	_, err := compileFunctionSource(t, "use io\nstruct A\n    f: io.Nope\nend\n")
	if err == nil {
		t.Fatal("compiler accepted a struct field typed with a struct the module does not export")
	}
	for _, want := range []string{"[line 2]", "struct 'A' field 'f'", "cannot resolve type 'io.Nope'", "module 'io' has no struct 'Nope'", "hint:"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err.Error(), want)
		}
	}
}

func TestQualifiedStructFieldWithoutNamespaceImportIsCompileError(t *testing.T) {
	_, err := compileFunctionSource(t, "struct A\n    f: foo.Bar\nend\n")
	if err == nil {
		t.Fatal("compiler accepted a qualified field type whose namespace was never imported")
	}
	for _, want := range []string{"[line 1]", "struct 'A' field 'f'", "cannot resolve type 'foo.Bar'", "'foo' is not an imported module", "hint:", "use foo"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err.Error(), want)
		}
	}
}
