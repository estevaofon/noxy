package vm

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"noxy-vm/internal/value"
)

// Sondas e reproducoes da issue #55: contêineres criados por natives passam
// a ser donos duraveis dos filhos compostos (value.NewArray/NewMapWithData/
// NewInstanceWith retem), com NewArrayAdopting nos moves para nao reter em
// dobro. Os programas Noxy abaixo falhavam no develop 1680266 (a copia por
// valor mutava o original).

// vmWithOwnersProbe registra o native de teste probe_owners(x), que grava
// value.OwnersCount(x) sem reter o argumento (ReadonlyArgs, como as sondas
// de rc_uniqueness_test.go).
func vmWithOwnersProbe(t *testing.T) (*VM, *int32) {
	t.Helper()
	machine := New()
	observed := int32(-99)
	machine.DefineNative("probe_owners", func(args []value.Value) value.Value {
		if len(args) == 1 {
			observed = value.OwnersCount(args[0])
		}
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_owners")
	return machine, &observed
}

// Literal de array: OP_ARRAY retem e entrega ao construtor adotante — o
// elemento tem exatamente UM dono (o array). Um NewArray que retivesse de
// novo deixaria 2 e todo elemento de literal nasceria "compartilhado".
func TestArrayLiteralElementHasExactlyOneOwner(t *testing.T) {
	machine, observed := vmWithOwnersProbe(t)
	src := `
struct Pair
    a: int
    b: int
end
let t: Pair[] = [Pair(1, 1)]
probe_owners(t[0])
`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if *observed != 1 {
		t.Fatalf("elemento de literal deve ter Owners=1 (sem double-retain em OP_ARRAY), veio %d", *observed)
	}
}

// copyValue (clone CoW raso): o array novo ganha posse dos filhos — cada
// filho passa a ter 2 donos (original + clone), nem 1 nem 3.
func TestCopyValueCloneGivesChildrenASecondOwner(t *testing.T) {
	machine := New()
	inner := value.NewArray(nil)
	outer := value.NewArray([]value.Value{inner}) // NewArray retem: inner Owners=1
	if got := value.OwnersCount(inner); got != 1 {
		t.Fatalf("pre-condicao: Owners=%d, esperado 1", got)
	}
	clone := machine.copyValue(outer)
	if got := value.OwnersCount(inner); got != 2 {
		t.Fatalf("apos copyValue o filho deve ter 2 donos (original + clone), veio %d", got)
	}
	if clone.Obj.(*value.ObjArray).Elements[0].Obj != inner.Obj {
		t.Fatal("clone raso deve compartilhar o filho (mesmo ponteiro)")
	}
}

// Envelope ok do call_result: a posse de r.value pelo envelope e registrada
// UMA vez (pelo NewMapWithData em callResultOkEnvelope); o retain manual de
// invokeBoundaryCall saiu. Owners=2 aqui significaria valor eternamente
// IsShared (clone a cada mutacao).
func TestCallResultOkValueHasExactlyOneOwner(t *testing.T) {
	machine, observed := vmWithOwnersProbe(t)
	src := `
func faz_array() -> int[]
    return [1, 2, 3]
end
let r: any = call_result(faz_array)
probe_owners(r["value"])
`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if *observed != 1 {
		t.Fatalf("r.value deve ter o envelope como unico dono (Owners=1), veio %d", *observed)
	}
}

// slice (builtins_collections.go): a copia e dona dos elementos que leva;
// mutar a copia nao pode alcancar o original.
func TestSliceCopyDoesNotAliasOriginal(t *testing.T) {
	reported := captureVMSource(t, `
struct Pair
    a: int
    b: int
end
let t: Pair[] = [Pair(0, 0), Pair(1, 1)]
let s: Pair[] = slice(t, 0, 2)
s[0].a = 9
test_report(to_str(t[0].a) + "|" + to_str(s[0].a))
`)
	if text, _ := reported.Obj.(string); text != "0|9" {
		t.Fatalf("slice deve copiar por valor (original intacto): %q, esperado \"0|9\"", text)
	}
}

// task_await (builtins_tasks.go): o envelope e dono de value e de error.
func TestTaskAwaitEnvelopeOwnsValueAndError(t *testing.T) {
	reported := captureVMSource(t, `
func mk() -> int[]
    return [1, 2, 3]
end
func boom() -> int
    let xs: int[] = [1]
    return xs[5]
end
let t1: any = spawn_task(mk)
let r1: any = task_await(t1)
let v: any = r1["value"]
v[0] = 99
let rv: any = r1["value"]
let t2: any = spawn_task(boom)
let r2: any = task_await(t2)
let e: any = r2["error"]
e["kind"] = "hacked"
let re: any = r2["error"]
test_report(to_str(rv[0]) + "|" + to_str(v[0]) + "|" + re["kind"] + "|" + e["kind"])
`)
	if text, _ := reported.Obj.(string); text != "1|99|runtime|hacked" {
		t.Fatalf("envelope de task_await deve ficar intacto (CoW na copia): %q", text)
	}
}

// sqlite.query (builtins_sqlite.go): QueryResult e dono de columns e rows;
// cada Row e dona de values.
func TestSQLiteQueryEnvelopeOwnsColumnsAndRowValues(t *testing.T) {
	reported := captureVMSource(t, `
use sqlite
let db: sqlite.Database = sqlite.open(":memory:")
sqlite.exec(db, "CREATE TABLE t (id INTEGER, nome TEXT)")
sqlite.exec(db, "INSERT INTO t VALUES (1, 'a')")
let res: sqlite.QueryResult = sqlite.query(db, "SELECT * FROM t")
let cols: string[] = res.columns
cols[0] = "ZZZ"
let vals: any[] = res.rows[0].values
vals[0] = 999
sqlite.close(db)
test_report(res.columns[0] + "|" + cols[0] + "|" + to_str(res.rows[0].values[0]) + "|" + to_str(vals[0]))
`)
	if text, _ := reported.Obj.(string); text != "id|ZZZ|1|999" {
		t.Fatalf("envelope de sqlite.query deve ficar intacto (CoW nas copias): %q", text)
	}
}

// io.read_lines (builtins_io.go): IOLinesResult e dono de data.
func TestIOReadLinesEnvelopeOwnsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "linhas.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reported := captureVMSource(t, "use io\nlet f: io.File = io.open("+strconv.Quote(path)+", \"r\")\n"+
		"let r: io.IOLinesResult = io.read_lines(f)\nio.close(f)\n"+
		"let d: string[] = r.data\nd[0] = \"ZZZ\"\n"+
		"test_report(r.data[0] + \"|\" + d[0])\n")
	if text, _ := reported.Obj.(string); text != "a|ZZZ" {
		t.Fatalf("envelope de io.read_lines deve ficar intacto (CoW na copia): %q", text)
	}
}

// strings.split (builtins_strings.go): SplitResult e dono de parts.
func TestStringsSplitEnvelopeOwnsParts(t *testing.T) {
	reported := captureVMSource(t, `
use strings
let r: strings.SplitResult = strings.split("a,b", ",")
let p: string[] = r.parts
p[0] = "ZZZ"
test_report(r.parts[0] + "|" + p[0])
`)
	if text, _ := reported.Obj.(string); text != "a|ZZZ" {
		t.Fatalf("envelope de strings.split deve ficar intacto (CoW na copia): %q", text)
	}
}
