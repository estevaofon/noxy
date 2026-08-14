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

func selectorNamed(expression ast.Expr, name string) bool {
	for {
		switch typed := expression.(type) {
		case *ast.ParenExpr:
			expression = typed.X
		case *ast.SelectorExpr:
			return typed.Sel.Name == name
		default:
			return false
		}
	}
}

func expressionContainsSelector(expression ast.Expr, name string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == name {
			found = true
			return false
		}
		return !found
	})
	return found
}

func indexesSelector(expression ast.Expr, name string) bool {
	index, ok := expression.(*ast.IndexExpr)
	return ok && selectorNamed(index.X, name)
}

func isNilExpression(expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "nil"
}

func isEmptyValueLiteral(expression ast.Expr) bool {
	literal, ok := expression.(*ast.CompositeLit)
	return ok && len(literal.Elts) == 0 && expressionTextForArchitecture(literal.Type) == "value.Value"
}

func expressionTextForArchitecture(expression ast.Expr) string {
	var text strings.Builder
	_ = printer.Fprint(&text, token.NewFileSet(), expression)
	return text.String()
}

func forClearsStackFromStackBase(loop *ast.ForStmt) bool {
	initialization, ok := loop.Init.(*ast.AssignStmt)
	if !ok || len(initialization.Lhs) != len(initialization.Rhs) {
		return false
	}
	loopIndexes := make(map[string]bool)
	for index, left := range initialization.Lhs {
		identifier, ok := left.(*ast.Ident)
		if ok && expressionContainsSelector(initialization.Rhs[index], "StackBase") {
			loopIndexes[identifier.Name] = true
		}
	}
	if len(loopIndexes) == 0 {
		return false
	}

	clears := false
	ast.Inspect(loop.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != len(assignment.Rhs) {
			return !clears
		}
		for index, left := range assignment.Lhs {
			stackIndex, ok := left.(*ast.IndexExpr)
			if !ok {
				continue
			}
			identifier, indexOK := stackIndex.Index.(*ast.Ident)
			if indexOK && selectorNamed(stackIndex.X, "stack") && loopIndexes[identifier.Name] && isEmptyValueLiteral(assignment.Rhs[index]) {
				clears = true
				return false
			}
		}
		return !clears
	})
	return clears
}

func unwindTerminalMutationMatches(t *testing.T, filename string, source []byte) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filename, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}

	var matches []string
	record := func(node ast.Node, description string) {
		matches = append(matches, description+" at line "+fileSet.Position(node.Pos()).String())
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IncDecStmt:
			if typed.Tok == token.DEC && selectorNamed(typed.X, "frameCount") {
				record(typed, "decrements frameCount")
			}
		case *ast.AssignStmt:
			for index, left := range typed.Lhs {
				if selectorNamed(left, "frameCount") && typed.Tok == token.SUB_ASSIGN {
					record(typed, "decrements frameCount")
				}
				if index >= len(typed.Rhs) {
					continue
				}
				right := typed.Rhs[index]
				if selectorNamed(left, "frameCount") && typed.Tok == token.ASSIGN {
					binary, ok := right.(*ast.BinaryExpr)
					if ok && binary.Op == token.SUB && expressionContainsSelector(binary.X, "frameCount") {
						record(typed, "decrements frameCount")
					}
				}
				if indexesSelector(left, "frames") && isNilExpression(right) {
					record(typed, "removes a frame")
				}
				if selectorNamed(left, "currentFrame") && isNilExpression(right) {
					record(typed, "clears currentFrame")
				}
				if selectorNamed(left, "stackTop") && expressionContainsSelector(right, "StackBase") {
					record(typed, "resets stackTop to StackBase")
				}
			}
		case *ast.CallExpr:
			identifier, ok := typed.Fun.(*ast.Ident)
			if !ok || identifier.Name != "clear" || len(typed.Args) != 1 {
				break
			}
			slice, ok := typed.Args[0].(*ast.SliceExpr)
			if ok && selectorNamed(slice.X, "stack") && slice.Low != nil && expressionContainsSelector(slice.Low, "StackBase") {
				record(typed, "clears stack from StackBase")
			}
		case *ast.ForStmt:
			if forClearsStackFromStackBase(typed) {
				record(typed, "clears stack from StackBase")
			}
		}
		return true
	})
	return matches
}

func runtimeTargetTypeName(expression ast.Expr, aliases map[string]string) string {
	var name string
	switch typed := expression.(type) {
	case *ast.Ident:
		name = typed.Name
	case *ast.SelectorExpr:
		name = typed.Sel.Name
	case *ast.StarExpr:
		return runtimeTargetTypeName(typed.X, aliases)
	case *ast.ParenExpr:
		return runtimeTargetTypeName(typed.X, aliases)
	}
	for name != "" {
		if name == "ObjMap" || name == "ObjNative" {
			return name
		}
		next, ok := aliases[name]
		if !ok || next == name {
			break
		}
		name = next
	}
	return ""
}

func runtimeValueTargetType(expression ast.Expr, variables, aliases map[string]string) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return variables[typed.Name]
	case *ast.ParenExpr:
		return runtimeValueTargetType(typed.X, variables, aliases)
	case *ast.UnaryExpr:
		return runtimeValueTargetType(typed.X, variables, aliases)
	case *ast.TypeAssertExpr:
		return runtimeTargetTypeName(typed.Type, aliases)
	case *ast.CallExpr:
		return runtimeTargetTypeName(typed.Fun, aliases)
	}
	return ""
}

func recordRuntimeVariableField(field *ast.Field, variables, aliases map[string]string) {
	target := runtimeTargetTypeName(field.Type, aliases)
	if target == "" {
		return
	}
	for _, name := range field.Names {
		variables[name.Name] = target
	}
}

func recordRuntimeVariableSpec(specification *ast.ValueSpec, variables, aliases map[string]string) {
	target := runtimeTargetTypeName(specification.Type, aliases)
	for index, name := range specification.Names {
		actual := target
		if actual == "" && index < len(specification.Values) {
			actual = runtimeValueTargetType(specification.Values[index], variables, aliases)
		}
		if actual != "" {
			variables[name.Name] = actual
		}
	}
}

func recordRuntimeAssignment(assignment *ast.AssignStmt, variables, aliases map[string]string) {
	if len(assignment.Lhs) != len(assignment.Rhs) {
		return
	}
	for index, left := range assignment.Lhs {
		name, ok := left.(*ast.Ident)
		if !ok {
			continue
		}
		if target := runtimeValueTargetType(assignment.Rhs[index], variables, aliases); target != "" {
			variables[name.Name] = target
		}
	}
}

func runtimeForbiddenSourceMatches(t *testing.T, filename string, source []byte) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filename, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	found := make(map[string]bool)
	aliases := make(map[string]string)
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
			if target := runtimeTargetTypeName(typeSpec.Type, aliases); target != "" {
				aliases[typeSpec.Name.Name] = target
			}
		}
	}
	globalVariables := make(map[string]string)
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, specification := range general.Specs {
			if valueSpec, ok := specification.(*ast.ValueSpec); ok {
				recordRuntimeVariableSpec(valueSpec, globalVariables, aliases)
			}
		}
	}
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
		legacyDispatchReceiver := ""
		if function.Name.Name == "Invoke" && function.Recv != nil && len(function.Recv.List) == 1 {
			receiver := function.Recv.List[0]
			if len(receiver.Names) == 1 && expressionText(t, receiver.Type) == "*ObjNative" {
				legacyDispatchReceiver = receiver.Names[0].Name
			}
		}
		variables := make(map[string]string, len(globalVariables))
		for name, target := range globalVariables {
			variables[name] = target
		}
		if function.Recv != nil {
			for _, field := range function.Recv.List {
				recordRuntimeVariableField(field, variables, aliases)
			}
		}
		if function.Type.Params != nil {
			for _, field := range function.Type.Params.List {
				recordRuntimeVariableField(field, variables, aliases)
			}
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				recordRuntimeAssignment(typed, variables, aliases)
			case *ast.DeclStmt:
				general, ok := typed.Decl.(*ast.GenDecl)
				if ok && general.Tok == token.VAR {
					for _, specification := range general.Specs {
						if valueSpec, ok := specification.(*ast.ValueSpec); ok {
							recordRuntimeVariableSpec(valueSpec, variables, aliases)
						}
					}
				}
			case *ast.SelectorExpr:
				if typed.Sel.Name == "Data" && runtimeValueTargetType(typed.X, variables, aliases) == "ObjMap" {
					found["ObjMap.Data selector"] = true
				}
			case *ast.CallExpr:
				selector, ok := typed.Fun.(*ast.SelectorExpr)
				allowedLegacyDispatch := false
				if ok && legacyDispatchReceiver != "" {
					receiver, isIdentifier := selector.X.(*ast.Ident)
					allowedLegacyDispatch = isIdentifier && receiver.Name == legacyDispatchReceiver
				}
				if ok && selector.Sel.Name == "Fn" && !allowedLegacyDispatch && runtimeValueTargetType(selector.X, variables, aliases) == "ObjNative" {
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

func TestUnwindArchitectureCentralizesTerminalFrameTeardown(t *testing.T) {
	callFrame := sourceStructFields(t, "vm.go", "CallFrame")
	want := map[string]string{
		"StackBase": "int",
		"LocalBase": "int",
		"Deferred":  "[]PreparedCall",
	}
	for name, expectedType := range want {
		if got := callFrame[name]; got != expectedType {
			t.Errorf("CallFrame.%s=%q want %q", name, got, expectedType)
		}
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range files {
		if filename == "unwind.go" || strings.HasSuffix(filename, "_test.go") {
			continue
		}
		source, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range unwindTerminalMutationMatches(t, filename, source) {
			t.Errorf("terminal frame teardown must remain in unwind.go: %s", match)
		}
	}
}

func TestUnwindArchitectureMatcherUsesExactSyntax(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "decrement operator",
			source: `package vm
				type VM struct { frameCount int }
				func teardown(vm *VM) { vm.frameCount-- }`,
			want: []string{"decrements frameCount"},
		},
		{
			name: "subtract assignment",
			source: `package vm
				type VM struct { frameCount int }
				func teardown(vm *VM) { vm.frameCount -= 2 }`,
			want: []string{"decrements frameCount"},
		},
		{
			name: "explicit subtraction",
			source: `package vm
				type VM struct { frameCount int }
				func teardown(vm *VM) { vm.frameCount = vm.frameCount - 1 }`,
			want: []string{"decrements frameCount"},
		},
		{
			name: "nil frame assignment",
			source: `package vm
				type CallFrame struct{}
				type VM struct { frames []*CallFrame }
				func teardown(vm *VM, index int) { vm.frames[index] = nil }`,
			want: []string{"removes a frame"},
		},
		{
			name: "nil current frame assignment",
			source: `package vm
				type CallFrame struct{}
				type VM struct { currentFrame *CallFrame }
				func teardown(vm *VM) { vm.currentFrame = nil }`,
			want: []string{"clears currentFrame"},
		},
		{
			name: "stack clearing loop",
			source: `package vm
				import "noxy-vm/internal/value"
				type CallFrame struct { StackBase int }
				type VM struct { stack []value.Value }
				func teardown(vm *VM, frame *CallFrame, top int) {
					for index := frame.StackBase; index < top; index++ { vm.stack[index] = value.Value{} }
				}`,
			want: []string{"clears stack from StackBase"},
		},
		{
			name: "stack clear builtin",
			source: `package vm
				type CallFrame struct { StackBase int }
				type VM struct { stack []int }
				func teardown(vm *VM, frame *CallFrame, top int) { clear(vm.stack[frame.StackBase:top]) }`,
			want: []string{"clears stack from StackBase"},
		},
		{
			name: "stack top reset",
			source: `package vm
				type CallFrame struct { StackBase int }
				type VM struct { stackTop int }
				func teardown(vm *VM, frame *CallFrame) { vm.stackTop = frame.StackBase }`,
			want: []string{"resets stackTop to StackBase"},
		},
		{
			name: "non-clearing stack window loop is allowed",
			source: `package vm
				type CallFrame struct { StackBase int }
				func inspect(frame *CallFrame, top int) {
					for index := frame.StackBase; index < top; index++ { current := index; _ = current }
				}`,
		},
		{
			name: "frame construction and similar text are allowed",
			source: `package vm
				type PreparedCall struct{}
				type CallFrame struct { StackBase int; LocalBase int; Deferred []PreparedCall }
				type VM struct { frames []*CallFrame; frameCount int }
				var note = "vm.frameCount--; vm.frames[index] = nil"
				func install(vm *VM) {
					// clear(vm.stack[frame.StackBase:])
					frame := &CallFrame{StackBase: 0, LocalBase: 1}
					vm.frames[vm.frameCount] = frame
					vm.frameCount++
					_ = frame.StackBase
				}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matches := unwindTerminalMutationMatches(t, "architecture.go", []byte(test.source))
			got := make([]string, len(matches))
			for index, match := range matches {
				got[index] = strings.SplitN(match, " at line ", 2)[0]
			}
			if strings.Join(got, "|") != strings.Join(test.want, "|") {
				t.Fatalf("matches=%v want=%v", got, test.want)
			}
		})
	}
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
				type ObjMap struct { Data map[string]int }
				type ObjNative struct { Fn func([]int) int }
				type MapAlias = ObjMap
				type NativeAlias = ObjNative
				type VM struct {
					openFiles map[int]int
					netBufferedData map[int][]byte
					netBufferedConns map[int]int
				}
				func use(mapping *MapAlias, native *NativeAlias, args []int) {
					mappingAlias := mapping
					nativeAlias := native
					_ = mappingAlias.Data
					_ = nativeAlias.Fn(args)
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
			name:     "unrelated fields comments strings and longer names are allowed",
			filename: "allowed.go",
			source: `package vm
				var assertion = "openFiles netBufferedData DbHandles native.Fn(args) .Data"
				// Globals map[string]int; GlobalOwner *map[string]int
				type holder struct { Data int }
				type callable struct { Fn func([]int) int }
				type SharedState struct { Databases int }
				func use(item *holder, function *callable, args []int) {
					itemAlias := item
					functionAlias := function
					_ = itemAlias.Data
					_ = item.Dataset
					_ = functionAlias.Fn(args)
				}`,
		},
		{
			name:     "legacy dispatch is allowed only inside Invoke",
			filename: "native.go",
			source: `package value
				type ObjNative struct { Fn func([]int) int }
				func (native *ObjNative) Invoke(args []int) int { return native.Fn(args) }`,
		},
		{
			name:     "legacy dispatch rejects receiver aliases inside Invoke",
			filename: "native.go",
			source: `package value
				type ObjNative struct { Fn func([]int) int }
				func (native *ObjNative) Invoke(args []int) int {
					other := native
					_ = native.Fn(args)
					return other.Fn(args)
				}`,
			want: []string{"direct native.Fn invocation"},
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
