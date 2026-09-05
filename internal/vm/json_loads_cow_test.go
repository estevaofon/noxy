package vm

import (
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// json_loads respeita a semântica de valor nos níveis ANINHADOS (issue #53
// item 1). O alvo top-level já era unicizado em populateRef (#83); um filho
// composto COMPARTILHADO — `let copia = t[0]` antes do json_loads — era
// mutado no lugar por prepareJSON{Struct,Array,Map}Mutation, e a cópia via a
// escrita. Cada caso afirma as duas metades: a cópia fica intacta E o alvo
// recebe o payload.

const jsonCowPrelude = `
struct Pair
    a: int
    b: int
end
struct Box
    p: Pair
    tags: string[]
end
`

func TestJSONLoadsClonesSharedNestedStructInArray(t *testing.T) {
	got := captureVMSource(t, jsonCowPrelude+`
let t: Pair[] = [Pair(0, 0)]
let copia: Pair = t[0]
let ok: bool = json_loads("[{\"a\":5,\"b\":6}]", ref t)
test_report([to_str(ok), to_str(copia.a), to_str(copia.b), to_str(t[0].a), to_str(t[0].b)])
`)
	reportedStrings(t, got, []string{"true", "0", "0", "5", "6"})
}

func TestJSONLoadsClonesSharedNestedArrayAndMap(t *testing.T) {
	got := captureVMSource(t, jsonCowPrelude+`
let grid: int[][] = [[1, 2]]
let linha: int[] = grid[0]
let ok1: bool = json_loads("[[9, 9, 9]]", ref grid)
let ms: map[string, int][] = [{"k": 1}]
let m2: map[string, int] = ms[0]
let ok2: bool = json_loads("[{\"k\": 7}]", ref ms)
test_report([to_str(ok1), to_str(linha), to_str(grid[0]), to_str(ok2), to_str(m2["k"]), to_str(ms[0]["k"])])
`)
	reportedStrings(t, got, []string{"true", "[1, 2]", "[9, 9, 9]", "true", "1", "7"})
}

// Dois níveis: o elemento (Box) é compartilhado com c2; o Pair dentro dele
// só tem um dono (o Box). Clonar o Box retém o Pair, que passa a ter dois
// donos e clona por sua vez — a cópia c2 não vê nada, nem no campo aninhado
// nem no array de strings.
func TestJSONLoadsClonesCascadeThroughNestedLevels(t *testing.T) {
	got := captureVMSource(t, jsonCowPrelude+`
let boxes: Box[] = [Box(Pair(0, 0), ["x"])]
let c2: Box = boxes[0]
let ok: bool = json_loads("[{\"p\":{\"a\":8,\"b\":9},\"tags\":[\"y\",\"z\"]}]", ref boxes)
test_report([to_str(ok), to_str(c2.p.a), to_str(c2.tags), to_str(boxes[0].p.a), to_str(boxes[0].p.b), to_str(boxes[0].tags)])
`)
	reportedStrings(t, got, []string{"true", "0", "[x]", "8", "9", "[y, z]"})
}

// Os casos (b), (c), (d) da issue já fechavam pelo top-level (#83); ficam
// como regressão, junto com o controle por bytecode (e).
func TestJSONLoadsTopLevelSharingStaysIsolated(t *testing.T) {
	got := captureVMSource(t, jsonCowPrelude+`
let xs: int[] = [1, 2]
let ys: int[] = xs
let ok2: bool = json_loads("[9, 9]", ref xs)
let m: map[string, int] = {"k": 1}
let m2: map[string, int] = m
let ok3: bool = json_loads("{\"k\": 7}", ref m)
let p: Pair = Pair(0, 0)
let q: Pair = p
let ok4: bool = json_loads("{\"a\":3,\"b\":4}", ref p)
let r: Pair = Pair(0, 0)
let s: Pair = r
r.a = 3
test_report([to_str(ys), to_str(xs), to_str(m2["k"]), to_str(m["k"]), to_str(q.a), to_str(p.a), to_str(s.a), to_str(r.a)])
`)
	reportedStrings(t, got, []string{"[1, 2]", "[9, 9]", "1", "7", "0", "3", "0", "3"})
}

// Dono único NÃO clona: o alvo e os filhos com um dono só mutam no lugar
// (CloneCountValue não cresce). Com um alias ao elemento, exatamente um clone.
func TestJSONLoadsDoesNotCloneUniqueOwners(t *testing.T) {
	machine := vmWithCloneReset()
	machine.DefineNative("clones_now", func(args []value.Value) value.Value {
		return value.NewInt(CloneCountValue())
	})
	var reported value.Value
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			reported = args[0]
		}
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, jsonCowPrelude+`
let t: Pair[] = [Pair(0, 0), Pair(1, 1)]
let antes: int = clones_now()
let ok1: bool = json_loads("[{\"a\":5,\"b\":6},{\"a\":7,\"b\":8}]", ref t)
let unico: int = clones_now() - antes
let alias: Pair = t[1]
let ok2: bool = json_loads("[{\"a\":1,\"b\":1},{\"a\":2,\"b\":2}]", ref t)
let compartilhado: int = clones_now() - antes - unico
test_report([to_str(ok1), to_str(unico), to_str(t[0].a), to_str(ok2), to_str(compartilhado), to_str(alias.a), to_str(t[1].a)])
`); err != nil {
		t.Fatal(err)
	}
	reportedStrings(t, reported, []string{"true", "0", "1", "true", "1", "7", "2"})
}
