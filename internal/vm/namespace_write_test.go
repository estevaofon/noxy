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

// Issue #133 (revisao adversarial, caso 1): o objeto do namespace e uma VISAO
// do estado vivo do modulo, nao um valor com semantica de copia. Guardado num
// slot `any` (`let s: any = m`) ou passado a um `func(x: any)` ele ganhava
// outros donos duraveis, e a primeira escrita unicizava o slot: copyValue
// clonava o ObjMap para um bindingStore NOVO e a escrita sumia em silencio
// (antes do #133 era erro de runtime — barulhento). Precedente: em Python, Go
// e Nim um modulo e uma referencia; atribui-lo a outro nome nao copia o
// estado.
func TestNamespaceViewIsNeverCopiedOnWrite(t *testing.T) {
	t.Run("slot any", func(t *testing.T) {
		root := writeModuleFiles(t, map[string]string{"st4.nx": liveStateModule})
		reported, err := runModuleProgram(t, root, `use st4
let s: any = st4
s.count = 3
test_report(st4.read_count())
`)
		if err != nil || reported.Int() != 3 {
			t.Fatalf("reported=%v err=%v (want 3)", reported, err)
		}
	})

	t.Run("parametro any", func(t *testing.T) {
		root := writeModuleFiles(t, map[string]string{"st5.nx": liveStateModule})
		reported, err := runModuleProgram(t, root, `use st5
func w(x: any) -> void
    x.count = 3
end
w(st5)
test_report(st5.read_count())
`)
		if err != nil || reported.Int() != 3 {
			t.Fatalf("reported=%v err=%v (want 3)", reported, err)
		}
	})

	t.Run("map comum em any continua copiando", func(t *testing.T) {
		// Controle: a isencao vale SO para a visao de modulo. Um map comum
		// com dois donos duraveis mantem a copia na escrita (spec §3).
		root := writeModuleFiles(t, map[string]string{"st6.nx": liveStateModule})
		reported, err := runModuleProgram(t, root, `let a: any = {"x": 1}
let b: any = a
b.x = 2
test_report(a["x"] * 10 + b["x"])
`)
		if err != nil || reported.Int() != 12 {
			t.Fatalf("reported=%v err=%v (want 12)", reported, err)
		}
	})
}

func TestNamespaceWriteReachesTheAliasOwnBinding(t *testing.T) {
	// caracterizacao: a escrita cai na ligacao do modulo DAQUELE alias, mesmo
	// quando o TIPO do membro resolve pelo modulo que o declarou. `mid.nx` so
	// alcanca `x` por re-export (`use base select *`), e select liga um
	// snapshot: `mid.x = 5` muda a copia de `mid`, nao a variavel viva de
	// `base` (spec §11, "Module state is writable through the namespace").
	root := writeModuleFiles(t, map[string]string{
		"base.nx": `let x: int = 1

func read_x() -> int
    return x
end
`,
		"mid.nx": "use base select *\n",
	})

	t.Run("escrita pelo re-exportador nao alcanca o declarante", func(t *testing.T) {
		reported, err := runModuleProgram(t, root, `use mid
use base
mid.x = 5
test_report(mid.x * 100 + base.read_x() * 10 + base.x)
`)
		if err != nil || reported.Int() != 511 {
			t.Fatalf("reported=%v err=%v (want 511: mid.x=5, base.read_x()=1, base.x=1)", reported, err)
		}
	})

	t.Run("escrita pelo declarante alcanca o estado vivo", func(t *testing.T) {
		reported, err := runModuleProgram(t, root, `use mid
use base
base.x = 5
test_report(mid.x * 100 + base.read_x() * 10 + base.x)
`)
		if err != nil || reported.Int() != 155 {
			t.Fatalf("reported=%v err=%v (want 155: mid.x=1, base.read_x()=5, base.x=5)", reported, err)
		}
	})
}
