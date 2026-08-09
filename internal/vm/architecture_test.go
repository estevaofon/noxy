package vm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func requireSourceFunctions(t *testing.T, filename string, names ...string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	found := make(map[string]bool)
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			found[function.Name.Name] = true
		}
	}
	for _, name := range names {
		if !found[name] {
			t.Errorf("%s does not declare %s", filename, name)
		}
	}
}

func TestBuiltinSourceLayout(t *testing.T) {
	expected := map[string][]string{
		"builtins.go":             {"defineBuiltins"},
		"builtins_core.go":        {"defineCoreBuiltins"},
		"builtins_collections.go": {"defineCollectionBuiltins"},
		"builtins_concurrency.go": {"defineConcurrencyBuiltins"},
		"builtins_time.go":        {"defineTimeBuiltins"},
		"builtins_strings.go":     {"defineStringBuiltins"},
		"builtins_io.go":          {"defineIOBuiltins"},
		"builtins_sys.go":         {"defineSystemBuiltins"},
		"builtins_crypto.go":      {"defineCryptoBuiltins"},
		"builtins_net.go":         {"defineNetworkBuiltins"},
		"builtins_sqlite.go":      {"defineSQLiteBuiltins"},
		"builtins_json.go":        {"defineJSONBuiltins"},
	}
	for filename, names := range expected {
		requireSourceFunctions(t, filename, names...)
	}
}

func TestStackAndCallSourceLayout(t *testing.T) {
	expected := map[string][]string{
		"stack.go": {"readShort", "isFalsey", "valuesEqual", "readConstant", "push", "pop", "peek", "captureUpvalue", "closeUpvalue"},
		"calls.go": {"callValue", "call", "copyValue"},
	}
	for filename, names := range expected {
		requireSourceFunctions(t, filename, names...)
	}
}
