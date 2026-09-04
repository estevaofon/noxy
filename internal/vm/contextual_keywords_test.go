package vm

import (
	"os"
	"path/filepath"
	"testing"
)

// Issue #134 ponta a ponta: keyword de tipo como nome de variavel, funcao,
// campo, alias de modulo e segmento de caminho (`src/map.nx`), com escrita,
// ref e f-string sobre o campo; e identificador Unicode. O compilador e a VM
// nao mudaram — o teste garante que nenhum deles trata `int`/`map` como
// nome especial.
func TestContextualTypeKeywordsEndToEnd(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	module := "struct Tile\n    map: int\nend\nfunc tile(n: int) -> Tile\n    return Tile(n)\nend\n"
	if err := os.WriteFile(filepath.Join(root, "src", "map.nx"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `use src.map as map
struct S
    map: int
    n: int
end
func any(x: int) -> int
    return x + 1
end
func bump(v: ref int) -> void
    *v = *v + 1
end
let int: int = 5
let s: S = S(1, 2)
s.map = any(s.map)
bump(ref s.map)
let t: map.Tile = map.tile(int)
let café = t.map
test_report(f"{s.map}-{s.n}-{café}")
`
	reported, err := runModuleProgram(t, root, source)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reported.Obj.(string)
	if !ok || got != "3-2-5" {
		t.Fatalf("reported = %#v, want \"3-2-5\"", reported)
	}
}
