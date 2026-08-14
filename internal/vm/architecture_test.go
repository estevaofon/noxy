package vm

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
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

type architectureTypeInfo struct {
	aliases   map[string]string
	fields    map[string]map[string]string
	functions map[string]string
	known     map[string]bool
	globals   map[string]string
}

type architectureTypeResolver struct {
	info      *architectureTypeInfo
	variables map[string]string
}

func newArchitectureTypeInfo(file *ast.File) *architectureTypeInfo {
	info := &architectureTypeInfo{
		aliases: map[string]string{},
		fields: map[string]map[string]string{
			"VM": {
				"frameCount":   "int",
				"frames":       "CallFrame",
				"currentFrame": "CallFrame",
				"stack":        "Value",
				"stackTop":     "int",
			},
			"CallFrame": {
				"StackBase": "int",
			},
		},
		functions: map[string]string{
			"New":           "VM",
			"NewWithConfig": "VM",
			"NewWithShared": "VM",
		},
		known: map[string]bool{
			"VM":        true,
			"CallFrame": true,
		},
		globals: map[string]string{},
	}

	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok {
				continue
			}
			info.known[typeSpec.Name.Name] = true
			if typeSpec.Assign.IsValid() {
				if target := architectureDeclaredTypeName(typeSpec.Type, nil); target != "" {
					info.aliases[typeSpec.Name.Name] = target
				}
			}
		}
	}

	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.GenDecl:
			for _, specification := range typed.Specs {
				switch spec := specification.(type) {
				case *ast.TypeSpec:
					structure, ok := spec.Type.(*ast.StructType)
					if !ok {
						continue
					}
					fields := info.fields[spec.Name.Name]
					if fields == nil {
						fields = make(map[string]string)
						info.fields[spec.Name.Name] = fields
					}
					for _, field := range structure.Fields.List {
						fieldType := architectureDeclaredTypeName(field.Type, info.aliases)
						for _, name := range field.Names {
							fields[name.Name] = fieldType
						}
					}
				case *ast.ValueSpec:
					if typed.Tok == token.VAR {
						info.recordValueSpec(spec, info.globals)
					}
				}
			}
		case *ast.FuncDecl:
			if typed.Type.Results != nil && len(typed.Type.Results.List) == 1 {
				if result := architectureDeclaredTypeName(typed.Type.Results.List[0].Type, info.aliases); result != "" {
					info.functions[typed.Name.Name] = result
				}
			}
		}
	}
	return info
}

func architectureDeclaredTypeName(expression ast.Expr, aliases map[string]string) string {
	var name string
	switch typed := expression.(type) {
	case *ast.Ident:
		name = typed.Name
	case *ast.SelectorExpr:
		name = typed.Sel.Name
	case *ast.StarExpr:
		return architectureDeclaredTypeName(typed.X, aliases)
	case *ast.ParenExpr:
		return architectureDeclaredTypeName(typed.X, aliases)
	case *ast.ArrayType:
		return architectureDeclaredTypeName(typed.Elt, aliases)
	case *ast.Ellipsis:
		return architectureDeclaredTypeName(typed.Elt, aliases)
	}
	for aliases != nil && name != "" {
		next, ok := aliases[name]
		if !ok || next == name {
			break
		}
		name = next
	}
	return name
}

func (info *architectureTypeInfo) recordValueSpec(spec *ast.ValueSpec, variables map[string]string) {
	declared := architectureDeclaredTypeName(spec.Type, info.aliases)
	resolver := &architectureTypeResolver{info: info, variables: variables}
	for index, name := range spec.Names {
		actual := declared
		if actual == "" && index < len(spec.Values) {
			actual = resolver.expressionType(spec.Values[index])
		}
		if actual != "" {
			variables[name.Name] = actual
		}
	}
}

func newArchitectureTypeResolver(info *architectureTypeInfo, function *ast.FuncDecl) *architectureTypeResolver {
	variables := make(map[string]string, len(info.globals))
	for name, target := range info.globals {
		variables[name] = target
	}
	resolver := &architectureTypeResolver{info: info, variables: variables}
	resolver.recordFieldList(function.Recv)
	resolver.recordFieldList(function.Type.Params)
	return resolver
}

func (resolver *architectureTypeResolver) clone() *architectureTypeResolver {
	variables := make(map[string]string, len(resolver.variables))
	for name, target := range resolver.variables {
		variables[name] = target
	}
	return &architectureTypeResolver{info: resolver.info, variables: variables}
}

func (resolver *architectureTypeResolver) recordFieldList(fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		target := architectureDeclaredTypeName(field.Type, resolver.info.aliases)
		for _, name := range field.Names {
			resolver.variables[name.Name] = target
		}
	}
}

func (resolver *architectureTypeResolver) expressionType(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return resolver.variables[typed.Name]
	case *ast.ParenExpr:
		return resolver.expressionType(typed.X)
	case *ast.UnaryExpr:
		return resolver.expressionType(typed.X)
	case *ast.CompositeLit:
		return architectureDeclaredTypeName(typed.Type, resolver.info.aliases)
	case *ast.SelectorExpr:
		owner := resolver.expressionType(typed.X)
		return resolver.info.fields[owner][typed.Sel.Name]
	case *ast.IndexExpr:
		return resolver.expressionType(typed.X)
	case *ast.CallExpr:
		if identifier, ok := typed.Fun.(*ast.Ident); ok {
			if result := resolver.info.functions[identifier.Name]; result != "" {
				return result
			}
			if identifier.Name == "new" && len(typed.Args) == 1 {
				target := architectureDeclaredTypeName(typed.Args[0], resolver.info.aliases)
				if resolver.info.known[target] {
					return target
				}
			}
		}
		converted := architectureDeclaredTypeName(typed.Fun, resolver.info.aliases)
		if resolver.info.known[converted] {
			return converted
		}
	}
	return ""
}

func (resolver *architectureTypeResolver) recordAssignment(assignment *ast.AssignStmt) {
	if len(assignment.Lhs) != len(assignment.Rhs) {
		return
	}
	for index, left := range assignment.Lhs {
		identifier, ok := left.(*ast.Ident)
		if !ok {
			continue
		}
		if target := resolver.expressionType(assignment.Rhs[index]); target != "" {
			resolver.variables[identifier.Name] = target
		}
	}
}

func (resolver *architectureTypeResolver) isField(expression ast.Expr, ownerType, fieldName string) bool {
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = parenthesized.X
	}
	selector, ok := expression.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == fieldName && resolver.expressionType(selector.X) == ownerType
}

func (resolver *architectureTypeResolver) containsField(expression ast.Expr, ownerType, fieldName string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && resolver.isField(selector, ownerType, fieldName) {
			found = true
			return false
		}
		return !found
	})
	return found
}

func (resolver *architectureTypeResolver) indexesField(expression ast.Expr, ownerType, fieldName string) bool {
	index, ok := expression.(*ast.IndexExpr)
	return ok && resolver.isField(index.X, ownerType, fieldName)
}

func (resolver *architectureTypeResolver) isNilForType(expression ast.Expr, target string) bool {
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name == "nil"
	}
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	identifier, ok := call.Args[0].(*ast.Ident)
	return ok && identifier.Name == "nil" && architectureDeclaredTypeName(call.Fun, resolver.info.aliases) == target
}

func isZeroInteger(expression ast.Expr) bool {
	if parenthesized, ok := expression.(*ast.ParenExpr); ok {
		return isZeroInteger(parenthesized.X)
	}
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.INT {
		return false
	}
	value, err := strconv.ParseInt(literal.Value, 0, 64)
	return err == nil && value == 0
}

func isEmptyCompositeLiteral(expression ast.Expr) bool {
	literal, ok := expression.(*ast.CompositeLit)
	return ok && len(literal.Elts) == 0
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

func forClearsStackFromStackBase(loop *ast.ForStmt, resolver *architectureTypeResolver) bool {
	initialization, ok := loop.Init.(*ast.AssignStmt)
	if !ok || len(initialization.Lhs) != len(initialization.Rhs) {
		return false
	}
	loopIndexes := make(map[string]bool)
	for index, left := range initialization.Lhs {
		identifier, ok := left.(*ast.Ident)
		if ok && resolver.containsField(initialization.Rhs[index], "CallFrame", "StackBase") {
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
			if indexOK && resolver.isField(stackIndex.X, "VM", "stack") && loopIndexes[identifier.Name] && isEmptyValueLiteral(assignment.Rhs[index]) {
				clears = true
				return false
			}
		}
		return !clears
	})
	return clears
}

func architectureIntroducesScope(node ast.Node) bool {
	switch node.(type) {
	case *ast.BlockStmt, *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt,
		*ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt,
		*ast.CaseClause, *ast.CommClause, *ast.FuncLit:
		return true
	}
	return false
}

func unwindTerminalMutationMatches(t *testing.T, filename string, source []byte) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filename, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}

	info := newArchitectureTypeInfo(file)
	var matches []string
	record := func(node ast.Node, description string) {
		matches = append(matches, description+" at line "+fileSet.Position(node.Pos()).String())
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		resolvers := []*architectureTypeResolver{newArchitectureTypeResolver(info, function)}
		var nodes []ast.Node
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if node == nil {
				last := nodes[len(nodes)-1]
				nodes = nodes[:len(nodes)-1]
				if architectureIntroducesScope(last) {
					resolvers = resolvers[:len(resolvers)-1]
				}
				return true
			}
			nodes = append(nodes, node)
			if architectureIntroducesScope(node) {
				nested := resolvers[len(resolvers)-1].clone()
				if literal, ok := node.(*ast.FuncLit); ok {
					nested.recordFieldList(literal.Type.Params)
				}
				resolvers = append(resolvers, nested)
			}
			resolver := resolvers[len(resolvers)-1]
			switch typed := node.(type) {
			case *ast.IncDecStmt:
				if typed.Tok == token.DEC && resolver.isField(typed.X, "VM", "frameCount") {
					record(typed, "decrements frameCount")
				}
			case *ast.AssignStmt:
				for index, left := range typed.Lhs {
					frameCount := resolver.isField(left, "VM", "frameCount")
					if frameCount && typed.Tok == token.SUB_ASSIGN {
						record(typed, "decrements frameCount")
					}
					if index >= len(typed.Rhs) {
						continue
					}
					right := typed.Rhs[index]
					if frameCount && typed.Tok == token.ASSIGN {
						binary, subtraction := right.(*ast.BinaryExpr)
						if subtraction && binary.Op == token.SUB && resolver.containsField(binary.X, "VM", "frameCount") {
							record(typed, "decrements frameCount")
						} else if isZeroInteger(right) {
							record(typed, "resets frameCount")
						}
					}
					if resolver.isField(left, "VM", "frames") && isEmptyCompositeLiteral(right) {
						record(typed, "resets frames")
					}
					if resolver.indexesField(left, "VM", "frames") && resolver.isNilForType(right, "CallFrame") {
						record(typed, "removes a frame")
					}
					if resolver.isField(left, "VM", "currentFrame") && resolver.isNilForType(right, "CallFrame") {
						record(typed, "clears currentFrame")
					}
					if resolver.isField(left, "VM", "stackTop") && resolver.containsField(right, "CallFrame", "StackBase") {
						record(typed, "resets stackTop to StackBase")
					}
				}
				resolver.recordAssignment(typed)
			case *ast.DeclStmt:
				general, ok := typed.Decl.(*ast.GenDecl)
				if ok && general.Tok == token.VAR {
					for _, specification := range general.Specs {
						if valueSpec, ok := specification.(*ast.ValueSpec); ok {
							info.recordValueSpec(valueSpec, resolver.variables)
						}
					}
				}
			case *ast.CallExpr:
				identifier, ok := typed.Fun.(*ast.Ident)
				if !ok || identifier.Name != "clear" || len(typed.Args) != 1 {
					break
				}
				slice, ok := typed.Args[0].(*ast.SliceExpr)
				if !ok {
					break
				}
				if resolver.isField(slice.X, "VM", "frames") {
					record(typed, "clears frames")
				}
				if resolver.isField(slice.X, "VM", "stack") && slice.Low != nil && resolver.containsField(slice.Low, "CallFrame", "StackBase") {
					record(typed, "clears stack from StackBase")
				}
			case *ast.ForStmt:
				if forClearsStackFromStackBase(typed, resolver) {
					record(typed, "clears stack from StackBase")
				}
			}
			return true
		})
	}
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
			name: "frame count reset assignment",
			source: `package vm
				type VM struct { frameCount int }
				func teardown(vm *VM) { vm.frameCount = 0 }`,
			want: []string{"resets frameCount"},
		},
		{
			name: "whole frame array reset",
			source: `package vm
				const FramesMax = 4
				type CallFrame struct{}
				type VM struct { frames [FramesMax]*CallFrame }
				func teardown(vm *VM) { vm.frames = [FramesMax]*CallFrame{} }`,
			want: []string{"resets frames"},
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
			name: "typed nil frame assignments",
			source: `package vm
				type CallFrame struct{}
				type VM struct { frames []*CallFrame; currentFrame *CallFrame }
				func teardown(vm *VM, index int) {
					vm.frames[index] = (*CallFrame)(nil)
					vm.currentFrame = (*CallFrame)(nil)
				}`,
			want: []string{"removes a frame", "clears currentFrame"},
		},
		{
			name: "clear frame array",
			source: `package vm
				type CallFrame struct{}
				type VM struct { frames [4]*CallFrame }
				func teardown(vm *VM) { clear(vm.frames[:]) }`,
			want: []string{"clears frames"},
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
			name: "homonymous fields on non runtime types are allowed",
			source: `package vm
				type CallFrame struct{}
				type queue struct { frameCount int }
				type cache struct { frames []*CallFrame; currentFrame *CallFrame; stackTop int }
				type window struct { StackBase int }
				func mutate(queue *queue, cache *cache, window *window, index int) {
					queue.frameCount--
					cache.frames[index] = nil
					cache.currentFrame = (*CallFrame)(nil)
					cache.stackTop = window.StackBase
					clear(cache.frames[:])
				}`,
		},
		{
			name: "defined type based on VM is not VM",
			source: `package vm
				type VM struct { frameCount int }
				type Mirror VM
				func reset(mirror *Mirror) { mirror.frameCount = 0 }`,
		},
		{
			name: "new VM receiver reset is detected",
			source: `package vm
				type VM struct { frameCount int }
				func reset() { vm := new(VM); vm.frameCount = 0 }`,
			want: []string{"resets frameCount"},
		},
		{
			name: "declared new function shadows builtin",
			source: `package vm
				type VM struct { frameCount int }
				type Mirror VM
				func new(value VM) *Mirror { return nil }
				func reset() { vm := new(VM); vm.frameCount = 0 }`,
		},
		{
			name: "shadowed receiver types stay in lexical scope",
			source: `package vm
				type VM struct { frameCount int }
				type queue struct { frameCount int }
				func inspect(vm *VM, queue *queue) {
					if true {
						vm := queue
						vm.frameCount--
					}
					vm.frameCount = 0
				}`,
			want: []string{"resets frameCount"},
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
