package vm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
		"stack.go": {"readShort", "valuesEqual", "readConstant", "push", "pop", "peek", "captureUpvalue", "closeUpvalue"},
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
		matches, err := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path))
		if err != nil {
			return err
		}
		if !matches {
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

func TestReadArchitectureSourcesRespectsActivePlatform(t *testing.T) {
	directory := t.TempDir()
	otherPlatform := "linux"
	if runtime.GOOS == "linux" {
		otherPlatform = "windows"
	}
	sources := map[string]string{
		"common.go":                         "package fixture\n",
		"active_" + runtime.GOOS + ".go":    "package fixture\n",
		"inactive_" + otherPlatform + ".go": "package fixture\n",
	}
	for filename, source := range sources {
		if err := os.WriteFile(filepath.Join(directory, filename), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := readArchitectureSources(directory)
	if err != nil {
		t.Fatal(err)
	}
	var basenames []string
	for _, source := range got {
		basenames = append(basenames, filepath.Base(source.filename))
	}
	want := []string{"active_" + runtime.GOOS + ".go", "common.go"}
	if strings.Join(basenames, ",") != strings.Join(want, ",") {
		t.Fatalf("sources=%v, want %v", basenames, want)
	}
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

type architectureSource struct {
	filename string
	source   string
}

type architecturePackage struct {
	fileSet *token.FileSet
	files   []*ast.File
	info    *types.Info
	pkg     *types.Package
	errors  []error
}

type architectureImporter struct {
	moduleRoot        string
	packages          map[string]*types.Package
	loading           map[string]bool
	fallback          types.Importer
	exportFiles       map[string]string
	exportFilesLoaded bool
	moduleImports     types.Importer
}

func newArchitectureImporter(t *testing.T) *architectureImporter {
	t.Helper()
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	loader := &architectureImporter{
		moduleRoot:  moduleRoot,
		packages:    make(map[string]*types.Package),
		loading:     make(map[string]bool),
		fallback:    importer.Default(),
		exportFiles: make(map[string]string),
	}
	loader.moduleImports = importer.ForCompiler(token.NewFileSet(), "gc", loader.openExportData)
	return loader
}

func (loader *architectureImporter) Import(importPath string) (*types.Package, error) {
	if loaded := loader.packages[importPath]; loaded != nil {
		return loaded, nil
	}
	if strings.HasPrefix(importPath, "noxy-vm/") && !loader.loading[importPath] {
		loader.loading[importPath] = true
		defer delete(loader.loading, importPath)
		directory := filepath.Join(loader.moduleRoot, filepath.FromSlash(strings.TrimPrefix(importPath, "noxy-vm/")))
		sources, err := readArchitectureSources(directory)
		if err == nil && len(sources) != 0 {
			checked := checkArchitecturePackage(importPath, sources, loader, true)
			if checked.pkg != nil {
				loader.packages[importPath] = checked.pkg
				return checked.pkg, nil
			}
		}
	}
	// Tenta primeiro o importer com dados de exportacao do modulo (mesma
	// instancia de moduleImports para todo o teste): assim um pacote como
	// "context", alcancavel tanto diretamente quanto via dependencia
	// transitiva de terceiros (ex.: github.com/tetratelabs/wazero),
	// resolve sempre para o MESMO *types.Package. Se a ordem fosse
	// invertida (fallback primeiro), o mesmo import path podia gerar dois
	// *types.Package distintos — um via importer.Default, outro embutido
	// nos dados de exportacao de um dependente — e o checker via erros do
	// tipo "does not implement ... (wrong type for method)".
	loaded, moduleErr := loader.importModuleExportData(importPath)
	if moduleErr == nil {
		loader.packages[importPath] = loaded
		return loaded, nil
	}
	loaded, fallbackErr := loader.fallback.Import(importPath)
	if fallbackErr != nil {
		return nil, errors.Join(
			fmt.Errorf("module-aware importer for %q: %w", importPath, moduleErr),
			fmt.Errorf("default importer for %q: %w", importPath, fallbackErr),
		)
	}
	loader.packages[importPath] = loaded
	return loaded, nil
}

type architectureListedPackage struct {
	ImportPath string
	Export     string
	Error      *struct {
		Err string
	}
}

func (loader *architectureImporter) importModuleExportData(importPath string) (*types.Package, error) {
	if !loader.exportFilesLoaded {
		if err := loader.populateExportFiles(); err != nil {
			return nil, err
		}
	}
	if loader.exportFiles[importPath] == "" {
		return nil, fmt.Errorf("go list returned no export data")
	}
	loaded, err := loader.moduleImports.Import(importPath)
	if err != nil {
		return nil, fmt.Errorf("read export data: %w", err)
	}
	return loaded, nil
}

// populateExportFiles roda "go list -deps -export" uma unica vez para
// "./..." (o modulo inteiro), em vez de uma vez por import path de
// primeiro nivel: alem de mais barato (um so processo "go list"), garante
// que todo pacote alheio — padrao ou de terceiros — passe sempre por
// loader.moduleImports, a mesma instancia de importer.ForCompiler, o que
// e o que da a eles identidade de tipo consistente (ver comentario em
// Import). "testdata" e ignorado por convencao do proprio "go list", entao
// o guest wasip1/wasm de internal/ext/testdata nunca entra nesta listagem.
func (loader *architectureImporter) populateExportFiles() error {
	loader.exportFilesLoaded = true
	command := exec.Command("go", "list", "-deps", "-export", "-json", "./...")
	command.Dir = loader.moduleRoot
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) != 0 {
			return fmt.Errorf("go list: %s: %w", strings.TrimSpace(string(exitErr.Stderr)), err)
		}
		return fmt.Errorf("go list: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var listed architectureListedPackage
		if err := decoder.Decode(&listed); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("decode go list output: %w", err)
		}
		if listed.Error != nil {
			return fmt.Errorf("go list package %q: %s", listed.ImportPath, listed.Error.Err)
		}
		if listed.Export != "" {
			loader.exportFiles[listed.ImportPath] = listed.Export
		}
	}
	return nil
}

func (loader *architectureImporter) openExportData(importPath string) (io.ReadCloser, error) {
	filename := loader.exportFiles[importPath]
	if filename == "" {
		return nil, fmt.Errorf("no export data for %q", importPath)
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open export data for %q: %w", importPath, err)
	}
	return file, nil
}

func readArchitectureSources(directory string) ([]architectureSource, error) {
	filenames, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		return nil, err
	}
	var sources []architectureSource
	for _, filename := range filenames {
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		matches, err := build.Default.MatchFile(directory, filepath.Base(filename))
		if err != nil {
			return nil, err
		}
		if !matches {
			continue
		}
		source, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		sources = append(sources, architectureSource{filename: filename, source: string(source)})
	}
	sort.Slice(sources, func(left, right int) bool { return sources[left].filename < sources[right].filename })
	return sources, nil
}

func networkConsumingProbeMatches(t *testing.T, sources []architectureSource) []string {
	t.Helper()
	forbiddenFields := map[string]map[string]bool{
		"ListenerResource": {"bufferedAccept": true, "acceptProbeDeadline": true},
		"SocketResource":   {"bufferedRead": true, "readProbeDeadline": true},
	}
	forbiddenFunctions := map[string]bool{
		"beginListenerProbe":  true,
		"finishListenerProbe": true,
		"selectListener":      true,
		"beginSocketProbe":    true,
		"finishSocketProbe":   true,
		"selectSocket":        true,
	}
	found := make(map[string]bool)
	for _, source := range sources {
		file, err := parser.ParseFile(token.NewFileSet(), source.filename, source.source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", source.filename, err)
		}
		for _, declaration := range file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if forbiddenFunctions[typed.Name.Name] {
					found[typed.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, specification := range typed.Specs {
					typeSpec, ok := specification.(*ast.TypeSpec)
					if !ok {
						continue
					}
					fields := forbiddenFields[typeSpec.Name.Name]
					structure, ok := typeSpec.Type.(*ast.StructType)
					if len(fields) == 0 || !ok {
						continue
					}
					for _, field := range structure.Fields.List {
						for _, name := range field.Names {
							if fields[name.Name] {
								found[typeSpec.Name.Name+"."+name.Name] = true
							}
						}
					}
				}
			}
		}
	}
	matches := make([]string, 0, len(found))
	for match := range found {
		matches = append(matches, match)
	}
	sort.Strings(matches)
	return matches
}

func TestNetworkArchitectureExcludesConsumingProbes(t *testing.T) {
	sources, err := readArchitectureSources(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range networkConsumingProbeMatches(t, sources) {
		t.Errorf("obsolete consuming network probe structure: %s", match)
	}
}

func TestNetworkArchitectureGuardMatchesExactSyntax(t *testing.T) {
	source := `package guarded
type ListenerResource struct {
	bufferedAccept int
	acceptProbeDeadline int
}
type SocketResource struct {
	bufferedRead, readProbeDeadline int
}
type Unrelated struct { bufferedRead int }
func beginListenerProbe() {}
func finishListenerProbe() {}
func selectListener() {}
func beginSocketProbe() {}
func finishSocketProbe() {}
func selectSocket() {}
var _ = "func beginSocketProbe; bufferedAccept"
// func selectListener() {}
`
	got := networkConsumingProbeMatches(t, []architectureSource{{filename: "guarded.go", source: source}})
	want := []string{
		"ListenerResource.acceptProbeDeadline",
		"ListenerResource.bufferedAccept",
		"SocketResource.bufferedRead",
		"SocketResource.readProbeDeadline",
		"beginListenerProbe",
		"beginSocketProbe",
		"finishListenerProbe",
		"finishSocketProbe",
		"selectListener",
		"selectSocket",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("matches=%v, want %v", got, want)
	}
}

func checkArchitecturePackage(packagePath string, sources []architectureSource, loader types.Importer, ignoreBodies bool) *architecturePackage {
	fileSet := token.NewFileSet()
	files := make([]*ast.File, 0, len(sources))
	for _, source := range sources {
		file, err := parser.ParseFile(fileSet, source.filename, source.source, 0)
		if err != nil {
			return &architecturePackage{fileSet: fileSet, errors: []error{err}}
		}
		files = append(files, file)
	}
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	var typeErrors []error
	configuration := types.Config{
		Importer:                 loader,
		IgnoreFuncBodies:         ignoreBodies,
		DisableUnusedImportCheck: true,
		Error:                    func(err error) { typeErrors = append(typeErrors, err) },
	}
	pkg, _ := configuration.Check(packagePath, fileSet, files, info)
	return &architecturePackage{fileSet: fileSet, files: files, info: info, pkg: pkg, errors: typeErrors}
}

func typeCheckArchitecturePackage(t *testing.T, packagePath string, sources []architectureSource) *architecturePackage {
	t.Helper()
	checked := checkArchitecturePackage(packagePath, sources, newArchitectureImporter(t), false)
	if checked.pkg == nil || len(checked.files) != len(sources) || len(checked.errors) != 0 {
		t.Fatalf("could not parse and type-check architecture package %s: %v", packagePath, checked.errors)
	}
	return checked
}

func typeCheckProductionPackages(t *testing.T) []*architecturePackage {
	t.Helper()
	loader := newArchitectureImporter(t)
	grouped := make(map[string][]architectureSource)
	for _, filename := range productionGoFiles(t) {
		source, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		directory := filepath.Clean(filepath.Dir(filename))
		grouped[directory] = append(grouped[directory], architectureSource{filename: filename, source: string(source)})
	}
	directories := make([]string, 0, len(grouped))
	for directory := range grouped {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	packages := make([]*architecturePackage, 0, len(directories))
	for _, directory := range directories {
		absoluteDirectory, err := filepath.Abs(directory)
		if err != nil {
			t.Fatal(err)
		}
		relativeDirectory, err := filepath.Rel(loader.moduleRoot, absoluteDirectory)
		if err != nil {
			t.Fatal(err)
		}
		packagePath := "noxy-vm"
		if relativeDirectory != "." {
			packagePath += "/" + filepath.ToSlash(relativeDirectory)
		}
		checked := checkArchitecturePackage(packagePath, grouped[directory], loader, false)
		if checked.pkg == nil || len(checked.files) != len(grouped[directory]) || len(checked.errors) != 0 {
			t.Fatalf("could not parse and type-check architecture package %s: %v", packagePath, checked.errors)
		}
		packages = append(packages, checked)
	}
	return packages
}

func architectureNamedType(typ types.Type) *types.TypeName {
	if typ == nil {
		return nil
	}
	for {
		typ = types.Unalias(typ)
		pointer, ok := typ.(*types.Pointer)
		if !ok {
			break
		}
		typ = pointer.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return nil
	}
	return named.Obj()
}

func (checked *architecturePackage) selectorIsField(expression ast.Expr, ownerType, fieldName string) bool {
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			break
		}
		expression = parenthesized.X
	}
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != fieldName {
		return false
	}
	selection := checked.info.Selections[selector]
	owner := architectureNamedType(checked.info.TypeOf(selector.X))
	return selection != nil && selection.Kind() == types.FieldVal && owner != nil && owner.Pkg() == checked.pkg && owner.Name() == ownerType
}

func (checked *architecturePackage) expressionContainsField(expression ast.Expr, ownerType, fieldName string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok && checked.selectorIsField(selector, ownerType, fieldName) {
			found = true
			return false
		}
		return !found
	})
	return found
}

func (checked *architecturePackage) indexesField(expression ast.Expr, ownerType, fieldName string) bool {
	index, ok := expression.(*ast.IndexExpr)
	return ok && checked.selectorIsField(index.X, ownerType, fieldName)
}

func (checked *architecturePackage) isNil(expression ast.Expr) bool {
	if parenthesized, ok := expression.(*ast.ParenExpr); ok {
		return checked.isNil(parenthesized.X)
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		return checked.info.Uses[identifier] == types.Universe.Lookup("nil")
	}
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !checked.info.Types[call.Fun].IsType() {
		return false
	}
	return checked.isNil(call.Args[0])
}

func (checked *architecturePackage) isZero(expression ast.Expr) bool {
	value := checked.info.Types[expression].Value
	return value != nil && constant.Sign(value) == 0
}

func isEmptyCompositeLiteral(expression ast.Expr) bool {
	literal, ok := expression.(*ast.CompositeLit)
	return ok && len(literal.Elts) == 0
}

func (checked *architecturePackage) isBuiltin(identifier *ast.Ident, name string) bool {
	return identifier != nil && checked.info.Uses[identifier] == types.Universe.Lookup(name)
}

func (checked *architecturePackage) assignmentObject(identifier *ast.Ident) types.Object {
	if object := checked.info.Defs[identifier]; object != nil {
		return object
	}
	return checked.info.Uses[identifier]
}

func (checked *architecturePackage) forClearsStackFromStackBase(loop *ast.ForStmt) bool {
	initialization, ok := loop.Init.(*ast.AssignStmt)
	if !ok || len(initialization.Lhs) != len(initialization.Rhs) {
		return false
	}
	loopIndexes := make(map[types.Object]bool)
	for index, left := range initialization.Lhs {
		identifier, ok := left.(*ast.Ident)
		if ok && checked.expressionContainsField(initialization.Rhs[index], "CallFrame", "StackBase") {
			loopIndexes[checked.assignmentObject(identifier)] = true
		}
	}
	clears := false
	ast.Inspect(loop.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Lhs) != len(assignment.Rhs) {
			return !clears
		}
		for index, left := range assignment.Lhs {
			stackIndex, ok := left.(*ast.IndexExpr)
			if !ok || !checked.selectorIsField(stackIndex.X, "VM", "stack") || !isEmptyCompositeLiteral(assignment.Rhs[index]) {
				continue
			}
			identifier, ok := stackIndex.Index.(*ast.Ident)
			if ok && loopIndexes[checked.info.Uses[identifier]] {
				clears = true
				return false
			}
		}
		return !clears
	})
	return clears
}

func (checked *architecturePackage) unwindTerminalMutationMatches(ignoredFiles ...string) []string {
	ignored := make(map[string]bool, len(ignoredFiles))
	for _, filename := range ignoredFiles {
		ignored[filename] = true
	}
	var matches []string
	record := func(node ast.Node, description string) {
		matches = append(matches, description+" at line "+checked.fileSet.Position(node.Pos()).String())
	}
	for _, file := range checked.files {
		if ignored[filepath.Base(checked.fileSet.Position(file.Pos()).Filename)] {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.IncDecStmt:
				if typed.Tok == token.DEC && checked.selectorIsField(typed.X, "VM", "frameCount") {
					record(typed, "decrements frameCount")
				}
			case *ast.AssignStmt:
				for index, left := range typed.Lhs {
					frameCount := checked.selectorIsField(left, "VM", "frameCount")
					if frameCount && typed.Tok == token.SUB_ASSIGN {
						record(typed, "decrements frameCount")
					}
					if index >= len(typed.Rhs) {
						continue
					}
					right := typed.Rhs[index]
					if frameCount && typed.Tok == token.ASSIGN {
						binary, subtraction := right.(*ast.BinaryExpr)
						if subtraction && binary.Op == token.SUB && checked.expressionContainsField(binary.X, "VM", "frameCount") {
							record(typed, "decrements frameCount")
						} else if checked.isZero(right) {
							record(typed, "resets frameCount")
						}
					}
					if checked.selectorIsField(left, "VM", "frames") && isEmptyCompositeLiteral(right) {
						record(typed, "resets frames")
					}
					if checked.indexesField(left, "VM", "frames") && checked.isNil(right) {
						record(typed, "removes a frame")
					}
					if checked.selectorIsField(left, "VM", "currentFrame") && checked.isNil(right) {
						record(typed, "clears currentFrame")
					}
					if checked.selectorIsField(left, "VM", "stackTop") && checked.expressionContainsField(right, "CallFrame", "StackBase") {
						record(typed, "resets stackTop to StackBase")
					}
				}
			case *ast.CallExpr:
				identifier, ok := typed.Fun.(*ast.Ident)
				if !ok || !checked.isBuiltin(identifier, "clear") || len(typed.Args) != 1 {
					break
				}
				slice, ok := typed.Args[0].(*ast.SliceExpr)
				if !ok {
					break
				}
				if checked.selectorIsField(slice.X, "VM", "frames") {
					record(typed, "clears frames")
				}
				if checked.selectorIsField(slice.X, "VM", "stack") && slice.Low != nil && checked.expressionContainsField(slice.Low, "CallFrame", "StackBase") {
					record(typed, "clears stack from StackBase")
				}
			case *ast.ForStmt:
				if checked.forClearsStackFromStackBase(typed) {
					record(typed, "clears stack from StackBase")
				}
			}
			return true
		})
	}
	return matches
}

func unwindTerminalMutationMatches(t *testing.T, filename string, source []byte) []string {
	t.Helper()
	return unwindTerminalPackageMutationMatches(t, []architectureSource{{filename: filename, source: string(source)}})
}

func unwindTerminalPackageMutationMatches(t *testing.T, sources []architectureSource) []string {
	t.Helper()
	return typeCheckArchitecturePackage(t, "architecture/unwind", sources).unwindTerminalMutationMatches()
}

func runtimeNamedType(typ types.Type, checked *architecturePackage, name string) bool {
	object := architectureNamedType(typ)
	if object == nil || object.Name() != name {
		return false
	}
	return object.Pkg() == checked.pkg || object.Pkg() != nil && object.Pkg().Path() == "noxy-vm/internal/value"
}

func (checked *architecturePackage) runtimeSelectorIsField(selector *ast.SelectorExpr, ownerType, fieldName string) bool {
	selection := checked.info.Selections[selector]
	return selector.Sel.Name == fieldName && selection != nil && selection.Kind() == types.FieldVal && runtimeNamedType(checked.info.TypeOf(selector.X), checked, ownerType)
}

func isMapType(typ types.Type) bool {
	if typ == nil {
		return false
	}
	_, ok := types.Unalias(typ).Underlying().(*types.Map)
	return ok
}

func isMapPointerType(typ types.Type) bool {
	pointer, ok := types.Unalias(typ).(*types.Pointer)
	return ok && isMapType(pointer.Elem())
}

func (checked *architecturePackage) runtimeForbiddenMatches() []string {
	found := make(map[string]bool)
	for _, file := range checked.files {
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
						fieldType := checked.info.TypeOf(field.Type)
						for _, name := range field.Names {
							switch {
							case typeSpec.Name.Name == "VM" && name.Name == "openFiles":
								found["VM.openFiles field"] = true
							case typeSpec.Name.Name == "VM" && name.Name == "netBufferedData":
								found["VM.netBufferedData field"] = true
							case typeSpec.Name.Name == "VM" && name.Name == "netBufferedConns":
								found["VM.netBufferedConns field"] = true
							case name.Name == "Globals" && isMapType(fieldType):
								found["Globals raw map field"] = true
							case name.Name == "GlobalOwner" && isMapPointerType(fieldType):
								found["GlobalOwner raw map pointer field"] = true
							case name.Name == "DbHandles" && isMapType(fieldType):
								found["DbHandles raw map field"] = true
							case name.Name == "StmtHandles" && isMapType(fieldType):
								found["StmtHandles raw map field"] = true
							case name.Name == "StmtParams" && isMapType(fieldType):
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
			var legacyDispatchReceiver types.Object
			if function.Name.Name == "Invoke" && function.Recv != nil && len(function.Recv.List) == 1 {
				receiver := function.Recv.List[0]
				if len(receiver.Names) == 1 && runtimeNamedType(checked.info.TypeOf(receiver.Type), checked, "ObjNative") {
					legacyDispatchReceiver = checked.info.Defs[receiver.Names[0]]
				}
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.SelectorExpr:
					if checked.runtimeSelectorIsField(typed, "ObjMap", "Data") {
						found["ObjMap.Data selector"] = true
					}
				case *ast.CallExpr:
					selector, ok := typed.Fun.(*ast.SelectorExpr)
					if !ok || !checked.runtimeSelectorIsField(selector, "ObjNative", "Fn") {
						break
					}
					receiver, identifier := selector.X.(*ast.Ident)
					allowed := identifier && checked.info.Uses[receiver] == legacyDispatchReceiver
					if !allowed {
						found["direct native.Fn invocation"] = true
					}
				}
				return true
			})
		}
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

func runtimeForbiddenSourceMatches(t *testing.T, filename string, source []byte) []string {
	t.Helper()
	return typeCheckArchitecturePackage(t, "architecture/runtime", []architectureSource{{filename: filename, source: string(source)}}).runtimeForbiddenMatches()
}

func TestRuntimeFoundationExcludesObsoleteProductionStructures(t *testing.T) {
	for _, checked := range typeCheckProductionPackages(t) {
		for _, issue := range checked.runtimeForbiddenMatches() {
			t.Errorf("%s: %s", checked.pkg.Path(), issue)
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

	sources, err := readArchitectureSources(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := typeCheckArchitecturePackage(t, "noxy-vm/internal/vm", sources)
	for _, match := range checked.unwindTerminalMutationMatches("unwind.go") {
		t.Errorf("terminal frame teardown must remain in unwind.go: %s", match)
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
			name: "shadowed nil identifier is allowed",
			source: `package vm
				type CallFrame struct{}
				type VM struct { currentFrame *CallFrame }
				func install(vm *VM, nil *CallFrame) { vm.currentFrame = nil }`,
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
			name: "declared clear function shadows builtin",
			source: `package vm
				type CallFrame struct{}
				type VM struct { frames [4]*CallFrame }
				func clear(frames []*CallFrame) {}
				func reset(vm *VM) { clear(vm.frames[:]) }`,
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
				func reset() { vm := new(VM{}); vm.frameCount = 0 }`,
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

func TestUnwindArchitectureMatcherResolvesCrossFileFunctionResults(t *testing.T) {
	tests := []struct {
		name    string
		sources []architectureSource
		want    []string
	}{
		{
			name: "function result",
			sources: []architectureSource{
				{
					filename: "runtime_types.go",
					source: `package vm
						type VM struct { frameCount int }
						func currentVM() *VM { return nil }`,
				},
				{
					filename: "teardown.go",
					source: `package vm
						func teardown() { currentVM().frameCount-- }`,
				},
			},
			want: []string{"decrements frameCount"},
		},
		{
			name: "aliased function result",
			sources: []architectureSource{
				{
					filename: "runtime_types.go",
					source: `package vm
						type VM struct { frameCount int }
						type ActiveVM = VM
						func currentVM() *ActiveVM { return nil }`,
				},
				{
					filename: "teardown.go",
					source: `package vm
						func teardown() { currentVM().frameCount-- }`,
				},
			},
			want: []string{"decrements frameCount"},
		},
		{
			name: "method result",
			sources: []architectureSource{
				{
					filename: "runtime_types.go",
					source: `package vm
						type VM struct { frameCount int }
						type runtimeState struct { active *VM }
						func (state *runtimeState) currentVM() *VM { return state.active }`,
				},
				{
					filename: "teardown.go",
					source: `package vm
						func teardown(state *runtimeState) { state.currentVM().frameCount-- }`,
				},
			},
			want: []string{"decrements frameCount"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matches := unwindTerminalPackageMutationMatches(t, test.sources)
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
				type holder struct { Data int; Dataset int }
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
