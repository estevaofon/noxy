package vm

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/stdlib"
	"noxy-vm/internal/token"
	"noxy-vm/internal/value"
)

var nativeRegistrationHelpers = map[string]bool{
	// defineValueNative (native_ref_args.go) e um DefineNative que interpoe
	// rejectRefArgs: registra native igual, e o coletor tem de ve-lo.
	"defineValueNative":                   true,
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
	fileSet := gotoken.NewFileSet()
	registrations := make(map[string][]string)
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, parseErr := goparser.ParseFile(fileSet, source, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", source, parseErr)
		}
		goast.Inspect(file, func(node goast.Node) bool {
			call, ok := node.(*goast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*goast.SelectorExpr)
			if !ok || !nativeRegistrationHelpers[selector.Sel.Name] {
				return true
			}
			literal, ok := call.Args[0].(*goast.BasicLit)
			if !ok || literal.Kind != gotoken.STRING {
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
	if captured.Type != value.VAL_BOOL || !captured.Bool() {
		t.Fatalf("client request = %#v, want ok", captured)
	}
	if len(printed) != 0 {
		t.Fatalf("http client printed %q, want nothing", string(printed))
	}
}

// Todo identificador que a stdlib CHAMA e que nao declara (nem importa de
// outro modulo da stdlib) tem de ser uma nativa registrada — io.nx declarou
// read_line/list_dir/rename por dois releases sem nativa (#56 item 4).
func TestStdlibWrappersCallOnlyRegisteredNatives(t *testing.T) {
	registrations := collectNativeRegistrations(t)
	entries, err := stdlib.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	topLevel := map[string]map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".nx") {
			continue
		}
		content, readErr := stdlib.FS.ReadFile(entry.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		module := strings.TrimSuffix(entry.Name(), ".nx")
		sources[module] = string(content)
		topLevel[module] = stdlibTopLevelNames(t, module, string(content))
	}
	checked := 0
	for module, source := range sources {
		for _, callee := range stdlibFreeCallees(source, module, topLevel) {
			checked++
			if _, registered := registrations[callee]; !registered {
				t.Errorf("stdlib/%s.nx calls %q, which is neither declared in the module nor a registered native", module, callee)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no free callees were checked; the scanner is broken")
	}
}

func stdlibTopLevelNames(t *testing.T, module, source string) map[string]bool {
	t.Helper()
	p := parser.New(lexer.New(source))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("stdlib/%s.nx: %v", module, p.Errors())
	}
	names := map[string]bool{}
	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case *ast.FunctionStatement:
			names[declaration.Name] = true
		case *ast.StructStatement:
			names[declaration.Name] = true
		case *ast.LetStmt:
			names[declaration.Name.Value] = true
		}
	}
	return names
}

// stdlibFreeCallees devolve (ordenados, sem repeticao) os identificadores
// usados como alvo de chamada `nome(` que nao sao: declarados no topo do
// modulo; parametros/campos/lets (qualquer `IDENT :` ou `let IDENT`);
// membros (`x.nome(`); nem trazidos por `use m select a, b` / `select *`.
func stdlibFreeCallees(source, module string, topLevel map[string]map[string]bool) []string {
	declared := map[string]bool{}
	for name := range topLevel[module] {
		declared[name] = true
	}
	var tokens []token.Token
	lex := lexer.New(source)
	for {
		tok := lex.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == token.EOF {
			break
		}
	}
	for i, tok := range tokens {
		if tok.Type != token.IDENTIFIER {
			continue
		}
		if i+1 < len(tokens) && tokens[i+1].Type == token.COLON {
			declared[tok.Literal] = true
		}
		if i > 0 && tokens[i-1].Type == token.LET {
			declared[tok.Literal] = true
		}
	}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].Type != token.USE || tokens[i+1].Type != token.IDENTIFIER || tokens[i+2].Type != token.SELECT {
			continue
		}
		imported := tokens[i+1].Literal
		for j := i + 3; j < len(tokens) && tokens[j].Type != token.NEWLINE && tokens[j].Type != token.EOF; j++ {
			if tokens[j].Literal == "*" {
				for name := range topLevel[imported] {
					declared[name] = true
				}
			} else if tokens[j].Type == token.IDENTIFIER {
				declared[tokens[j].Literal] = true
			}
		}
	}
	seen := map[string]bool{}
	var callees []string
	for i := 0; i+1 < len(tokens); i++ {
		tok := tokens[i]
		if tok.Type != token.IDENTIFIER || tokens[i+1].Type != token.LPAREN {
			continue
		}
		if i > 0 && tokens[i-1].Type == token.DOT {
			continue
		}
		if declared[tok.Literal] || seen[tok.Literal] {
			continue
		}
		seen[tok.Literal] = true
		callees = append(callees, tok.Literal)
	}
	sort.Strings(callees)
	return callees
}
