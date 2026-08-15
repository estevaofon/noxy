//go:build windows

package vm

import "testing"

func TestArchitectureImporterLoadsWindowsModuleExportData(t *testing.T) {
	loaded, err := newArchitectureImporter(t).Import("golang.org/x/sys/windows")
	if err != nil {
		t.Fatal(err)
	}
	if symbol := loaded.Scope().Lookup("Handle"); symbol == nil {
		t.Fatal("golang.org/x/sys/windows export data is missing Handle")
	}
}

func TestArchitectureImporterReportsUnresolvedModule(t *testing.T) {
	loaded, err := newArchitectureImporter(t).Import("noxy-vm/internal/architecture_missing_package")
	if err == nil {
		t.Fatalf("loaded=%v, want module resolution error", loaded)
	}
	if loaded != nil {
		t.Fatalf("loaded=%v alongside error=%v, want nil package", loaded, err)
	}
}
