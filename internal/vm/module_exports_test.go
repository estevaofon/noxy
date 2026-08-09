package vm

import (
	"noxy-vm/internal/value"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeFileModuleGlobalsContainDirectExports(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "collision.nx"), []byte("func delete(url: string) -> void\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	module, err := NewWithConfig(VMConfig{RootPath: root}).loadModule("collision")
	if err != nil {
		t.Fatal(err)
	}
	data := module.Obj.(*value.ObjMap).Data
	if _, ok := data["delete"]; !ok {
		t.Fatal("runtime file module omitted direct delete export")
	}
}

func TestRuntimeEmbeddedWildcardExportsAreDurable(t *testing.T) {
	module, err := New().loadModule("http")
	if err != nil {
		t.Fatal(err)
	}
	data := module.Obj.(*value.ObjMap).Data
	if _, ok := data["delete"]; !ok {
		t.Fatal("runtime http module omitted wildcard-imported delete")
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
	data := module.Obj.(*value.ObjMap).Data
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
			if _, ok := module.Obj.(*value.ObjMap).Data["delete"]; !ok {
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
			deleteBinding, ok := module.Obj.(*value.ObjMap).Data["delete"]
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
