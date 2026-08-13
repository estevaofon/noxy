package vm

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
