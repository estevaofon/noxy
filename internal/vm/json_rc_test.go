package vm

// Under-count de RC dos builders JSON (achado lateral da #50, spec §5.3):
// valores construidos por json_loads/json_parse entravam nos conteineres sem
// Retain e substituicoes nao soltavam o ocupante anterior, entao uma copia
// por valor (`let p: Pair = t[0]`) chegava a Owners=1, IsShared falso, e a
// mutacao da copia acontecia no lugar — vazando para o conteiner. O modelo a
// espelhar e o do bytecode: todo conteiner que guarda um composto e um dono.

import "testing"

const jsonRCPrelude = `
struct Pair
    a: int
    b: int
end
`

func TestJSONLoadsNewArrayElementIsOwnedByArray(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let t: Pair[] = []
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", ref t)
let p: Pair = t[0]
p.a = 99
if ok then
    test_report(t[0].a)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}

func TestJSONLoadsNewMapValueIsOwnedByMap(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let m: map[string, Pair] = {}
let ok: bool = json_loads("{\"k\":{\"a\":1,\"b\":2}}", ref m)
let p: Pair = m["k"]
p.a = 99
if ok then
    test_report(m["k"].a)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}

func TestJSONLoadsNewStructFieldIsOwnedByInstance(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
struct Outer
    inner: Pair?
end
let o: Outer = Outer(null)
let ok: bool = json_loads("{\"inner\":{\"a\":1,\"b\":2}}", ref o)
if ok && o.inner != null then
    let p: Pair = o.inner
    p.a = 99
    test_report(o.inner.a)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}

func TestJSONLoadsReplacedElementReleasesOldAndRetainsNew(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let t: Pair[] = [Pair(0, 0)]
let antigo: Pair = t[0]
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", ref t)
let p: Pair = t[0]
p.a = 99
antigo.a = 55
if ok then
    test_report(t[0].a * 100 + antigo.a)
else
    test_report(999)
end`)
	// t[0] mutado no lugar (a=1 → json atualizou o proprio Pair(0,0), que
	// tinha 2 donos: array + `antigo`) — p (3o dono) clona ao escrever;
	// `antigo` tambem clona: 1*100 + 55.
	testExpectedObject(t, 155, got)
}

func TestJSONLoadsShrunkArrayReleasesDroppedElements(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let t: Pair[] = [Pair(0, 0), Pair(5, 5)]
let solto: Pair = t[1]
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", ref t)
solto.a = 77
if ok && length(t) == 1 then
    test_report(solto.a)
else
    test_report(999)
end`)
	// Depois do encolhimento `solto` e o unico dono de Pair(5,5): muta no
	// lugar sem clonar (Owners voltou a 1 — o array soltou).
	testExpectedObject(t, 77, got)
}

func TestJSONLoadsThroughRefIntoVariableRetains(t *testing.T) {
	got := runTypedFunctionProgram(t, jsonRCPrelude+`
let backing: Pair? = null
let t: (ref (Pair?))[] = [ref backing]
let ok: bool = json_loads("[{\"a\":1,\"b\":2}]", ref t)
if ok && backing != null then
    let p: Pair = backing
    p.a = 99
    test_report(backing.a)
else
    test_report(999)
end`)
	testExpectedObject(t, 1, got)
}

func TestJSONLoadsDynamicTopLevelRetains(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let d: any = [1, 2]
let ok: bool = json_loads("[5, 6]", ref d)
let e: any = d
e[0] = 99
if ok then
    test_report(d[0])
else
    test_report(999)
end`)
	testExpectedObject(t, 5, got)
}

func TestJSONParseBuildsOwnedChildren(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let d: any = json_parse("{\"k\": [1, 2]}")
let e: any = d["k"]
e[0] = 99
test_report(d["k"][0])`)
	testExpectedObject(t, 1, got)
}
