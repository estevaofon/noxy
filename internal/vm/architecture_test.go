package vm

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
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

func productionGoFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir("..", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func expressionText(t *testing.T, expression ast.Expr) string {
	t.Helper()
	var text strings.Builder
	if err := printer.Fprint(&text, token.NewFileSet(), expression); err != nil {
		t.Fatal(err)
	}
	return text.String()
}

func sourceStructFields(t *testing.T, filename, structName string) map[string]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	fields := make(map[string]string)
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != structName {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s.%s is not a struct", filename, structName)
			}
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					fields[name.Name] = expressionText(t, field.Type)
				}
			}
		}
	}
	if len(fields) == 0 {
		t.Fatalf("%s does not declare struct %s", filename, structName)
	}
	return fields
}

func runtimeForbiddenSourceMatches(t *testing.T, filename string, source []byte) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	found := make(map[string]bool)
	for _, declaration := range file.Decls {
		if general, ok := declaration.(*ast.GenDecl); ok {
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structure, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range structure.Fields.List {
					_, rawMap := field.Type.(*ast.MapType)
					pointer, pointerType := field.Type.(*ast.StarExpr)
					rawMapPointer := false
					if pointerType {
						_, rawMapPointer = pointer.X.(*ast.MapType)
					}
					for _, name := range field.Names {
						switch {
						case typeSpec.Name.Name == "VM" && name.Name == "openFiles":
							found["VM.openFiles field"] = true
						case typeSpec.Name.Name == "VM" && name.Name == "netBufferedData":
							found["VM.netBufferedData field"] = true
						case typeSpec.Name.Name == "VM" && name.Name == "netBufferedConns":
							found["VM.netBufferedConns field"] = true
						case name.Name == "Globals" && rawMap:
							found["Globals raw map field"] = true
						case name.Name == "GlobalOwner" && pointerType && rawMapPointer:
							found["GlobalOwner raw map pointer field"] = true
						case name.Name == "DbHandles" && rawMap:
							found["DbHandles raw map field"] = true
						case name.Name == "StmtHandles" && rawMap:
							found["StmtHandles raw map field"] = true
						case name.Name == "StmtParams" && rawMap:
							found["StmtParams raw map field"] = true
						}
					}
				}
			}
		}
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		allowedLegacyDispatch := function.Name.Name == "Invoke" && function.Recv != nil && expressionText(t, function.Recv.List[0].Type) == "*ObjNative"
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.SelectorExpr:
				if typed.Sel.Name == "Data" {
					found["ObjMap.Data selector"] = true
				}
			case *ast.CallExpr:
				selector, ok := typed.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "Fn" && !allowedLegacyDispatch {
					found["direct native.Fn invocation"] = true
				}
			}
			return true
		})
	}
	order := []string{
		"ObjMap.Data selector",
		"VM.openFiles field",
		"VM.netBufferedData field",
		"VM.netBufferedConns field",
		"direct native.Fn invocation",
		"Globals raw map field",
		"GlobalOwner raw map pointer field",
		"DbHandles raw map field",
		"StmtHandles raw map field",
		"StmtParams raw map field",
	}
	var matches []string
	for _, issue := range order {
		if found[issue] {
			matches = append(matches, issue)
		}
	}
	return matches
}

func TestRuntimeFoundationExcludesObsoleteProductionStructures(t *testing.T) {
	for _, filename := range productionGoFiles(t) {
		source, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		for _, issue := range runtimeForbiddenSourceMatches(t, filename, source) {
			t.Errorf("%s: %s", filename, issue)
		}
	}
}

func TestRuntimeFoundationRequiresContextAndEnvironmentOwnership(t *testing.T) {
	callFrame := sourceStructFields(t, "vm.go", "CallFrame")
	if got := callFrame["Environment"]; got != "*value.GlobalEnvironment" {
		t.Errorf("CallFrame.Environment=%q", got)
	}
	for _, structName := range []string{"ObjFunction", "ObjClosure"} {
		fields := sourceStructFields(t, filepath.Join("..", "value", "value.go"), structName)
		if got := fields["Environment"]; got != "*GlobalEnvironment" {
			t.Errorf("%s.Environment=%q", structName, got)
		}
	}
	reference := sourceStructFields(t, filepath.Join("..", "value", "value.go"), "ObjRef")
	if got := reference["GlobalOwner"]; got != "*GlobalEnvironment" {
		t.Errorf("ObjRef.GlobalOwner=%q", got)
	}

	calls, err := os.ReadFile("calls.go")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), "calls.go", calls, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundInvoke := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		receiver, receiverOK := selector.X.(*ast.Ident)
		activeContext, contextOK := call.Args[0].(*ast.Ident)
		if receiverOK && contextOK && selector.Sel.Name == "Invoke" && receiver.Name == "native" && activeContext.Name == "vm" {
			foundInvoke = true
		}
		return true
	})
	if !foundInvoke {
		t.Error("calls.go does not dispatch natives through Invoke")
	}
}

func TestResourceRegistriesAndModuleCacheHaveSharedOwners(t *testing.T) {
	shared := sourceStructFields(t, "vm.go", "SharedState")
	wantRegistries := map[string]string{
		"Files":      "*handleRegistry[*FileResource]",
		"Listeners":  "*handleRegistry[*ListenerResource]",
		"Sockets":    "*handleRegistry[*SocketResource]",
		"Databases":  "*handleRegistry[*DatabaseResource]",
		"Statements": "*handleRegistry[*StatementResource]",
	}
	for name, want := range wantRegistries {
		if got := shared[name]; got != want {
			t.Errorf("SharedState.%s=%q want %q", name, got, want)
		}
	}
	if got := shared["Modules"]; got != "*moduleCache" {
		t.Errorf("SharedState.Modules=%q", got)
	}

	for _, filename := range productionGoFiles(t) {
		if filepath.Dir(filename) != "." && filepath.Clean(filepath.Dir(filename)) != filepath.Clean("../vm") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if typeSpec.Name.Name == "moduleCache" && filepath.Base(filename) != "module_cache.go" {
					t.Errorf("moduleCache declared outside module_cache.go: %s", filename)
				}
				structure, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range structure.Fields.List {
					if strings.Contains(expressionText(t, field.Type), "handleRegistry[") && typeSpec.Name.Name != "SharedState" {
						t.Errorf("resource registry declared on %s in %s", typeSpec.Name.Name, filename)
					}
				}
			}
		}
	}
	requireSourceFunctions(t, "module_cache.go", "newModuleCache")
}

func TestRuntimeForbiddenSourceMatchesExactSyntax(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		source   string
		want     []string
	}{
		{
			name:     "forbidden selectors and VM fields",
			filename: "runtime.go",
			source: `package vm
				type VM struct {
					openFiles map[int]int
					netBufferedData map[int][]byte
					netBufferedConns map[int]int
				}
				func use(mapping *holder, native *callable, args []int) {
					_ = mapping.Data
					_ = native.Fn(args)
				}`,
			want: []string{"ObjMap.Data selector", "VM.openFiles field", "VM.netBufferedData field", "VM.netBufferedConns field", "direct native.Fn invocation"},
		},
		{
			name:     "forbidden raw ownership fields",
			filename: "ownership.go",
			source: `package vm
				type owner struct {
					Globals map[string]int
					GlobalOwner *map[string]int
					DbHandles map[int]int
					StmtHandles map[int]int
					StmtParams map[int]map[int]int
				}`,
			want: []string{"Globals raw map field", "GlobalOwner raw map pointer field", "DbHandles raw map field", "StmtHandles raw map field", "StmtParams raw map field"},
		},
		{
			name:     "comments strings and longer names are allowed",
			filename: "allowed.go",
			source: `package vm
				var assertion = "openFiles netBufferedData DbHandles native.Fn(args) .Data"
				// Globals map[string]int; GlobalOwner *map[string]int
				type SharedState struct { Databases int }
				func use(item *holder) { _ = item.Dataset }`,
		},
		{
			name:     "legacy dispatch is allowed only inside Invoke",
			filename: "native.go",
			source: `package value
				type ObjNative struct { Fn func([]int) int }
				func (native *ObjNative) Invoke(args []int) int { return native.Fn(args) }`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runtimeForbiddenSourceMatches(t, test.filename, []byte(test.source))
			if strings.Join(got, "|") != strings.Join(test.want, "|") {
				t.Fatalf("matches=%v want=%v", got, test.want)
			}
		})
	}
}
