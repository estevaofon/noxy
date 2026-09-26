package vm

import (
	"strings"
	"testing"
)

// Achado 3 do Noxy-Editor: a stdlib nao tinha ordenacao — a arvore do
// editor precisava de nomes ordenados e a spec manda "ordenar keys(m)"
// sem oferecer como. `sort(ref xs)` ordena int[]/float[]/string[] no
// lugar; `sort_by(ref xs, key)` ordena qualquer T[] pela chave (int, float
// ou string) que a funcao devolve, de forma estavel. Os dois passam pelo
// mesmo CoW de append: uma copia tirada antes nao ve a ordenacao.

func sortReport(t *testing.T, program string) string {
	t.Helper()
	got := captureVMSource(t, program)
	s, ok := got.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", got)
	}
	return s
}

func TestSortOrdersPrimitiveArraysInPlace(t *testing.T) {
	got := sortReport(t, `
let xs: int[] = [3, -1, 2, 2, 0]
sort(ref xs)
let fs: float[] = [2.5, -1.0, 0.5]
sort(ref fs)
let ss: string[] = ["pera", "abacate", "Banana", "uva"]
sort(ref ss)
let empty: int[] = []
sort(ref empty)
test_report(to_str(xs) + "|" + to_str(fs) + "|" + to_str(ss) + "|" + to_str(empty))`)
	want := "[-1, 0, 2, 2, 3]|[-1.000000, 0.500000, 2.500000]|[Banana, abacate, pera, uva]|[]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSortIsCopyOnWriteThroughTheRef(t *testing.T) {
	got := sortReport(t, `
let xs: int[] = [3, 1, 2]
let before: int[] = xs
sort(ref xs)
test_report(to_str(before) + "|" + to_str(xs))`)
	if want := "[3, 1, 2]|[1, 2, 3]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSortByOrdersByKeyAndIsStable(t *testing.T) {
	got := sortReport(t, `
struct Node
    name: string
    size: int
end
let nodes: Node[] = [Node("b", 2), Node("a", 2), Node("c", 1), Node("A", 3)]
func by_size(n: Node) -> int
    return n.size
end
sort_by(ref nodes, by_size)
let by_size_names: string = ""
for n in nodes do
    by_size_names = by_size_names + n.name
end
let by_name = func(n: Node) -> string
    return n.name
end
sort_by(ref nodes, by_name)
let by_name_names: string = ""
for n in nodes do
    by_name_names = by_name_names + n.name
end
test_report(by_size_names + "|" + by_name_names)`)
	// tamanho 2 mantem a ordem original (b antes de a): estavel.
	if want := "cbaA|Aabc"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSortByAcceptsABareFuncAndFloatKeys(t *testing.T) {
	got := sortReport(t, `
let xs: int[] = [1, 2, 3, 4]
let key: func = func(v: int) -> float
    return 0.0 - to_float(v)
end
sort_by(ref xs, key)
test_report(to_str(xs))`)
	if want := "[4, 3, 2, 1]"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSortByPropagatesAKeyFunctionRuntimeError(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
let xs: int[] = [1, 2]
let boom = func(v: int) -> int
    let empty: int[] = []
    return empty[v]
end
sort_by(ref xs, boom)`)
	if err == nil || !strings.Contains(err.Error(), "array index out of bounds") {
		t.Fatalf("error=%v, want the key function's runtime error", err)
	}
}

func TestSortByRejectsMixedKeyTypesAtRuntime(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
let xs: int[] = [1, 2]
let key: func = func(v: int) -> any
    if v == 1 then
        return "um"
    end
    return 2
end
sort_by(ref xs, key)`)
	if err == nil || !strings.Contains(err.Error(), "sort_by: key function must return int, float or string") {
		t.Fatalf("error=%v", err)
	}
}

func TestSortByRejectsAKeyFunctionWithTheWrongArityAtRuntime(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
let xs: int[] = [1, 2]
let key: func = func(a: int, b: int) -> int
    return a
end
sort_by(ref xs, key)`)
	if err == nil || !strings.Contains(err.Error(), "sort_by") || !strings.Contains(err.Error(), "expected 2 arguments but got 1") {
		t.Fatalf("error=%v", err)
	}
}

func TestSortRejectsNonOrderableElementsAtCompileTime(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
struct P
    x: int
end
let ps: P[] = [P(1)]
sort(ref ps)`)
	want := "sort expects ref int[], ref float[] or ref string[], got ref P[]\n  hint: use sort_by(ref xs, key) with a key function returning int, float or string"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error=%v, want %q", err, want)
	}
}

func TestSortByRejectsAWrongKeyTypeAtCompileTime(t *testing.T) {
	cases := []struct {
		name, program, want string
	}{
		{"not callable", "let xs: int[] = [1]\nsort_by(ref xs, 5)", "argument 2 to 'sort_by': expected func(int) -> int, float or string, got int"},
		{"wrong return", "let xs: int[] = [1]\nlet k = func(v: int) -> bool\n    return true\nend\nsort_by(ref xs, k)", "argument 2 to 'sort_by': expected func(int) -> int, float or string, got func(int) -> bool"},
		{"wrong parameter", "let xs: int[] = [1]\nlet k = func(v: string) -> int\n    return 1\nend\nsort_by(ref xs, k)", "argument 2 to 'sort_by': expected func(int) -> int, float or string, got func(string) -> int"},
		{"wrong arity", "let xs: int[] = [1]\nlet k = func(a: int, b: int) -> int\n    return 1\nend\nsort_by(ref xs, k)", "argument 2 to 'sort_by': expected func(int) -> int, float or string, got func(int, int) -> int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretOrCompileErr(t, New(), tc.program)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %q", err, tc.want)
			}
		})
	}
}
