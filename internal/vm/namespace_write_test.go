package vm

import (
	"strings"
	"testing"
)

// Issue #133 item 1: a escrita pelo namespace cai na variavel VIVA do modulo
// (ExportMap compartilha o bindingStore), como a leitura ja era.

const liveStateModule = `struct P
    x: int
end
let origin: P = P(0)
let count: int = 0
let xs: int[] = [1, 2]
func read_count() -> int
    return count
end
func read_origin_x() -> int
    return origin.x
end
func read_xs_len() -> int
    return length(xs)
end
`

func TestNamespaceDirectWriteIsSeenInsideTheModule(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, "use st\nst.count = 9\ntest_report(st.read_count())\n")
	if err != nil || reported.Int() != 9 {
		t.Fatalf("reported=%v err=%v", reported, err)
	}
}

func TestNamespaceNestedAndIndexedWritesAreSeenInsideTheModule(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, "use st\nst.origin.x = 99\nst.xs[0] = 10\ntest_report(st.read_origin_x() * 100 + st.xs[0])\n")
	if err != nil || reported.Int() != 9910 {
		t.Fatalf("reported=%v err=%v", reported, err)
	}
}

func TestNamespaceRefMemberMutatesTheModule(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, `use st
func bump(c: ref int) -> void
    *c = *c + 1
end
bump(ref st.count)
bump(ref st.count)
append(ref st.xs, 3)
test_report(st.read_count() * 10 + st.read_xs_len())
`)
	if err != nil || reported.Int() != 23 {
		t.Fatalf("reported=%v err=%v", reported, err)
	}
}

func TestSelectRemainsASnapshotAfterNamespaceWrite(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, "use st\nuse st select count\nst.count = 9\ntest_report(count * 100 + st.count)\n")
	if err != nil || reported.Int() != 9 {
		t.Fatalf("select must stay a snapshot: reported=%v err=%v", reported, err)
	}
}

func TestNamespaceWriteReplacesCompositeWithoutLeakOrDoubleFree(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, `use st
func replace() -> void
    let fresh: int[] = [7, 8, 9]
    st.xs = fresh
end
replace()
replace()
let copy: int[] = st.xs
append(ref st.xs, 1)
test_report(length(copy) * 10 + st.read_xs_len())
`)
	if err != nil || reported.Int() != 34 {
		t.Fatalf("reported=%v err=%v", reported, err)
	}
}

func TestNamespaceWriteRejectsWrongTypeAtCompileTime(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	_, err := runModuleProgram(t, root, "use st\nst.count = \"a\"\n")
	if err == nil || !strings.Contains(err.Error(), "type mismatch in assignment to 'st.count': expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestConcurrentNamespaceWritesDoNotCorruptTheRuntime(t *testing.T) {
	// docs/concurrency.md: operacoes individuais em global sao sincronizadas
	// (o bindingStore e mutexado); uma sequencia leitura-escrita nao e
	// atomica, entao o teste so exige que o runtime Go nao quebre (roda sob
	// -race no CI) e que a ultima escrita seja um dos valores escritos.
	root := writeModuleFiles(t, map[string]string{"st.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, `use st
func worker(id: int, c: any) -> void
    let i: int = 0
    while i < 200 do
        st.count = id
        i = i + 1
    end
    chan_send(c, id)
end
let c: any = make_chan(4)
spawn(worker, 1, c)
spawn(worker, 2, c)
spawn(worker, 3, c)
spawn(worker, 4, c)
let done: int = 0
while done < 4 do
    let got: any = chan_recv(c)
    done = done + 1
end
test_report(st.read_count())
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := reported.Int(); got < 1 || got > 4 {
		t.Fatalf("count must be one of the written ids, got %d", got)
	}
}

// Issue #133: o objeto do namespace e uma VISAO do modulo, nunca um valor com
// semantica de copia. Com mais de um dono do ObjMap (dois `use` do mesmo
// modulo — o cache entrega o MESMO valor de ExportMap —, ou um `let m: any =
// st2`), o caminho de escrita aninhada/indexada/por ref unicizava o global e
// copyValue clonava o mapa para um store NOVO, destacado do modulo: a escrita
// caia no orfao e o modulo nunca a via. O compilador nao pode unicizar essa
// base.
func TestNamespaceWritesSurviveMultipleOwners(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st2.nx": liveStateModule})
	reported, err := runModuleProgram(t, root, `use st2
use st2 as s
func bump(c: ref int) -> void
    *c = *c + 1
end
let m: any = st2
st2.origin.x = 99
st2.xs[0] = 77
append(ref st2.xs, 3)
bump(ref st2.count)
st2.count = st2.count + 1
test_report(st2.read_origin_x() * 1000000 + s.read_origin_x() * 10000 + st2.xs[0] * 100 + s.read_xs_len() * 10 + st2.read_count())
`)
	// origin.x=99 pelos dois aliases; xs[0]=77; xs cresce para 3; count = 1 (bump) + 1 = 2.
	if err != nil || reported.Int() != 99990000+7700+30+2 {
		t.Fatalf("reported=%v err=%v (want %d)", reported, err, 99990000+7700+30+2)
	}
}

// Issue #133: rebind de um membro `ref T` do modulo e escrita ATRAVES dele.
func TestNamespaceRefMemberRebindsAndWritesThroughIt(t *testing.T) {
	root := writeModuleFiles(t, map[string]string{"st3.nx": "let count: int = 5\nlet link: ref int = ref count\nfunc read_count() -> int\n    return count\nend\nfunc read_link() -> int\n    return *link\nend\n"})
	reported, err := runModuleProgram(t, root, `use st3
let other: int = 10
st3.link = ref other
*st3.link = *st3.link + 1
test_report(other * 100 + st3.read_count() * 10 + st3.read_link())
`)
	// other passa a 11; count do modulo intacto em 5; o link do modulo ve other.
	if err != nil || reported.Int() != 1161 {
		t.Fatalf("reported=%v err=%v (want 1161)", reported, err)
	}
}
