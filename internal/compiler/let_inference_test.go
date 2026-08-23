package compiler

// Inferencia local de tipo em `let` (issue #41, spec §3): `let x = expr` binda
// o tipo ESTATICO do RHS como se tivesse sido anotado — a variavel continua
// type-stable. Inferencia e unidirecional (RHS -> binding) e so em `let`; o
// tipo inferido tem de ser totalmente determinado e nao pode ser `any` no
// topo (fronteira dinamica pede anotacao explicita).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/ast"
)

const cannotInferText = "cannot infer type for"

func requireCompileError(t *testing.T, src string, wants ...string) {
	t.Helper()
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatalf("programa deveria falhar na compilacao:\n%s", src)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("erro deveria conter %q: %v", want, err)
		}
	}
}

// inferredLetType compila src e devolve o tipo gravado no `let` de topo
// chamado name (o compilador escreve o tipo inferido in-place no AST, como ja
// faz com anotacoes resolvidas).
func inferredLetType(t *testing.T, src, name string) string {
	t.Helper()
	program := parse(src)
	if _, _, err := New().Compile(program); err != nil {
		t.Fatalf("programa valido nao deveria falhar: %v", err)
	}
	for _, stmt := range program.Statements {
		if let, ok := stmt.(*ast.LetStmt); ok && let.Name.Value == name {
			if let.Type == nil {
				t.Fatalf("let %s: tipo inferido nao foi gravado no AST", name)
			}
			return let.Type.String()
		}
	}
	t.Fatalf("let %s nao encontrado", name)
	return ""
}

func TestLetInfersPrimitiveTypes(t *testing.T) {
	src := `let i = 10
let f = 1.5
let s = "a" + "b"
let b = 1 < 2
`
	for name, want := range map[string]string{"i": "int", "f": "float", "s": "string", "b": "bool"} {
		if got := inferredLetType(t, src, name); got != want {
			t.Errorf("let %s: want %s, got %s", name, want, got)
		}
	}
}

func TestInferredLetIsTypeStable(t *testing.T) {
	// Global
	requireCompileError(t, "let x = 10\nx = \"a\"\n", "type mismatch in assignment to global 'x'", "expected int, got string")
	// Local
	requireCompileError(t, `func f()
    let x = 10
    x = "a"
end`, "type mismatch in assignment to 'x'", "expected int, got string")
}

func TestInferredLetCompatibleReassignmentCompiles(t *testing.T) {
	requireCompiles(t, "let x = 10\nx = 20\nlet s = \"a\"\ns = \"b\"\n")
}

func TestLetInfersArrayLiteralAsDynamicArray(t *testing.T) {
	// `[1, 2, 3]` e int[3] como literal, mas o binding inferido e int[]:
	// fixar o tamanho surpreenderia (push, reatribuir com outro tamanho).
	src := "let xs = [1, 2, 3]\nxs = [4]\nlet grid = [[1, 2], [3, 4]]\n"
	if got := inferredLetType(t, src, "xs"); got != "int[]" {
		t.Errorf("xs: want int[], got %s", got)
	}
	if got := inferredLetType(t, src, "grid"); got != "int[][]" {
		t.Errorf("grid: want int[][], got %s", got)
	}
}

func TestLetInfersMapStructFunctionAndRef(t *testing.T) {
	src := `struct P
    x: int
end
let m = {"a": 1}
let p = P(1)
let f = func(a: int) -> int
    return a
end
let n = 5
let r = ref n
`
	for name, want := range map[string]string{
		"m": "map[string, int]",
		"p": "P",
		"f": "func(int) -> int",
		"r": "ref int",
	} {
		if got := inferredLetType(t, src, name); got != want {
			t.Errorf("let %s: want %s, got %s", name, want, got)
		}
	}
}

func TestLetInferenceRejectsEmptyLiterals(t *testing.T) {
	requireCompileError(t, "let xs = []\n", cannotInferText+" 'xs'", "hint: use 'let xs: <type>[] = []'")
	requireCompileError(t, "let m = {}\n", cannotInferText+" 'm'", "hint: use 'let m: map[<key>, <value>] = {}'")
	requireCompileError(t, "let n = null\n", cannotInferText+" 'n'", "hint: use 'let n: <type> = null'")
}

func TestLetInferenceRejectsTopLevelAny(t *testing.T) {
	// `any` no topo e fronteira dinamica: a anotacao torna a escolha explicita.
	requireCompileError(t, `func dyn() -> any
    return 1
end
let v = dyn()
`, cannotInferText+" 'v'", "is 'any'", "hint: use 'let v: any = ...'")
	// `any` ANINHADO e um tipo declaravel comum e e inferido fielmente.
	if got := inferredLetType(t, "let m = {\"a\": 1, \"b\": \"s\"}\n", "m"); got != "map[string, any]" {
		t.Errorf("m: want map[string, any], got %s", got)
	}
}

func TestLetInferenceRejectsUnknownGlobal(t *testing.T) {
	// `b` e declarado depois: na posicao do `let a` nao ha tipo estatico
	// (e em runtime seria leitura de global indefinido).
	requireCompileError(t, "let a = b\nlet b = 1\n", cannotInferText+" 'a'")
}

func TestInferredGlobalIsVisibleToEarlierFunctionBodies(t *testing.T) {
	// Corpo de funcao declarado ANTES do `let` de topo: a pre-declaracao de
	// globais ja precisa do tipo inferido — senao o global cairia em "tipo
	// desconhecido" e a atribuicao errada passaria em silencio.
	requireCompileError(t, `func bad()
    x = "a"
end
let x = 10
`, "type mismatch in assignment to global 'x'", "expected int, got string")
	requireCompiles(t, `func ok() -> int
    return x + 1
end
let x = 10
`)
}

func TestLetInfersFromGenericCall(t *testing.T) {
	src := `func id<T>(v: T) -> T
    return v
end
let y = id(5)
let z = id("s")
func use_it() -> int
    let w = id(1)
    return w + y
end
`
	if got := inferredLetType(t, src, "y"); got != "int" {
		t.Errorf("y: want int, got %s", got)
	}
	if got := inferredLetType(t, src, "z"); got != "string" {
		t.Errorf("z: want string, got %s", got)
	}
}

func TestInferredModuleLetExportsItsType(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cfg.nx"), []byte("let limit = 10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "main.nx")
	program := parse("use cfg select limit\nlimit = \"a\"\n")
	_, _, err := NewWithState(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), mainPath).Compile(program)
	if err == nil {
		t.Fatal("atribuicao de string a global int importado (tipo inferido no modulo) deveria falhar")
	}
	if !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("erro deveria citar o tipo inferido no modulo: %v", err)
	}
}

func TestLetInferenceRejectsVoidCall(t *testing.T) {
	// Chamada a funcao void nao produz valor: nao ha tipo para bindar (com
	// anotacao o mesmo programa ja era "expected int, got void").
	requireCompileError(t, `func f()
    print(1)
end
let v = f()
`, cannotInferText+" 'v'", "does not return a value")
}
