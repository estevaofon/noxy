package vm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"noxy-vm/internal/stdlib"
)

var nativeRegistrationHelpers = map[string]bool{
	"DefineNative":                        true,
	"DefineNativeWithSignature":           true,
	"DefineContextualNative":              true,
	"DefineContextualNativeWithSignature": true,
}

// collectNativeRegistrations returns every native name registered in non-test
// Go sources under internal/vm, with the file:line of each registration.
func collectNativeRegistrations(t *testing.T) map[string][]string {
	t.Helper()
	sources, err := filepath.Glob(filepath.Join(".", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	registrations := make(map[string][]string)
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, source, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", source, parseErr)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !nativeRegistrationHelpers[selector.Sel.Name] {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			name, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil {
				return true
			}
			position := fileSet.Position(call.Pos())
			registrations[name] = append(registrations[name],
				filepath.Base(position.Filename)+":"+strconv.Itoa(position.Line))
			return true
		})
	}
	return registrations
}

func TestEveryNativeIsRegisteredExactlyOnce(t *testing.T) {
	registrations := collectNativeRegistrations(t)
	if len(registrations) == 0 {
		t.Fatal("no native registrations were found; the collector is broken")
	}
	for name, positions := range registrations {
		if len(positions) > 1 {
			t.Errorf("native %q is registered %d times: %s", name, len(positions), strings.Join(positions, ", "))
		}
	}
}

func TestNoShippedDebugOutput(t *testing.T) {
	// Walk all of internal/, not just internal/vm, so a debug marker cannot
	// hide in the parser, compiler, or lexer.
	walkErr := filepath.WalkDir("..", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, marker := range []string{"DEBUG:", "Debug:"} {
			if strings.Contains(string(content), marker) {
				t.Errorf("%s contains the debug marker %q", filepath.ToSlash(path), marker)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}

	entries, err := stdlib.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nx") {
			continue
		}
		content, readErr := stdlib.FS.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, marker := range []string{"DEBUG:", "Debug:"} {
			if strings.Contains(string(content), marker) {
				t.Errorf("stdlib/%s contains the debug marker %q", entry.Name(), marker)
			}
		}
	}
}

func TestEmbeddedStdlibSourcesAreValidUTF8(t *testing.T) {
	entries, err := stdlib.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nx") {
			continue
		}
		content, readErr := stdlib.FS.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		checked++
		if !utf8.Valid(content) {
			t.Errorf("stdlib/%s is not valid UTF-8", entry.Name())
		}
		if strings.ContainsRune(string(content), utf8.RuneError) {
			t.Errorf("stdlib/%s contains a U+FFFD replacement character", entry.Name())
		}
	}
	if checked == 0 {
		t.Fatal("no embedded .nx sources were checked; the walk is broken")
	}
}
