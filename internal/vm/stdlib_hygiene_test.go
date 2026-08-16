package vm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"noxy-vm/internal/stdlib"
	"noxy-vm/internal/value"
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

func TestHTTPClientDoesNotPrintOnRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		buffer := make([]byte, 4096)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, _ = conn.Read(buffer)
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"))
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	source := fmt.Sprintf(`use http_client select *
let r: ClientResponse = get("http://127.0.0.1:%d/")
test_report(r.ok)`, port)

	original := os.Stdout
	read, write, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	os.Stdout = write

	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	interpretErr := interpretVMSource(t, machine, source)

	_ = write.Close()
	os.Stdout = original
	printed, _ := io.ReadAll(read)

	if interpretErr != nil {
		t.Fatal(interpretErr)
	}
	if captured.Type != value.VAL_BOOL || !captured.AsBool {
		t.Fatalf("client request = %#v, want ok", captured)
	}
	if len(printed) != 0 {
		t.Fatalf("http client printed %q, want nothing", string(printed))
	}
}
