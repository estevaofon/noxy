package vm

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

func setTestMap(mapping *value.ObjMap, key interface{}, item value.Value) {
	mapping.Set(key, item)
}

func requireTestMapValue(t *testing.T, mapping *value.ObjMap, key interface{}) value.Value {
	t.Helper()
	item, ok := mapping.Get(key)
	if !ok {
		t.Fatalf("map is missing key %v", key)
	}
	return item
}

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

func TestModuleSourceLayout(t *testing.T) {
	requireSourceFunctions(t, "modules.go", "loadModule")
}

func TestExecutorSourceLayout(t *testing.T) {
	t.Run("executor declarations", func(t *testing.T) {
		requireSourceFunctions(t, "executor.go", "Interpret", "InterpretWithGlobals", "InterpretWithEnvironment", "run")
	})

	t.Run("vm.go boundary", func(t *testing.T) {
		source, err := os.ReadFile("vm.go")
		if err != nil {
			t.Fatalf("read vm.go: %v", err)
		}
		lineCount := bytes.Count(source, []byte{'\n'})
		if len(source) > 0 && source[len(source)-1] != '\n' {
			lineCount++
		}
		if lineCount >= 400 {
			t.Errorf("vm.go has %d physical lines; want fewer than 400", lineCount)
		}

		file, err := parser.ParseFile(token.NewFileSet(), "vm.go", source, 0)
		if err != nil {
			t.Fatalf("parse vm.go: %v", err)
		}
		allowed := map[string]bool{
			"runtimeError":                        true,
			"IsNativeContext":                     true,
			"nativeVM":                            true,
			"New":                                 true,
			"NewWithConfig":                       true,
			"NewWithShared":                       true,
			"DefineNative":                        true,
			"DefineNativeWithSignature":           true,
			"DefineContextualNative":              true,
			"DefineContextualNativeWithSignature": true,
			"SetGlobal":                           true,
			"GetGlobal":                           true,
			"SetModule":                           true,
			"GetModule":                           true,
		}
		found := make(map[string]bool)
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok {
				found[function.Name.Name] = true
				if !allowed[function.Name.Name] {
					t.Errorf("vm.go unexpectedly declares %s", function.Name.Name)
				}
			}
		}
		for name := range allowed {
			if !found[name] {
				t.Errorf("vm.go does not declare %s", name)
			}
		}
	})
}

func TestRuntimeOwnershipDoesNotUseRawGlobalMaps(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	valueFiles, err := filepath.Glob(filepath.Join("..", "value", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, valueFiles...)

	for _, filename := range files {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.SelectorExpr:
				if typed.Sel.Name == "Globals" {
					t.Errorf("%s accesses obsolete Globals ownership", filename)
				}
			case *ast.KeyValueExpr:
				if key, ok := typed.Key.(*ast.Ident); ok && key.Name == "Globals" {
					t.Errorf("%s initializes obsolete Globals ownership", filename)
				}
			case *ast.Field:
				for _, name := range typed.Names {
					if name.Name == "Globals" {
						t.Errorf("%s declares obsolete Globals ownership", filename)
					}
					if name.Name == "GlobalOwner" {
						if pointer, ok := typed.Type.(*ast.StarExpr); ok {
							if _, rawMap := pointer.X.(*ast.MapType); rawMap {
								t.Errorf("%s declares GlobalOwner as a map pointer", filename)
							}
						}
					}
				}
			}
			return true
		})
	}
}

func TestRuntimeDoesNotAccessObjMapDataDirectly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "architecture_test.go" {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(source, []byte(".Dat"+"a")) {
			t.Errorf("%s accesses ObjMap.Dat"+"a directly", file)
		}
	}
}
