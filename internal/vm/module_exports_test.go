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
