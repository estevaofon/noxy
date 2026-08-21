package vm

import (
	"testing"
)

// Issue #58 item 1: o acesso a membro de um valor tipado por struct de modulo
// e tipado estaticamente, com o tipo do campo traduzido para a visao do
// programa. Estes testes garantem que o programa TIPADO continua rodando —
// leitura, escrita via CoW e atribuicao de campo composto com o tipo
// traduzido (`db.Row[]`) resolvendo no runtime type info.

const dbModuleSource = `struct Row
    v: int
    name: string
end
struct QueryResult
    rows: Row[]
    count: int
    by_name: map[string, Row]
end
func q() -> QueryResult
    let r: Row = Row(1, "a")
    return QueryResult([r], 1, {"a": r})
end
`

const modAModuleSource = `struct Inner
    v: int
end
struct Outer
    i: Inner
end
func make(v: int) -> Outer
    return Outer(Inner(v))
end
`

func TestTypedModuleFieldReadsThroughNamespace(t *testing.T) {
	root := t.TempDir()
	write(t, root, "db.nx", dbModuleSource)
	got := captureVMSourceAtRoot(t, root, `use db
let res: db.QueryResult = db.q()
let r: db.Row = res.rows[0]
let by: db.Row = res.by_name["a"]
test_report(r.v + by.v + res.count)`)
	assertReportedInt(t, got, 3)
}

func TestTypedModuleFieldReadsThroughSelect(t *testing.T) {
	root := t.TempDir()
	write(t, root, "db.nx", dbModuleSource)
	got := captureVMSourceAtRoot(t, root, `use db select QueryResult, Row, q
let res: QueryResult = q()
let r: Row = res.rows[0]
test_report(r.v + res.count)`)
	assertReportedInt(t, got, 2)
}

func TestTypedModuleFieldUnnameableStaysDynamicAtRuntime(t *testing.T) {
	root := t.TempDir()
	write(t, root, "db.nx", dbModuleSource)
	got := captureVMSourceAtRoot(t, root, `use db select QueryResult, q
let res: QueryResult = q()
let first: any = res.rows[0]
test_report(first.v)`)
	assertReportedInt(t, got, 1)
}

func TestTypedModuleArrayFieldAssignmentCarriesTranslatedRuntimeType(t *testing.T) {
	root := t.TempDir()
	write(t, root, "db.nx", dbModuleSource)
	got := captureVMSourceAtRoot(t, root, `use db
let res: db.QueryResult = db.q()
res.rows = [db.Row(2, "b"), db.Row(3, "c")]
res.by_name["b"] = db.Row(4, "d")
test_report(res.rows[1].v + res.by_name["b"].v)`)
	assertReportedInt(t, got, 7)
}

func TestTypedNestedModuleChainReadAndWrite(t *testing.T) {
	root := t.TempDir()
	write(t, root, "mod_a.nx", modAModuleSource)
	got := captureVMSourceAtRoot(t, root, `use mod_a
struct W
    o: mod_a.Outer
end
let w: W = W(mod_a.make(5))
let before: int = w.o.i.v
w.o.i.v = 7
w.o.i = mod_a.Inner(w.o.i.v + 10)
test_report(before * 100 + w.o.i.v)`)
	assertReportedInt(t, got, 517)
}

func TestTypedQualifiedStdlibFieldReadAndRefArgument(t *testing.T) {
	got := captureVMSource(t, `use io
struct A
    f: io.File
    n: int
end
func bump(r: ref int) -> void
    *r = *r + 1
end
let a: A = A(io.stdin(), 0)
let p: string = a.f.path
bump(ref a.n)
if p == "<stdin>" then
    test_report(a.n)
else
    test_report(-1)
end`)
	assertReportedInt(t, got, 1)
}
