package vm

import (
	"errors"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrentModuleImportInitializesOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "counter.nx"), []byte("test_module_init()\nlet answer: int = 42\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	parent := NewWithConfig(VMConfig{RootPath: root})
	var initializations atomic.Int32
	entered := make(chan struct{})
	secondEntered := make(chan struct{})
	release := make(chan struct{})
	parent.DefineNative("test_module_init", func([]value.Value) value.Value {
		switch initializations.Add(1) {
		case 1:
			close(entered)
		case 2:
			close(secondEntered)
		}
		<-release
		return value.NewNull()
	})
	code := compileModuleProgram(t, root, "use counter\n")
	machines := []*VM{
		NewWithShared(parent.shared, parent.Config),
		NewWithShared(parent.shared, parent.Config),
	}
	start := make(chan struct{})
	errors := make(chan error, len(machines))
	for _, machine := range machines {
		go func(machine *VM) {
			<-start
			errors <- machine.Interpret(code)
		}(machine)
	}
	close(start)
	<-entered
	select {
	case <-secondEntered:
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	for range machines {
		select {
		case err := <-errors:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent imports did not complete")
		}
	}
	if initializations.Load() != 1 {
		t.Fatalf("initializations=%d, want 1", initializations.Load())
	}
}

func TestDirectImportCycleReportsPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.nx"), []byte("use b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.nx"), []byte("use a\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := compileModuleProgram(t, root, "use a\n")
	result := make(chan error, 1)
	go func() {
		result <- NewWithConfig(VMConfig{RootPath: root}).Interpret(code)
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "a -> b -> a") {
			t.Fatalf("cycle error=%v, want path a -> b -> a", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("direct import cycle did not complete")
	}
}

func TestDirectoryImportCycleReportsPath(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "bundle", "child")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "child.nx"), []byte("use bundle\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := compileModuleProgram(t, root, "use bundle\n")
	result := make(chan error, 1)
	go func() {
		result <- NewWithConfig(VMConfig{RootPath: root}).Interpret(code)
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "bundle -> bundle.child -> bundle") {
			t.Fatalf("cycle error=%v, want path bundle -> bundle.child -> bundle", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("directory import cycle did not complete")
	}
}

func TestNestedDirectoryImportCycleReportsPath(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "bundle", "child")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "x.nx"), []byte("use bundle\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := compileModuleProgram(t, root, "use bundle\n")
	result := make(chan error, 1)
	go func() {
		result <- NewWithConfig(VMConfig{RootPath: root}).Interpret(code)
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "bundle -> bundle.child -> bundle.child.x -> bundle") {
			t.Fatalf("cycle error=%v, want path bundle -> bundle.child -> bundle.child.x -> bundle", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nested directory import cycle did not complete")
	}
}

func compileModuleProgram(t *testing.T, root, source string) *chunk.Chunk {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root).Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestNestedModuleDeferBoundary(t *testing.T) {
	root := t.TempDir()
	moduleSource := `
defer module_record("module-old")
defer module_fail()
let zero: int = 0
print(1 / zero)
`
	if err := os.WriteFile(filepath.Join(root, "broken.nx"), []byte(moduleSource), 0o600); err != nil {
		t.Fatal(err)
	}
	code := compileModuleProgram(t, root, `
defer module_record("importer-old")
defer module_record("importer-new")
use broken
`)

	machine := NewWithConfig(VMConfig{RootPath: root})
	sentinel := errors.New("module cleanup failed")
	var order []string
	machine.DefineNative("module_record", func(args []value.Value) value.Value {
		order = append(order, args[0].Obj.(string))
		return value.NewNull()
	})
	machine.DefineContextualNative("module_fail", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), sentinel
	})

	err := machine.Interpret(code)
	if !slices.Equal(order, []string{"module-old", "importer-new", "importer-old"}) {
		t.Fatalf("cleanup order=%v, want [module-old importer-new importer-old]", order)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error=%v, want module cleanup sentinel", err)
	}
	importError, ok := err.(*RuntimeError)
	if !ok {
		t.Fatalf("error=%T %v, want outer *RuntimeError", err, err)
	}
	if importError.Message != "failed to import module 'broken'" {
		t.Fatalf("outer runtime message=%q, want structured import context", importError.Message)
	}
	moduleUnwind, ok := importError.Cause.(*UnwindError)
	if !ok {
		t.Fatalf("import cause=%T %v, want module *UnwindError", importError.Cause, importError.Cause)
	}
	if !errors.Is(moduleUnwind, sentinel) {
		t.Fatalf("module unwind=%v, want cleanup sentinel", moduleUnwind)
	}
}

func TestRuntimeModuleCacheSharesIdentityAcrossImportsAndChildVM(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "answer.nx"), []byte("let answer: int = 42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	source := "use answer as first\nuse answer as second\n"
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root).Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	if err := machine.Interpret(code); err != nil {
		t.Fatal(err)
	}
	first, ok := machine.GetGlobal("first")
	if !ok {
		t.Fatal("missing first module alias")
	}
	second, ok := machine.GetGlobal("second")
	if !ok {
		t.Fatal("missing second module alias")
	}
	if first.Obj != second.Obj {
		t.Fatal("second module import did not reuse the cached module object")
	}
	cached, ok := machine.GetModule("answer")
	if !ok || cached.Obj != first.Obj {
		t.Fatalf("cached module=%v, want first module object", cached)
	}

	child := NewWithShared(machine.shared, machine.Config)
	childLexer := lexer.New("use answer as child_answer\n")
	childParser := parser.New(childLexer)
	childProgram := childParser.ParseProgram()
	if len(childParser.Errors()) != 0 {
		t.Fatalf("child parser errors: %v", childParser.Errors())
	}
	childCode, _, err := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "child.nx"), root).Compile(childProgram)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Interpret(childCode); err != nil {
		t.Fatal(err)
	}
	childAnswer, ok := child.GetGlobal("child_answer")
	if !ok || childAnswer.Obj != cached.Obj {
		t.Fatalf("child module=%v, want shared cached module", childAnswer)
	}

	if _, err := machine.loadModule("missing_answer_module"); err == nil || !strings.Contains(err.Error(), "missing_answer_module") {
		t.Fatalf("missing module error=%v", err)
	}
}

func TestRuntimeFileModuleGlobalsContainDirectExports(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "collision.nx"), []byte("func delete(url: string) -> void\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	module, err := NewWithConfig(VMConfig{RootPath: root}).loadModule("collision")
	if err != nil {
		t.Fatal(err)
	}
	data := module.Obj.(*value.ObjMap).Snapshot()
	if _, ok := data["delete"]; !ok {
		t.Fatal("runtime file module omitted direct delete export")
	}
}

func TestRuntimeEmbeddedWildcardExportsAreDurable(t *testing.T) {
	module, err := New().loadModule("http")
	if err != nil {
		t.Fatal(err)
	}
	data := module.Obj.(*value.ObjMap).Snapshot()
	if _, ok := data["delete"]; !ok {
		t.Fatal("runtime http module omitted wildcard-imported delete")
	}
}

func TestRuntimeModuleCachePreservesStructConstructorCallableSchema(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "constructors.nx"), []byte("struct Box\n    value: int\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	expected := &value.RuntimeTypeInfo{
		Kind:       value.TYPE_CALLABLE,
		Params:     []*value.RuntimeTypeInfo{integer},
		ParamIsRef: []bool{false},
		Return:     &value.RuntimeTypeInfo{Kind: value.TYPE_STRUCT, Name: "Box", Fields: map[string]*value.RuntimeTypeInfo{"value": integer}},
	}
	l := lexer.New("use constructors as first\nuse constructors as second\n")
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root).Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	if err := machine.Interpret(code); err != nil {
		t.Fatal(err)
	}
	first, ok := machine.GetGlobal("first")
	if !ok {
		t.Fatal("missing first module alias")
	}
	second, ok := machine.GetGlobal("second")
	if !ok {
		t.Fatal("missing cached module alias")
	}
	firstConstructor := requireTestMapValue(t, first.Obj.(*value.ObjMap), "Box")
	secondConstructor := requireTestMapValue(t, second.Obj.(*value.ObjMap), "Box")
	if firstConstructor.Obj != secondConstructor.Obj {
		t.Fatal("cached import replaced the constructor definition")
	}
	if !runtimeCallableMatchesType(firstConstructor, expected) {
		t.Fatal("cached constructor did not satisfy exact schema")
	}
}

func TestRuntimeDirectoryModuleGlobalsContainOnlyLoadableChildren(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(root, "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "unloadable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "delete.nx"), []byte("let marker: int = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "unloadable", "broken.nx"), []byte("let marker: int = \"wrong\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	module, err := NewWithConfig(VMConfig{RootPath: root}).loadModule("bundle")
	if err != nil {
		t.Fatal(err)
	}
	data := module.Obj.(*value.ObjMap).Snapshot()
	if _, ok := data["delete"]; !ok {
		t.Fatal("runtime directory module omitted loadable file child")
	}
	if _, ok := data["unloadable"]; ok {
		t.Fatal("runtime directory module retained unloadable directory child")
	}
}

func TestRuntimeDirectoryModuleFailsForUnloadableFileChild(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "broken")
	if err := os.MkdirAll(broken, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "good.nx"), []byte("let marker: int = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "bad.nx"), []byte("let marker: int = \"wrong\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewWithConfig(VMConfig{RootPath: root}).loadModule("broken"); err == nil {
		t.Fatal("runtime directory module accepted unloadable file child")
	}
}

func TestRuntimeFileModuleFailsForMissingDirectImport(t *testing.T) {
	root := t.TempDir()
	source := "use definitely_missing_task5_module as dependency\nfunc delete(url: string) -> void\nend\n"
	if err := os.WriteFile(filepath.Join(root, "collision.nx"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWithConfig(VMConfig{RootPath: root}).loadModule("collision"); err == nil {
		t.Fatal("runtime file module accepted a missing direct import")
	}
}

func TestRuntimeFileModuleFailsForMissingSelectedExport(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dependency.nx"), []byte("let present: int = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "use dependency select missing\nfunc delete(url: string) -> void\nend\n"
	if err := os.WriteFile(filepath.Join(root, "collision.nx"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWithConfig(VMConfig{RootPath: root}).loadModule("collision"); err == nil {
		t.Fatal("runtime file module accepted a missing selected export")
	}
}

func TestRuntimeFileModuleFailsForInvalidTopLevelWildcardDependency(t *testing.T) {
	tests := []struct {
		name       string
		dependency string
	}{
		{name: "missing", dependency: "definitely_missing_task5_module"},
		{name: "compile invalid", dependency: "broken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "broken.nx"), []byte("let marker: int = \"wrong\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			wrapper := "use " + tt.dependency + " select *\nfunc delete(url: string) -> void\nend\n"
			if err := os.WriteFile(filepath.Join(root, "wrapper.nx"), []byte(wrapper), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewWithConfig(VMConfig{RootPath: root}).loadModule("wrapper"); err == nil {
				t.Fatal("runtime module accepted invalid top-level wildcard dependency")
			}
		})
	}
}

func TestRuntimeFunctionBodyOnlyWildcardDoesNotInvalidateModule(t *testing.T) {
	tests := []struct {
		name       string
		dependency string
	}{
		{name: "missing", dependency: "definitely_missing_task5_module"},
		{name: "self cycle", dependency: "safe"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			source := "func unused() -> void\n    use " + tt.dependency + " select *\nend\nfunc delete(url: string) -> void\nend\n"
			if err := os.WriteFile(filepath.Join(root, "safe.nx"), []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			module, err := NewWithConfig(VMConfig{RootPath: root}).loadModule("safe")
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := module.Obj.(*value.ObjMap).Get("delete"); !ok {
				t.Fatal("runtime module omitted direct delete export")
			}
		})
	}
}

func TestRuntimeRejectsTopLevelWildcardImportCycles(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		root  string
	}{
		{
			name: "self cycle",
			files: map[string]string{
				"cycle.nx": "use cycle select *\n",
			},
			root: "cycle",
		},
		{
			name: "mutual cycle",
			files: map[string]string{
				"left.nx":  "use right select *\n",
				"right.nx": "use left select *\n",
			},
			root: "left",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for name, source := range tt.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := NewWithConfig(VMConfig{RootPath: root}).loadModule(tt.root); err == nil {
				t.Fatal("runtime accepted a cyclic top-level wildcard import")
			}
		})
	}
}

func TestRuntimeNestedModuleCompilerUsesVMRoot(t *testing.T) {
	tests := []struct {
		name      string
		shadowDep string
	}{
		{name: "dependency only at root"},
		{
			name: "divergent nested dependency",
			shadowDep: `
func append(value: int) -> void
end
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			pkgDir := filepath.Join(root, "pkg")
			if err := os.MkdirAll(pkgDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "dep.nx"), []byte("func delete(url: string) -> void\nend\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			wrapper := `
use dep select *
func forward(url: string) -> void
    delete(url)
end
`
			if err := os.WriteFile(filepath.Join(pkgDir, "wrapper.nx"), []byte(wrapper), 0o600); err != nil {
				t.Fatal(err)
			}
			if tt.shadowDep != "" {
				if err := os.WriteFile(filepath.Join(pkgDir, "dep.nx"), []byte(tt.shadowDep), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			module, err := NewWithConfig(VMConfig{RootPath: root}).loadModule("pkg.wrapper")
			if err != nil {
				t.Fatal(err)
			}
			deleteBinding, ok := module.Obj.(*value.ObjMap).Get("delete")
			if !ok {
				t.Fatal("nested wrapper omitted root dependency delete")
			}
			closure, ok := deleteBinding.Obj.(*value.ObjClosure)
			if deleteBinding.Type != value.VAL_FUNCTION || !ok || closure.Function.Arity != 1 {
				t.Fatalf("delete binding=%v, want root one-argument closure", deleteBinding)
			}
		})
	}
}

func writeModuleFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func runModuleProgram(t *testing.T, root, source string) (value.Value, error) {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root).Compile(program)
	if err != nil {
		return value.NewNull(), err
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	// Interpret ANTES do return: `return reported, machine.Interpret(code)`
	// dependeria da ordem de avaliacao entre um operando comum e uma chamada,
	// que a spec do Go deixa em aberto — aqui reported so e lido depois.
	runErr := machine.Interpret(code)
	return reported, runErr
}

const geometryModule = `struct Point
    x: int
    y: int
end
func dist2(a: Point, b: Point) -> int
    return (a.x - b.x) * (a.x - b.x) + (a.y - b.y) * (a.y - b.y)
end
func apply(f: func(Point) -> int, p: Point) -> int
    return f(p)
end
func first<T>(a: T, b: T) -> T
    return a
end
`

func TestNamespaceAndSelectNameTheSameStruct(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"geometry.nx": geometryModule})
	reported, err := runModuleProgram(t, root, `use geometry
use geometry select dist2, Point, apply, first
let a: geometry.Point = geometry.Point(0, 0)
let b: Point = Point(3, 4)
let viaSelect: int = dist2(a, b)
let viaNamespace: int = geometry.dist2(b, a)
let viaFunc: int = apply(func(p: geometry.Point) -> int return p.x end, b)
let viaGeneric: Point = first(a, b)
test_report(to_str(viaSelect) + "|" + to_str(viaNamespace) + "|" + to_str(viaFunc) + "|" + to_str(viaGeneric.x))
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := reported.Obj.(string); got != "25|25|3|0" {
		t.Fatalf("got %q", got)
	}
}

// O struct `Point` declarado no importador NAO e o `Point` de geometry:
// typesEquivalent so aproxima dois nomes que resolvem para a MESMA
// *ast.StructStatement, entao um `Point` LOCAL continua recusado onde se
// espera o `Point` de geometry, e vice-versa — nos dois sentidos, porque a
// assinatura importada por select carrega a DECLARACAO de geometry.Point
// (Decl), nao o nome cru: `geometry.Point` passado para `dist2` (que quer o
// Point de geometry) e legitimamente a MESMA declaracao e compila; um
// `Point` local passado no lugar e uma declaracao DIFERENTE e e recusado. E
// o guard contra a versao frouxa da regra (looselySameType, que compara so o
// nome sem qualificador, aceitaria os dois como iguais nos dois sentidos).
func TestLocalStructIsNotTheModuleStructOfTheSameName(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"geometry.nx": geometryModule})
	// Sentido 1: geometry.Point (a MESMA declaracao que dist2 quer) compila.
	if _, err := runModuleProgram(t, root, `use geometry
use geometry select dist2
struct Point
    x: int
    y: int
end
let fromModule: geometry.Point = geometry.Point(0, 0)
dist2(fromModule, fromModule)
`); err != nil {
		t.Fatalf("geometry.Point passed to geometry's own dist2 must compile: %v", err)
	}
	// Sentido 2 (o guard de identidade cross-modulo): um Point LOCAL, com o
	// MESMO nome mas uma declaracao DIFERENTE, e recusado no lugar do Point
	// de geometry. So a substring do argumento e conferida — a mensagem
	// completa ("expected geometry.Point, got Point") depende da exibicao
	// qualificada que a Task 5 acrescenta.
	_, err := runModuleProgram(t, root, `use geometry select dist2
struct Point
    x: int
    y: int
end
let local: Point = Point(0, 0)
dist2(local, local)
`)
	if err == nil || !strings.Contains(err.Error(), "argument 1 to 'dist2'") {
		t.Fatalf("error=%v, want nominal mismatch on the local Point", err)
	}
}

func TestModuleVariableAssignmentViaNamespaceIsCompileError(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"calc.nx": "let sp: int = 0\nfunc push() -> void\n    sp = sp + 1\nend\n"})
	_, err := runModuleProgram(t, root, "use calc\ncalc.push()\ncalc.sp = 5\n")
	if err == nil || !strings.Contains(err.Error(), "cannot assign to 'calc.sp': module variables are read-only outside the module") || !strings.Contains(err.Error(), "hint: expose a function in 'calc'") {
		t.Fatalf("error=%v", err)
	}
	reported, err := runModuleProgram(t, root, "use calc\ncalc.push()\ntest_report(calc.sp)\n")
	if err != nil || reported.Int() != 1 {
		t.Fatalf("live read via namespace: %v / %v", reported, err)
	}
}

// Issue #47 parte 2: um `let` global homonimo ao `use` ja nao sombreia o
// binding de namespace em silencio — o escopo global e um namespace so e a
// colisao e erro de compilacao apontando o import.
func TestGlobalLetOverNamespaceImportIsCompileError(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"calc.nx": "let x: int = 1\n"})
	_, err := runModuleProgram(t, root, `use calc
struct P
    x: int
end
let calc: P = P(1)
calc.x = 2
test_report(calc.x)
`)
	if err == nil || !strings.Contains(err.Error(), "'calc' redeclared in this scope (previous declaration as import at line 1)") {
		t.Fatalf("global let over namespace import: %v", err)
	}
}
