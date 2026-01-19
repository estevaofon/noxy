package pkgmanager

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestModFile(t *testing.T) {
	content := `
module noxy-test

noxy v1.2.0

require github.com/user/repo v1.0.0
`
	tmpfile, err := ioutil.TempFile("", "noxy.mod")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Test Parse
	config, err := ParseModFile(tmpfile.Name())
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
	if err := config.Save(tmpfile.Name()); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := ioutil.ReadFile(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

	savedContent := string(data)
	if !strings.Contains(savedContent, "noxy v1.3.0") {
		t.Errorf("Expected saved content to contain 'noxy v1.3.0', got:\n%s", savedContent)
	}
}
