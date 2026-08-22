package vm

import (
	"testing"
)

// json_loads(text, alvo) popula um alvo TIPADO no lugar (JSON_SUPPORT.md):
// cada campo/elemento novo é construído a partir do schema do alvo — string,
// float, bool, int[], float[], map, struct aninhado, `any` dinâmico — e um
// payload que não cabe devolve false sem escrita parcial. Os construtores
// tipados (buildTypedJSONValue/dynamicJSONValue) para array/map/struct/float/
// string não passavam por nenhum teste Go.

const jsonTypedPrelude = `
struct Inner
    n: int
end
struct Rec
    s: string
    f: float
    b: bool
    xs: int[]
    fs: float[]
    names: string[]
    m: map[string, int]
    grid: map[string, int[]]
    inner: Inner
    dyn: any
end
`

func TestJSONLoadsBuildsEveryTypedShapeFromSchema(t *testing.T) {
	got := captureVMSource(t, jsonTypedPrelude+`
let r: Rec = Rec("", 0.0, false, [], [], [], {}, {}, null, null)
let ok: bool = json_loads("{\"s\":\"hi\",\"f\":2.5,\"b\":true,\"xs\":[1,2],\"fs\":[1.5],\"names\":[\"a\",\"b\"],\"m\":{\"k\":3},\"grid\":{\"row\":[4,5]},\"inner\":{\"n\":7},\"dyn\":{\"a\":[1,true,null,2.5,\"x\"]}}", ref r)
test_report([to_str(ok), r.s, to_str(r.f), to_str(r.b), to_str(r.xs), to_str(r.fs), to_str(r.names), to_str(r.m), to_str(r.grid), to_str(r.inner.n), to_str(r.dyn)])
`)
	want := []string{"true", "hi", "2.500000", "true", "[1, 2]", "[1.500000]", "[a, b]", "{k: 3}", "{row: [4, 5]}", "7", "{a: [1, true, null, 2.500000, x]}"}
	cells := semArray(t, got)
	if len(cells) != len(want) {
		t.Fatalf("células=%d, want %d: %s", len(cells), len(want), got.String())
	}
	for i, cell := range cells {
		if s, ok := cell.Obj.(string); !ok || s != want[i] {
			t.Fatalf("célula %d: got %s, want %q", i, cell.String(), want[i])
		}
	}
}

func TestJSONLoadsRejectsPayloadThatDoesNotFitWithoutPartialWrites(t *testing.T) {
	cases := []struct{ name, body string }{
		{"string into int array", "let xs: int[] = [9]\nlet ok: bool = json_loads(\"[\\\"a\\\"]\", xs)\ntest_report([to_str(ok), to_str(xs)])\n"},
		{"float into int array", "let xs: int[] = [9]\nlet ok: bool = json_loads(\"[1.5]\", xs)\ntest_report([to_str(ok), to_str(xs)])\n"},
		{"object into int array", "let xs: int[] = [9]\nlet ok: bool = json_loads(\"{\\\"a\\\":1}\", xs)\ntest_report([to_str(ok), to_str(xs)])\n"},
		{"wrong field type in struct", jsonTypedPrelude + "let r: Rec = Rec(\"keep\", 0.0, false, [], [], [], {}, {}, Inner(0), null)\nlet ok: bool = json_loads(\"{\\\"s\\\": 5}\", ref r)\ntest_report([to_str(ok), r.s])\n"},
		{"array into map", "let m: map[string, int] = {\"keep\": 1}\nlet ok: bool = json_loads(\"[1]\", m)\ntest_report([to_str(ok), to_str(m)])\n"},
		{"string into map value", "let m: map[string, int[]] = {}\nlet ok: bool = json_loads(\"{\\\"a\\\": \\\"x\\\"}\", m)\ntest_report([to_str(ok), to_str(m)])\n"},
	}
	wantAfter := map[string]string{
		"string into int array":      "[9]",
		"float into int array":       "[9]",
		"object into int array":      "[9]",
		"wrong field type in struct": "keep",
		"array into map":             "{keep: 1}",
		"string into map value":      "{}",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cells := semArray(t, captureVMSource(t, tc.body))
			if len(cells) != 2 {
				t.Fatalf("esperava [ok, estado], obtido %d células", len(cells))
			}
			if s, _ := cells[0].Obj.(string); s != "false" {
				t.Fatalf("json_loads deveria devolver false, obtido %s", cells[0].String())
			}
			if s, _ := cells[1].Obj.(string); s != wantAfter[tc.name] {
				t.Fatalf("alvo deveria ficar intocado (%q), obtido %q", wantAfter[tc.name], s)
			}
		})
	}
}

func TestJSONLoadsGrowsTypedMapAndArrayInPlace(t *testing.T) {
	got := captureVMSource(t, `
let m: map[string, float[]] = {"old": [0.5]}
let ok1: bool = json_loads("{\"new\": [1.5, 2.5]}", m)
let xs: int[] = []
let ok2: bool = json_loads("[1, 2, 3]", xs)
let bs: bool[] = []
let ok3: bool = json_loads("[true, false]", bs)
let mm: map[string, map[string, int]] = {}
let ok4: bool = json_loads("{\"outer\": {\"k\": 1}}", mm)
test_report([to_str(ok1), to_str(length(m)), to_str(m["new"]), to_str(ok2), to_str(xs), to_str(ok3), to_str(bs), to_str(ok4), to_str(mm)])
`)
	want := []string{"true", "2", "[1.500000, 2.500000]", "true", "[1, 2, 3]", "true", "[true, false]", "true", "{outer: {k: 1}}"}
	cells := semArray(t, got)
	if len(cells) != len(want) {
		t.Fatalf("células=%d, want %d: %s", len(cells), len(want), got.String())
	}
	for i, cell := range cells {
		if s, ok := cell.Obj.(string); !ok || s != want[i] {
			t.Fatalf("célula %d: got %s, want %q", i, cell.String(), want[i])
		}
	}
}
