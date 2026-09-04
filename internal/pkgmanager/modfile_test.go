package pkgmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModFile(t *testing.T) {
	content := `
module noxy-test

noxy v1.2.0

require github.com/user/repo v1.0.0
`
	path := filepath.Join(t.TempDir(), "noxy.mod")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Test Parse
	config, err := ParseModFile(path)
	if err != nil {
		t.Fatalf("ParseModFile failed: %v", err)
	}

	if config.Module != "noxy-test" {
		t.Errorf("Expected module noxy-test, got %s", config.Module)
	}

	if config.NoxyVersion != "v1.2.0" {
		t.Errorf("Expected noxy version v1.2.0, got %s", config.NoxyVersion)
	}

	if config.Require["github.com/user/repo"] != "v1.0.0" {
		t.Errorf("Expected require github.com/user/repo v1.0.0, got %s", config.Require["github.com/user/repo"])
	}

	// Test Save
	config.NoxyVersion = "v1.3.0"
	if err := config.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	savedContent := string(data)
	if !strings.Contains(savedContent, "noxy v1.3.0") {
		t.Errorf("Expected saved content to contain 'noxy v1.3.0', got:\n%s", savedContent)
	}
}

func TestModFileSaveIsSortedAndNormalized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noxy.mod")
	c := NewModuleConfig()
	c.Module = "proj"
	c.Require["github.com/z/z"] = "1.0.0"
	c.Require["github.com/a/a"] = "v0.2.0"
	c.Require["github.com/m/m"] = "HEAD"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "module proj\n\nrequire github.com/a/a v0.2.0\nrequire github.com/m/m HEAD\nrequire github.com/z/z v1.0.0\n"
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	back, err := ParseModFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Requires(); len(got) != 3 || got[0] != "github.com/a/a" || got[2] != "github.com/z/z" {
		t.Fatalf("Requires: %v", got)
	}
}

func TestModFileRejectsBadRequire(t *testing.T) {
	for _, line := range []string{
		"require https://github.com/x/y v1.0.0",
		"require git@github.com:x/y v1.0.0",
		"require github.com/x v1.0.0",
		"require github.com/x/y abc123",
	} {
		path := filepath.Join(t.TempDir(), "noxy.mod")
		if err := os.WriteFile(path, []byte("module p\n"+line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ParseModFile(path); err == nil || !strings.Contains(err.Error(), "noxy.mod:2:") {
			t.Fatalf("%q must fail with a line number, got %v", line, err)
		}
	}
	if err := ValidateModulePath("github.com/x/y"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateModulePath("github_com/x/y"); err == nil {
		t.Fatal("local path is not a module path")
	}
}
