package compiler

import (
	"noxy-vm/internal/chunk"
	"strings"
	"testing"
)

func TestMutatingBuiltinsBorrowAddressableArguments(t *testing.T) {
	_, err := compileFunctionSource(t, `
let values: int[] = [1]
let mapping: map[string, int] = {"a": 1}
append(ref values, 2)
let removed: int = pop(ref values)
delete(ref mapping, "a")`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMutatingBuiltinsRejectNonAddressableArguments(t *testing.T) {
	_, err := compileFunctionSource(t, `append([1], 2)`)
	if err == nil || !strings.Contains(err.Error(), "bind the value to a variable and pass 'ref <name>'") {
		t.Fatalf("error=%v", err)
	}
}

func TestAppendChecksElementType(t *testing.T) {
	_, err := compileFunctionSource(t, `
let values: int[] = [1]
append(ref values, "wrong")`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAppendAcceptsReferenceValuedElements(t *testing.T) {
	_, err := compileFunctionSource(t, `
struct Vertex
    value: int
end
let first: Vertex = Vertex(1)
let second: Vertex = Vertex(2)
let existing: ref Vertex = ref second
let values: (ref Vertex)[] = []
append(ref values, ref first)
append(ref values, ref second)
append(ref values, existing)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAppendRejectsExplicitReferenceForOrdinaryElement(t *testing.T) {
	_, err := compileFunctionSource(t, `
let item: int = 1
let values: int[] = []
append(ref values, ref item)`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got ref int") {
		t.Fatalf("error=%v", err)
	}
}

func TestAppendRejectsExplicitReferenceForAnyElement(t *testing.T) {
	_, err := compileFunctionSource(t, `
let item: int = 1
let values: any[] = []
append(ref values, ref item)`)
	if err == nil || !strings.Contains(err.Error(), "expected any, got ref int") {
		t.Fatalf("error=%v", err)
	}
}

func TestMutatingBuiltinNamesCanBeShadowed(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "global function",
			source: `
func append(left: int, right: int) -> int
    return left + right
end
let answer: int = append(20, 22)`,
		},
		{
			name: "local function",
			source: `
func answer() -> int
    let append: func(int, int) -> int = func(left: int, right: int) -> int
        return left + right
    end
    return append(20, 22)
end`,
		},
		{
			name: "captured function",
			source: `
func make_answer() -> func() -> int
    let append: func(int, int) -> int = func(left: int, right: int) -> int
        return left + right
    end
    return func() -> int
        return append(20, 22)
    end
end`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := compileFunctionSource(t, tt.source); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestImportedMutatingBuiltinNamesUseDynamicCallPath(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "selected append", source: "use fake_module select append\nappend(1, 2)"},
		{name: "aliased append", source: "use fake_module as append\nappend(1, 2)"},
		{name: "selected pop", source: "use fake_module select pop\npop(1)"},
		{name: "aliased pop", source: "use fake_module as pop\npop(1)"},
		{name: "selected delete", source: "use fake_module select delete\ndelete(1, 2)"},
		{name: "aliased delete", source: "use fake_module as delete\ndelete(1, 2)"},
		{name: "selected json_loads", source: "use fake_module select json_loads\njson_loads(1, 2)"},
		{name: "aliased json_loads", source: "use fake_module as json_loads\njson_loads(1, 2)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := compileFunctionSource(t, tt.source); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnrelatedWildcardImportKeepsBuiltinLowering(t *testing.T) {
	_, err := compileFunctionSource(t, "use sys select *\nappend([1], 2)")
	if err == nil || !strings.Contains(err.Error(), "bind the value to a variable and pass 'ref <name>'") {
		t.Fatalf("error=%v", err)
	}
}

func TestWildcardImportCollisionUsesDynamicCallPath(t *testing.T) {
	_, err := compileFunctionSource(t, `
use http_client select *
let url: string = "http://example.invalid"
delete(url)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWildcardImportCollisionIsKnownInsideEarlierFunction(t *testing.T) {
	_, err := compileFunctionSource(t, `
func remove(url: string) -> void
    delete(url)
end
use http_client select *
remove("http://example.invalid")`)
	if err != nil {
		t.Fatal(err)
	}
}

// Issue #47 parte 2: antes, func x `use m select *` do mesmo nome era
// "quem vem depois vence" (silencioso, dependente da ordem). Agora o escopo
// global e um namespace so: a colisao e erro de compilacao nas duas ordens.
func TestWildcardImportAndFunctionCollisionIsCompileError(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "wildcard after function",
			source: `
func delete(left: int, right: int) -> int
    return left + right
end
func remove(url: string) -> void
    delete(url)
end
use http_client select *
remove("http://example.invalid")`,
			want: "'delete' redeclared in this scope (previous declaration as function at line 2)",
		},
		{
			name: "function after wildcard",
			source: `
use http_client select *
func delete(left: int, right: int) -> int
    return left + right
end
func answer() -> int
    return delete(20, 22)
end`,
			want: "'delete' redeclared in this scope (previous declaration as import at line 2)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileFunctionSource(t, tt.source)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
		})
	}
}

func TestBuiltinCallBeforeLaterWildcardImportUsesSequentialBinding(t *testing.T) {
	_, err := compileFunctionSource(t, `
delete({"a": 1}, "a")
use http_client select *`)
	if err == nil || !strings.Contains(err.Error(), "bind the value to a variable and pass 'ref <name>'") {
		t.Fatalf("error=%v", err)
	}
}

func TestWildcardWrapperCollisionUsesDynamicCallPathOnFirstLoad(t *testing.T) {
	_, err := compileFunctionSource(t, `
use http select *
let url: string = "http://example.invalid"
delete(url)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCachedWildcardWrapperCollidesWithUserBinding(t *testing.T) {
	// Issue #47 parte 2: o wrapper `delete` de `use http select *` e a
	// funcao do usuario disputam o mesmo nome global — erro, nao sombra.
	_, err := compileFunctionSource(t, `
use http
func delete(left: int, right: int) -> int
    return left + right
end
use http select *
let url: string = "http://example.invalid"
delete(url)`)
	if err == nil || !strings.Contains(err.Error(), "'delete' redeclared in this scope (previous declaration as function at line 3)") {
		t.Fatalf("got %v", err)
	}
}

func TestPlainWrapperImportDoesNotShadowUnqualifiedBuiltin(t *testing.T) {
	_, err := compileFunctionSource(t, `
use http
delete({"a": 1}, "a")`)
	if err == nil || !strings.Contains(err.Error(), "bind the value to a variable and pass 'ref <name>'") {
		t.Fatalf("error=%v", err)
	}
}

func TestUncalledFunctionImportsDoNotShadowOuterBuiltins(t *testing.T) {
	tests := []struct {
		name      string
		importUse string
	}{
		{name: "selected", importUse: "use http_client select delete"},
		{name: "alias", importUse: "use http_client as delete"},
		{name: "wildcard", importUse: "use http_client select *"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := "func unused() -> void\n    " + tt.importUse + "\nend\ndelete({\"a\": 1}, \"a\")"
			_, err := compileFunctionSource(t, source)
			if err == nil || !strings.Contains(err.Error(), "bind the value to a variable and pass 'ref <name>'") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestMutatingBuiltinArityContracts(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "append too few", source: "append([])", want: "append expects 2 arguments, got 1"},
		{name: "append too many", source: "append([], 1, 2)", want: "append expects 2 arguments, got 3"},
		{name: "pop", source: "pop()", want: "pop expects 1 or 2 arguments, got 0"},
		{name: "delete", source: "delete({})", want: "delete expects 2 arguments, got 1"},
		{name: "json_loads", source: `json_loads("{}")`, want: "json_loads expects 2 arguments, got 1"},
		// Issue #126 item 4
		{name: "swap_remove too few", source: "let xs: int[] = [1]\nswap_remove(ref xs)", want: "swap_remove expects 2 arguments, got 1"},
		{name: "pop too many", source: "let xs: int[] = [1]\npop(ref xs, 0, 0)", want: "pop expects 1 or 2 arguments, got 3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileFunctionSource(t, tt.source)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestMutatingBuiltinAddressabilityContracts(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "pop", source: "pop([1])"},
		{name: "delete", source: `delete({"a": 1}, "a")`},
		{name: "json_loads", source: `json_loads("{}", {"a": 1})`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileFunctionSource(t, tt.source)
			if err == nil || !strings.Contains(err.Error(), "bind the value to a variable and pass 'ref <name>'") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestMutatingBuiltinTypeContracts(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "pop container",
			source: `
let mapping: map[string, int] = {"a": 1}
pop(ref mapping)`,
			want: "pop expects an array, got map[string, int]",
		},
		{
			name: "delete container",
			source: `
let values: int[] = [1]
delete(ref values, 0)`,
			want: "delete expects a map, got int[]",
		},
		{
			name: "delete key",
			source: `
let mapping: map[string, int] = {"a": 1}
delete(ref mapping, 0)`,
			want: "expected string, got int",
		},
		{
			name: "json text",
			source: `
let target: map[string, int] = {}
json_loads(42, ref target)`,
			want: "expected string, got int",
		},
		// Issue #126 item 4
		{
			name: "swap_remove without ref",
			source: `
let xs: int[] = [1]
swap_remove(xs, 0)`,
			want: "argument 1 to 'swap_remove': expected ref T[], got int[]",
		},
		{
			name: "swap_remove container",
			source: `
let m: map[string, int] = {}
swap_remove(ref m, 0)`,
			want: "swap_remove expects an array, got map[string, int]",
		},
		{
			name: "swap_remove index type",
			source: `
let xs: int[] = [1]
swap_remove(ref xs, "0")`,
			want: "argument 2 to 'swap_remove': expected int, got string",
		},
		{
			name: "pop index type",
			source: `
let xs: int[] = [1]
pop(ref xs, "0")`,
			want: "argument 2 to 'pop': expected int, got string",
		},
		{
			name: "pop with index preserves element type",
			source: `
let xs: string[] = ["a"]
let n: int = pop(ref xs, 0)`,
			want: "expected int, got string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileFunctionSource(t, tt.source)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestPopWithIndexAndSwapRemoveReturnElementType(t *testing.T) {
	src := `let xs: string[] = ["a", "b", "c"]
let first = pop(ref xs, 0)
let last = pop(ref xs)
let only = swap_remove(ref xs, 0)
`
	for _, name := range []string{"first", "last", "only"} {
		if got := inferredLetType(t, src, name); got != "string" {
			t.Fatalf("%s: %s, want string", name, got)
		}
	}
}

func TestPopRejectsExplicitReferenceIndex(t *testing.T) {
	_, err := compileFunctionSource(t, `
let xs: int[] = [1, 2, 3]
let i: int = 0
let removed: int = pop(ref xs, ref i)`)
	want := "argument 2 to 'pop': expected int, got ref int\n  hint: use '*' to read the referenced value"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error=%v, want %q", err, want)
	}
}

func TestDeleteRejectsExplicitReferenceForOrdinaryAnyKey(t *testing.T) {
	_, err := compileFunctionSource(t, `
let key: string = "a"
let mapping: map[any, int] = {"a": 1}
delete(ref mapping, ref key)`)
	if err == nil || !strings.Contains(err.Error(), "expected any, got ref string") {
		t.Fatalf("error=%v", err)
	}
}

func TestDeleteAcceptsDerefOfExistingReferenceKey(t *testing.T) {
	_, err := compileFunctionSource(t, `
let key_value: string = "a"
let key: ref string = ref key_value
let mapping: map[any, int] = {"a": 1}
delete(ref mapping, *key)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCompileDeferBuiltinCallsEmitDeferredOpcode(t *testing.T) {
	tests := []struct{ name, body string }{
		{"append", "let items: int[] = [1]\ndefer append(ref items, 2)"},
		{"pop", "let items: int[] = [1]\ndefer pop(ref items)"},
		{"delete", "let items: map[string, int] = {\"x\": 1}\ndefer delete(ref items, \"x\")"},
		{"json_loads", "let target: int = 0\ndefer json_loads(\"1\", ref target)"},
		{"chan_send", "let ch: chan int = make_chan(1)\ndefer chan_send(ch, 1)"},
		{"chan_recv", "let ch: chan int = make_chan(1)\ndefer chan_recv(ch)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fn := compiledFunction(t, "func run() -> void\n"+test.body+"\nend\n", "run")
			if !containsOpcode(fn.Chunk.(*chunk.Chunk).Code, chunk.OP_DEFER) {
				t.Fatalf("%s omitted OP_DEFER", test.name)
			}
		})
	}
}
