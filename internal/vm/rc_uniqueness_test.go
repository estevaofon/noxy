package vm

import (
	"fmt"
	"testing"

	"noxy-vm/internal/value"
)

// markProbeReadonly estampa ReadonlyArgs=true no native de sonda registrado
// via DefineNative. DefineNative usa stampReadonlyArgs, que só liga o flag
// para nomes na allowlist de cow_natives.go — nossos probes não estão lá, e
// por padrão ficariam com ReadonlyArgs=false (native "sem assinatura fora da
// allowlist": marca Shared nos args, mas nesta fase isso não mexe em
// Owners). Ligamos explicitamente porque uma tarefa futura fará natives sem
// assinatura reterem permanentemente seus args por padrão — sem isso aqui,
// essa mudança futura destabilizaria a aritmética medida por estes testes,
// que auditam apenas o retain do parâmetro de valor da função Noxy chamada
// (reader/inner/outer), não qualquer retain feito pelo próprio native de
// sonda.
func markProbeReadonly(t *testing.T, machine *VM, name string) {
	t.Helper()
	requireBuiltin(t, machine, name).ReadonlyArgs = true
}

// O programa passa um map por valor a um helper e reporta depois do
// retorno. O teste intercepta o valor via native de teste para medir Owners
// em tres momentos.
func TestParamRetainReleasedAfterReturn(t *testing.T) {
	machine := New()
	var during, after int32
	machine.DefineNative("probe_during", func(args []value.Value) value.Value {
		during = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_after", func(args []value.Value) value.Value {
		after = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_during")
	markProbeReadonly(t, machine, "probe_after")

	src := `
func reader(m: map[string, int]) -> int
    probe_during(m)
    return 1
end

func main()
    let m: map[string, int] = {"a": 1}
    reader(m)
    probe_after(m)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// during: slot do parametro de reader retido => >= 1
	if during < 1 {
		t.Fatalf("durante a chamada esperado Owners >= 1, veio %d", during)
	}
	// after: o retain do parametro foi liberado no fim do frame => o valor
	// caiu em exatamente 1 em relacao ao pico medido durante
	if after != during-1 {
		t.Fatalf("apos o retorno esperado %d, veio %d", during-1, after)
	}
}

// boom() aborta com erro de indice fora dos limites. O runtime desenrola
// (unwind) todos os frames ate o topo — inclusive o de main() — entao
// probe_after nunca chega a rodar dentro deste programa; o que resta
// auditavel aqui e que o erro realmente ocorre (nao silenciosamente ignora o
// unwind, nao entra em panico). A asserção real de que o release aconteceu
// durante o unwind está em TestParamReleaseOnUnwindDirect (Step 1b), que
// monta o frame a mao e inspeciona Owners diretamente.
func TestParamReleaseRunsOnUnwind(t *testing.T) {
	machine := New()
	machine.DefineNative("probe_after", func(args []value.Value) value.Value {
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_after")
	src := `
func boom(m: map[string, int]) -> int
    let arr: int[] = [1]
    return arr[99]
end

func main()
    let m: map[string, int] = {"a": 1}
    boom(m)
    probe_after(m)
end

main()`
	err := interpretVMSource(t, machine, src)
	if err == nil {
		t.Fatalf("esperado erro de indice fora dos limites, veio nil")
	}
}

// Step 1b: teste Go direto do caminho de unwind. Monta um frame a mao via
// callPreparedClosure com um map como argumento por valor, forca um
// unwindTo com erro (sem passar pelo retorno normal OP_RETURN) e confere
// que o release do parametro aconteceu mesmo assim.
func TestParamReleaseOnUnwindDirect(t *testing.T) {
	machine := New()
	m := value.NewMap()

	fn := &value.ObjFunction{
		Name:   "boom",
		Arity:  1,
		Params: []value.ParamInfo{{IsRef: false, TypeName: "map[string, int]"}},
	}
	closure := &value.ObjClosure{Function: fn}

	// Monta a pilha manualmente: [callee, arg] como um OP_CALL deixaria.
	machine.push(value.Value{Type: value.VAL_FUNCTION, Obj: closure})
	machine.push(m)

	if ok, err := machine.callPreparedClosure(closure, 1, nil, 0); !ok {
		t.Fatalf("callPreparedClosure falhou: %v", err)
	}

	if got := value.OwnersCount(m); got != 1 {
		t.Fatalf("apos a chamada esperado Owners=1, veio %d", got)
	}

	machine.unwindTo(0, frameOutcome{Err: fmt.Errorf("erro forcado para testar unwind")})

	if got := value.OwnersCount(m); got != 0 {
		t.Fatalf("apos unwind esperado Owners=0 (release rodou), veio %d", got)
	}
}

// f(m) chama g(m) chama probe_during(m); apos tudo, probe_after. during >= 2
// (dois frames retendo, um por chamada aninhada); after == during - 2.
func TestNestedByValueCallsStackAndUnstack(t *testing.T) {
	machine := New()
	var during, after int32
	machine.DefineNative("probe_during", func(args []value.Value) value.Value {
		during = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_after", func(args []value.Value) value.Value {
		after = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_during")
	markProbeReadonly(t, machine, "probe_after")

	src := `
func inner(m: map[string, int]) -> int
    probe_during(m)
    return 1
end

func outer(m: map[string, int]) -> int
    inner(m)
    return 1
end

func main()
    let m: map[string, int] = {"a": 1}
    outer(m)
    probe_after(m)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if during < 2 {
		t.Fatalf("durante a chamada aninhada esperado Owners >= 2, veio %d", during)
	}
	if after != during-2 {
		t.Fatalf("apos os dois retornos esperado %d, veio %d", during-2, after)
	}
}

// O let e um vinculo duravel do frame (spec §4.2): o slot local retem o
// composto assim que e criado. probe roda ainda dentro do frame de main,
// entao Owners deve refletir exatamente o retain do OP_OWN_LOCAL do let.
func TestLetBindRetainsComposite(t *testing.T) {
	machine := New()
	var count int32
	machine.DefineNative("probe", func(args []value.Value) value.Value {
		count = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe")

	src := `
func main()
    let m: map[string, int] = {"a": 1}
    probe(m)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperado Owners=1 (slot do let), veio %d", count)
	}
}

// a = b deve soltar o objeto que ocupava o slot de "a" e reter o objeto de
// "b" (que passa a ter dois donos: os slots de "a" e "b"). Captura-se o
// value.Value velho de "a" via native de sonda para medir seu OwnersCount
// depois da reatribuicao, quando o slot ja nao aponta mais para ele.
func TestAssignReleasesOldRetainsNew(t *testing.T) {
	machine := New()
	var oldA value.Value
	var ownersA1, ownersA2 int32
	machine.DefineNative("probe_a1", func(args []value.Value) value.Value {
		oldA = args[0]
		ownersA1 = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_a2", func(args []value.Value) value.Value {
		ownersA2 = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_a1")
	markProbeReadonly(t, machine, "probe_a2")

	src := `
func main()
    let a: map[string, int] = {"x": 1}
    let b: map[string, int] = {"y": 2}
    probe_a1(a)
    a = b
    probe_a2(a)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if ownersA1 != 1 {
		t.Fatalf("esperado Owners(a)=1 antes da reatribuicao, veio %d", ownersA1)
	}
	if ownersA2 != 2 {
		t.Fatalf("esperado Owners(b)=2 apos a=b (slots a e b), veio %d", ownersA2)
	}
	if got := value.OwnersCount(oldA); got != 0 {
		t.Fatalf("esperado Owners(objeto velho de a)=0 apos a reatribuicao, veio %d", got)
	}
}

// Task 4 (Step 1): let global de composto retem 1 dono; reatribuicao do
// global (g = h, ambos globais) solta o velho e o novo passa a ter 2 donos
// (os slots globais g e h).
func TestGlobalLetRetainsCompositeAndReassignmentSwaps(t *testing.T) {
	machine := New()
	var ownersG1, ownersG2 int32
	var oldG value.Value
	machine.DefineNative("probe_g1", func(args []value.Value) value.Value {
		oldG = args[0]
		ownersG1 = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_g2", func(args []value.Value) value.Value {
		ownersG2 = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_g1")
	markProbeReadonly(t, machine, "probe_g2")

	src := `
let g: map[string, int] = {"a": 1}
probe_g1(g)
let h: map[string, int] = {"b": 2}
g = h
probe_g2(g)`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if ownersG1 != 1 {
		t.Fatalf("esperado Owners(g)=1 apos let global, veio %d", ownersG1)
	}
	if ownersG2 != 2 {
		t.Fatalf("esperado Owners(g)=2 apos g=h (globais g e h), veio %d", ownersG2)
	}
	if got := value.OwnersCount(oldG); got != 0 {
		t.Fatalf("esperado Owners(objeto velho de g)=0 apos a reatribuicao, veio %d", got)
	}
}

// Task 4 (Step 1): funcao com parametro ref escreve no composto do
// chamador via *target = novo. O velho deve ser liberado, o novo retido.
func TestWriteViaRefReleasesOldRetainsNew(t *testing.T) {
	machine := New()
	var before, after int32
	var oldM value.Value
	machine.DefineNative("probe_before", func(args []value.Value) value.Value {
		oldM = args[0]
		before = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_after", func(args []value.Value) value.Value {
		after = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_before")
	markProbeReadonly(t, machine, "probe_after")

	src := `
func write_via_ref(target: ref map[string, int]) -> void
    *target = {"z": 9}
end

func main()
    let m: map[string, int] = {"a": 1}
    probe_before(m)
    write_via_ref(ref m)
    probe_after(m)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if before != 1 {
		t.Fatalf("esperado Owners(m)=1 antes da escrita via ref, veio %d", before)
	}
	if got := value.OwnersCount(oldM); got != 0 {
		t.Fatalf("esperado Owners(objeto velho de m)=0 apos escrita via ref, veio %d", got)
	}
	if after != 1 {
		t.Fatalf("esperado Owners(novo valor de m)=1 apos escrita via ref, veio %d", after)
	}
}

// Task 4 (Step 1): closure que captura um composto local sobrevive ao
// retorno do frame externo — o box do upvalue precisa reter o valor ao
// fechar (closeUpvalue), senao o retorno do frame externo (que solta o
// slot local) deixaria Owners em 0 enquanto a closure ainda vive.
func TestClosureCaptureSurvivesFrameReturn(t *testing.T) {
	machine := New()
	var owners int32
	machine.DefineNative("probe", func(args []value.Value) value.Value {
		owners = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe")

	src := `
func make_holder() -> func() -> map[string, int]
    let m: map[string, int] = {"a": 1}
    return func() -> map[string, int]
        probe(m)
        return m
    end
end

func main()
    let holder: func() -> map[string, int] = make_holder()
    holder()
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if owners < 1 {
		t.Fatalf("esperado Owners(capturado) >= 1 apos retorno do frame externo (box retem), veio %d", owners)
	}
}

// Task 5 (Step 1): OP_ARRAY retem cada elemento como dono duravel (ao lado
// do MarkShared existente, que fica ate a Task 8). x tem dois donos apos
// virar elemento de arr: o slot do let e o elemento do array.
func TestArrayLiteralRetainsSharedElement(t *testing.T) {
	machine := New()
	var owners int32
	machine.DefineNative("probe", func(args []value.Value) value.Value {
		owners = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe")

	src := `
func main()
    let x: map[string, int] = {"a": 1}
    let arr: map[string, int][] = [x]
    probe(x)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if owners != 2 {
		t.Fatalf("esperado Owners(x)=2 (slot de x + elemento do array), veio %d", owners)
	}
}

// Task 5 (Step 1): OP_SET_INDEX (array) libera o elemento velho e retem o
// novo. arr nao esta compartilhado (literal fresco), entao OP_GET_LOCAL_MUT
// nao clona — o teste isola exatamente o par retain/release do proprio
// OP_SET_INDEX.
func TestArraySetIndexReleasesOldRetainsNew(t *testing.T) {
	machine := New()
	var oldVal value.Value
	var ownersOldBefore, ownersNewAfter int32
	machine.DefineNative("probe_old", func(args []value.Value) value.Value {
		oldVal = args[0]
		ownersOldBefore = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_new", func(args []value.Value) value.Value {
		ownersNewAfter = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_old")
	markProbeReadonly(t, machine, "probe_new")

	src := `
func main()
    let x: map[string, int] = {"a": 1}
    let y: map[string, int] = {"b": 2}
    let arr: map[string, int][] = [x]
    probe_old(x)
    arr[0] = y
    probe_new(y)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if ownersOldBefore != 2 {
		t.Fatalf("esperado Owners(x)=2 antes de arr[0]=y (slot x + elemento), veio %d", ownersOldBefore)
	}
	if got := value.OwnersCount(oldVal); got != 1 {
		t.Fatalf("esperado Owners(x)=1 apos arr[0]=y (elemento liberado, slot x permanece), veio %d", got)
	}
	if ownersNewAfter != 2 {
		t.Fatalf("esperado Owners(y)=2 apos arr[0]=y (slot y + elemento), veio %d", ownersNewAfter)
	}
}

// Task 5 (Step 1): OP_SET_INDEX (map) sobre chave existente libera o valor
// velho e retem o novo. m nao esta compartilhado, entao GET_LOCAL_MUT nao
// clona — isola o par retain/release do proprio OP_SET_INDEX (branch map).
func TestMapSetIndexReleasesOldOnExistingKey(t *testing.T) {
	machine := New()
	var oldVal value.Value
	var ownersOldBefore, ownersNewAfter int32
	machine.DefineNative("probe_old", func(args []value.Value) value.Value {
		oldVal = args[0]
		ownersOldBefore = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_new", func(args []value.Value) value.Value {
		ownersNewAfter = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_old")
	markProbeReadonly(t, machine, "probe_new")

	src := `
func main()
    let x: map[string, int] = {"a": 1}
    let y: map[string, int] = {"b": 2}
    let m: map[string, map[string, int]] = {"k": x}
    probe_old(x)
    m["k"] = y
    probe_new(y)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if ownersOldBefore != 2 {
		t.Fatalf(`esperado Owners(x)=2 antes de m["k"]=y (slot x + valor no map), veio %d`, ownersOldBefore)
	}
	if got := value.OwnersCount(oldVal); got != 1 {
		t.Fatalf(`esperado Owners(x)=1 apos m["k"]=y (valor liberado do map, slot x permanece), veio %d`, got)
	}
	if ownersNewAfter != 2 {
		t.Fatalf(`esperado Owners(y)=2 apos m["k"]=y (slot y + valor no map), veio %d`, ownersNewAfter)
	}
}

// Task 5 (Step 1): OP_SET_PROPERTY libera o campo velho e retem o novo. A
// 1a atribuicao clona box (Box(x) nao e reconhecido como "fresco" pelo
// compilador — so array/map/zeros literal sao — entao o let marca o
// resultado do construtor Shared, e a base "box" e clonada via
// OP_GET_LOCAL_MUT). A 2a atribuicao roda com box ja unshared (o clone
// nasce sem o bit ligado), isolando o par retain/release do proprio
// OP_SET_PROPERTY sem a clonagem do caminho MUT interferir na contagem.
func TestSetPropertyReleasesOldRetainsNew(t *testing.T) {
	machine := New()
	var oldFieldVal value.Value
	var ownersOldBefore, ownersNewAfter int32
	machine.DefineNative("probe_old", func(args []value.Value) value.Value {
		oldFieldVal = args[0]
		ownersOldBefore = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_new", func(args []value.Value) value.Value {
		ownersNewAfter = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_old")
	markProbeReadonly(t, machine, "probe_new")

	src := `
struct Box
    value: map[string, int]
end

func main()
    let x: map[string, int] = {"a": 1}
    let y1: map[string, int] = {"y1": 1}
    let y2: map[string, int] = {"y2": 2}
    let box: Box = Box(x)
    box.value = y1
    probe_old(y1)
    box.value = y2
    probe_new(y2)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if ownersOldBefore != 2 {
		t.Fatalf("esperado Owners(y1)=2 apos a 1a atribuicao (slot + campo), veio %d", ownersOldBefore)
	}
	if got := value.OwnersCount(oldFieldVal); got != 1 {
		t.Fatalf("esperado Owners(y1)=1 apos a 2a atribuicao liberar o campo, veio %d", got)
	}
	if ownersNewAfter != 2 {
		t.Fatalf("esperado Owners(y2)=2 apos a 2a atribuicao (slot + campo), veio %d", ownersNewAfter)
	}
}

// Task 5 (Step 1): construtor de struct (callPreparedValue) retem cada
// argumento ao lado do MarkShared existente — o campo e um slot duravel.
func TestStructConstructorRetainsFieldArgument(t *testing.T) {
	machine := New()
	var owners int32
	machine.DefineNative("probe", func(args []value.Value) value.Value {
		owners = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe")

	src := `
struct Box
    value: map[string, int]
end

func main()
    let m: map[string, int] = {"a": 1}
    let box: Box = Box(m)
    probe(m)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if owners != 2 {
		t.Fatalf("esperado Owners(m)=2 (slot de m + campo do struct), veio %d", owners)
	}
}

// Task 5 (Step 1): copyValue retem cada filho clonado (ao lado do
// MarkShared existente). arr e compartilhado via alias; arr[1]=repl forca o
// clone de arr (OP_GET_LOCAL_MUT) mas so sobrescreve o indice 1 — "child"
// (indice 0) fica intocado e deve ganhar exatamente +1 dono vindo do clone.
func TestCloneOnMutationRetainsUntouchedChild(t *testing.T) {
	machine := New()
	var before, after int32
	machine.DefineNative("probe_before", func(args []value.Value) value.Value {
		before = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_after", func(args []value.Value) value.Value {
		after = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_before")
	markProbeReadonly(t, machine, "probe_after")

	src := `
func main()
    let child: map[string, int] = {"a": 1}
    let other: map[string, int] = {"b": 2}
    let arr: map[string, int][] = [child, other]
    let alias: map[string, int][] = arr
    probe_before(child)
    let repl: map[string, int] = {"c": 3}
    arr[1] = repl
    probe_after(child)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if before != 2 {
		t.Fatalf("esperado Owners(child)=2 antes do clone (slot + elemento), veio %d", before)
	}
	if after != before+1 {
		t.Fatalf("esperado Owners(child) crescer +1 apos o clone de arr (filho intocado herda dono do clone), before=%d after=%d", before, after)
	}
}

// Task 5 (Step 1): caminho MUT (OP_GET_INDEX_MUT, branch array) — quando
// a[0] esta Shared (marcado pelo proprio OP_ARRAY), a mutacao a[0].x=9
// clona o elemento, grava o clone de volta em Elements[0] com retain, e
// libera a instancia velha. Auditado por contagem na instancia velha
// (capturada antes da mutacao) e na nova (lida depois).
func TestMutIndexClonePathRetainsWriteBackReleasesOld(t *testing.T) {
	machine := New()
	var oldInst value.Value
	var ownersOldBefore, ownersNewAfter int32
	machine.DefineNative("probe_before", func(args []value.Value) value.Value {
		oldInst = args[0]
		ownersOldBefore = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_after", func(args []value.Value) value.Value {
		ownersNewAfter = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_before")
	markProbeReadonly(t, machine, "probe_after")

	src := `
struct P
    x: int
end

func main()
    let a: P[] = [P(1)]
    probe_before(a[0])
    a[0].x = 9
    probe_after(a[0])
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if ownersOldBefore != 1 {
		t.Fatalf("esperado Owners(a[0])=1 antes da mutacao (retain do elemento no OP_ARRAY), veio %d", ownersOldBefore)
	}
	if got := value.OwnersCount(oldInst); got != 0 {
		t.Fatalf("esperado Owners(instancia velha)=0 apos o clone gravado de volta em OP_GET_INDEX_MUT, veio %d", got)
	}
	if ownersNewAfter != 1 {
		t.Fatalf("esperado Owners(clone gravado em a[0])=1 apos a mutacao, veio %d", ownersNewAfter)
	}
}
