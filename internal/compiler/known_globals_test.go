package compiler

import (
	"strings"
	"testing"
)

// Issue #47 parte 3: global inexistente e erro de compilacao quando o
// embutidor semeia os nomes que o runtime garante (SetKnownGlobals).

func compileKnown(t *testing.T, src string, known ...string) error {
	t.Helper()
	c := New()
	c.SetKnownGlobals(append([]string{"print", "length", "sys_load_plugin"}, known...))
	_, _, err := c.Compile(parse(src))
	return err
}

func TestUndefinedGlobalBehindBranchIsCompileError(t *testing.T) {
	err := compileKnown(t, "let cond: bool = false\nif cond then\n    print(typo_global)\nend\n")
	want := "[line 3] undefined global 'typo_global'\n  hint: declare it with 'let typo_global = ...' or check the spelling"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestUndefinedGlobalInsideFunctionBodyIsCompileError(t *testing.T) {
	err := compileKnown(t, "func f() -> int\n    return nao_existe\nend\n")
	if err == nil || !strings.Contains(err.Error(), "undefined global 'nao_existe'") {
		t.Fatalf("got %v", err)
	}
}

func TestAssignToUndeclaredNameIsCompileError(t *testing.T) {
	err := compileKnown(t, "i = 0\n")
	want := "[line 1] cannot assign to undeclared name 'i'\n  hint: declare it with 'let i = ...'"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestRefOfUndefinedGlobalIsCompileError(t *testing.T) {
	err := compileKnown(t, "func inc(r: ref int) -> void\n    *r = *r + 1\nend\ninc(ref nada)\n")
	if err == nil || !strings.Contains(err.Error(), "undefined global 'nada'") {
		t.Fatalf("got %v", err)
	}
}

func TestForwardReferenceInsideFunctionStillCompiles(t *testing.T) {
	if err := compileKnown(t, "func f() -> int\n    return later\nend\nlet later: int = 1\nprint(f())\n"); err != nil {
		t.Fatalf("forward reference must compile: %v", err)
	}
}

func TestKnownNativeAndPluginNamesCompile(t *testing.T) {
	src := "let loaded: bool = sys_load_plugin(\"dynamodb\", \"bin\")\nlet r: any = dynamodb_request(\"connect\", 1)\n"
	program := parse(src)
	c := New()
	c.SetKnownGlobals(append([]string{"sys_load_plugin"}, PluginNativeNames(program)...))
	if _, _, err := c.Compile(program); err != nil {
		t.Fatalf("plugin native must be known: %v", err)
	}
}

func TestNamespaceImportAndStdlibNamesAreKnown(t *testing.T) {
	if err := compileKnown(t, "use strings\nuse array_utils select slice\nprint(strings.upper(\"a\"))\nprint(slice)\n", "strings_upper", "slice"); err != nil {
		t.Fatalf("imports must be known: %v", err)
	}
}

func TestWithoutSeedCompilerStaysPermissive(t *testing.T) {
	if _, _, err := New().Compile(parse("print(whatever)\n")); err != nil {
		t.Fatalf("unseeded compiler must not check globals: %v", err)
	}
}
