package vm

import (
	"fmt"
	"testing"
	"time"

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

// TestFrameReleaseTargetsRecordedObjectNotSlotOccupant reproduz o cenario
// unsound do desenho por indice (Owned []int + release lendo vm.stack[slot]):
// um local de bloco possuido "morre" sem drop (OP_POP simples no fim do
// while/if) e o slot e reusado por um temporario NUNCA retido (ex.:
// OP_GET_LOCAL de outra variavel). O release do fim do frame nao pode tocar
// o ocupante atual do slot — so o objeto que ele de fato reteve.
func TestFrameReleaseTargetsRecordedObjectNotSlotOccupant(t *testing.T) {
	machine := New()
	blockLocal := value.NewArray([]value.Value{value.NewInt(1)})
	bystander := value.NewMap()
	value.Retain(bystander) // dono duravel em outro lugar (ex.: global)

	fn := &value.ObjFunction{
		Name:   "boom",
		Arity:  1,
		Params: []value.ParamInfo{{IsRef: false, TypeName: "int[]"}},
	}
	closure := &value.ObjClosure{Function: fn}

	// Monta a pilha manualmente: [callee, arg] como um OP_CALL deixaria.
	// O param bind chama ownSlot internamente (callPreparedClosure), que
	// registra blockLocal como o objeto possuido no slot do parametro —
	// exatamente como um `let` de bloco registraria via OP_OWN_LOCAL.
	machine.push(value.Value{Type: value.VAL_FUNCTION, Obj: closure})
	machine.push(blockLocal)

	if ok, err := machine.callPreparedClosure(closure, 1, nil, 0); !ok {
		t.Fatalf("callPreparedClosure falhou: %v", err)
	}

	if got := value.OwnersCount(blockLocal); got != 1 {
		t.Fatalf("apos o bind esperado Owners=1 para blockLocal, veio %d", got)
	}

	// Simula o fim do bloco (OP_POP sem drop) seguido de reuso do slot por
	// um temporario nunca retido — sobrescrita crua de vm.stack, sem passar
	// por ownSlot (e assim sem atualizar/duplicar a entrada em Owned).
	slot := machine.currentFrame.LocalBase + 1
	machine.stack[slot] = bystander

	machine.unwindTo(0, frameOutcome{Err: fmt.Errorf("erro forcado para testar unwind")})

	if got := value.OwnersCount(bystander); got != 1 {
		t.Fatalf("temporario nunca retido foi liberado (dec a menos): Owners=%d, esperado 1", got)
	}
	if got := value.OwnersCount(blockLocal); got != 0 {
		t.Fatalf("objeto gravado deveria ter sido liberado no fim do frame: Owners=%d", got)
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
	// Task 2b: esta leitura acontece apos interpretVMSource retornar, ou
	// seja, apos main() ja ter retornado e seu frame ja ter sido liberado.
	// Antes da Task 2b, o slot do "let x" ficava preso (guard `slot <
	// vm.stackTop` pulava o release por indice apos o OP_POP de fim de
	// escopo do compilador) — agora o release e do objeto gravado, sem esse
	// guard, entao o slot de x tambem libera no fim do frame e so resta o
	// release do elemento (ja contado acima): total 0.
	if got := value.OwnersCount(oldVal); got != 0 {
		t.Fatalf("esperado Owners(x)=0 apos arr[0]=y liberar o elemento E main() retornar (Task 2b libera o slot do let no fim do frame), veio %d", got)
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
	// Task 2b: mesma observacao de TestArraySetIndexReleasesOldRetainsNew —
	// esta leitura acontece apos main() ja ter retornado; o slot do "let x"
	// tambem libera no fim do frame agora (sem o guard de stackTop), entao
	// so resta o release do valor do map (ja contado acima): total 0.
	if got := value.OwnersCount(oldVal); got != 0 {
		t.Fatalf(`esperado Owners(x)=0 apos m["k"]=y liberar o valor E main() retornar (Task 2b libera o slot do let no fim do frame), veio %d`, got)
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
	// Task 2b: esta leitura acontece apos interpretVMSource retornar, ou
	// seja, apos main() ja ter retornado por completo — o release em massa
	// de finalizeCurrentFrame ja rodou para o frame de main. Antes da Task
	// 2b, o guard `slot < vm.stackTop` fazia esse release pular TODOS os
	// locais de main (o epilogo de fim de escopo do compilador ja tinha
	// poppado x/y1/y2/box do stack antes do OP_RETURN, entao seus slots
	// pareciam "mortos" para o guard) — o retain do "let y1" ficava preso
	// para sempre (Owners=1 residual). A Task 2b libera por OBJETO GRAVADO,
	// sem esse guard, entao o slot do "let y1" agora e devidamente liberado
	// no fim do frame de main: so resta o release do campo (ja contado
	// acima), e o total cai a 0.
	if got := value.OwnersCount(oldFieldVal); got != 0 {
		t.Fatalf("esperado Owners(y1)=0 apos a 2a atribuicao liberar o campo E main() retornar (Task 2b libera o slot do let no fim do frame), veio %d", got)
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
// a[0] esta compartilhado, a mutacao a[0].x=9 clona o elemento, grava o clone
// de volta em Elements[0] com retain, e libera a instancia velha. Auditado
// por contagem na instancia velha (capturada antes da mutacao) e na nova
// (lida depois).
//
// Task 7: antes da chave bastava `let a: P[] = [P(1)]` — o MarkShared que o
// OP_ARRAY faz no elemento era suficiente para o caminho de clone disparar.
// Depois da chave (spec docs/superpowers/specs/
// 2026-08-17-cow-rc-uniqueness-design.md §3), "compartilhado" e Owners > 1, e
// um elemento com um unico dono (o proprio array) e unico por definicao —
// mutar em lugar e correto e nao clona. Para continuar exercitando o caminho
// de clone, o programa agora da DOIS donos duraveis a instancia: o slot do
// `let p` e o elemento de `a`.
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
    let p: P = P(1)
    let a: P[] = [p]
    probe_before(a[0])
    a[0].x = 9
    probe_after(a[0])
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if ownersOldBefore != 2 {
		t.Fatalf("esperado Owners(a[0])=2 antes da mutacao (slot do let p + elemento retido no OP_ARRAY), veio %d", ownersOldBefore)
	}
	if got := value.OwnersCount(oldInst); got != 0 {
		t.Fatalf("esperado Owners(instancia velha)=0 apos o clone gravado de volta em OP_GET_INDEX_MUT, veio %d", got)
	}
	if ownersNewAfter != 1 {
		t.Fatalf("esperado Owners(clone gravado em a[0])=1 apos a mutacao, veio %d", ownersNewAfter)
	}
}

// Task 6 (Step 1): append retem o item ao lado do MarkShared existente; pop
// libera o elemento removido depois de tira-lo do array.
func TestAppendRetainsItemPopReleasesRemoved(t *testing.T) {
	machine := New()
	item := value.NewMap()
	storedArray := value.NewArray(nil)
	arrayRef := pointerReference(&storedArray)

	before := value.OwnersCount(item)
	callBuiltin(t, machine, "append", arrayRef, item)
	if got := value.OwnersCount(item); got != before+1 {
		t.Fatalf("esperado Owners=%d apos append, veio %d", before+1, got)
	}

	popped := callBuiltin(t, machine, "pop", arrayRef)
	if popped.Obj != item.Obj {
		t.Fatalf("pop retornou objeto diferente do item empilhado")
	}
	if got := value.OwnersCount(item); got != before {
		t.Fatalf("esperado Owners=%d apos pop remover o elemento, veio %d", before, got)
	}
}

// Task 6 (Step 1): delete busca o valor velho antes de remover a chave e so
// libera se a chave de fato existia (chave ausente nao mexe em Owners).
func TestDeleteReleasesValueOnlyWhenKeyExisted(t *testing.T) {
	machine := New()
	oldVal := value.NewMap()
	// simula a retencao duravel que o proprio mapa ja teria feito ao guardar
	// oldVal (Task 5 / OP_MAP) — aqui construimos o mapa via helper de teste
	// (fora do bytecode), entao retemos manualmente para ter algo para o
	// delete soltar.
	value.Retain(oldVal)

	storedMap := value.NewMap()
	mapObject := storedMap.Obj.(*value.ObjMap)
	setTestMap(mapObject, "k", oldVal)
	mapRef := pointerReference(&storedMap)

	before := value.OwnersCount(oldVal)
	callBuiltin(t, machine, "delete", mapRef, value.NewString("k"))
	if got := value.OwnersCount(oldVal); got != before-1 {
		t.Fatalf("esperado Owners=%d apos delete da chave existente, veio %d", before-1, got)
	}

	missing := value.NewMap()
	value.Retain(missing)
	beforeMissing := value.OwnersCount(missing)
	setTestMap(mapObject, "other", missing)
	afterSet := value.OwnersCount(missing)
	callBuiltin(t, machine, "delete", mapRef, value.NewString("chave-inexistente"))
	if got := value.OwnersCount(missing); got != afterSet {
		t.Fatalf("delete de chave ausente nao deveria alterar Owners de outro valor: antes=%d depois=%d", beforeMissing, got)
	}
}

// Task 6 (Step 1): chan_send retem o valor ao lado do MarkShared existente
// (o buffer e durave); chan_recv libera apos um recebimento bem-sucedido —
// o valor saiu do buffer.
func TestChanSendRetainsChanRecvReleasesOnSuccess(t *testing.T) {
	machine := New()
	item := value.NewMap()
	ch := value.NewChannel(1)

	before := value.OwnersCount(item)
	callBuiltin(t, machine, "chan_send", ch, item)
	if got := value.OwnersCount(item); got != before+1 {
		t.Fatalf("esperado Owners=%d apos chan_send, veio %d", before+1, got)
	}

	got := callBuiltin(t, machine, "chan_recv", ch)
	if got.Obj != item.Obj {
		t.Fatalf("chan_recv retornou valor diferente do enviado")
	}
	if got := value.OwnersCount(item); got != before {
		t.Fatalf("esperado Owners=%d apos chan_recv receber com sucesso, veio %d", before, got)
	}
}

// Task 6 (Step 1): o caminho !ok de chan_recv (canal fechado e vazio) nao
// pode liberar nada — nao ha valor recebido para soltar.
func TestChanRecvOnClosedEmptyChannelDoesNotRelease(t *testing.T) {
	machine := New()
	item := value.NewMap()
	ch := value.NewChannel(1)
	callBuiltin(t, machine, "chan_send", ch, item)
	first := callBuiltin(t, machine, "chan_recv", ch)
	if first.Obj != item.Obj {
		t.Fatalf("primeiro recv nao retornou o item esperado")
	}
	afterFirstRecv := value.OwnersCount(item)

	chObj := ch.Obj.(*value.ObjChannel)
	chObj.Lock.Lock()
	close(chObj.Chan)
	chObj.Closed = true
	chObj.Lock.Unlock()

	second := callBuiltin(t, machine, "chan_recv", ch)
	if second.Type != value.VAL_NULL {
		t.Fatalf("esperado null ao receber de canal fechado e vazio, veio %#v", second)
	}
	if got := value.OwnersCount(item); got != afterFirstRecv {
		t.Fatalf("recv em canal fechado/vazio nao deveria alterar Owners: antes=%d depois=%d", afterFirstRecv, got)
	}
}

// Task 6 (Step 1): spawn e a excecao onde retain e registro no frame.Owned
// ficam em pontos separados do codigo (o retain acontece no loop de push,
// sincrono, antes do goroutine ser lancado; o registro no frame manual da
// thread e o que causa o release quando aquele frame termina). Como o
// retain+registro sao sincronos (rodam dentro do proprio native "spawn",
// antes do "go func(){...}()"), medimos logo apos spawn.Invoke retornar —
// sem precisar sincronizar com a goroutine para a medida "durante".
func TestSpawnRetainsArgSynchronouslyAndReleasesWhenThreadFrameEnds(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func worker(m: map[string, int])
end`); err != nil {
		t.Fatal(err)
	}
	workerFn, _ := machine.GetGlobal("worker")
	m := value.NewMap()
	before := value.OwnersCount(m)

	spawn := requireBuiltin(t, machine, "spawn")
	if _, err := spawn.Invoke(machine, []value.Value{workerFn, m}); err != nil {
		t.Fatal(err)
	}
	if got := value.OwnersCount(m); got != before+1 {
		t.Fatalf("esperado Owners=%d logo apos spawn (retain sincrono no push), veio %d", before+1, got)
	}

	deadline := time.Now().Add(statefulBuiltinTimeout)
	for time.Now().Before(deadline) {
		if value.OwnersCount(m) == before {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := value.OwnersCount(m); got != before {
		t.Fatalf("esperado Owners=%d apos a thread da spawn terminar (frame.Owned liberado), veio %d", before, got)
	}
}

// Task 6 (Step 1): spawn_task retem o argumento na preparacao (captura,
// sincrona, antes do goroutine da task rodar) e libera quando a task
// termina (mirror do padrao de defer) — a asserção "durante" tambem nao
// precisa de sincronizacao porque o retain acontece antes do "go".
func TestSpawnTaskCapturesArgSynchronouslyAndReleasesWhenTaskCompletes(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func worker(m: int[])
end`); err != nil {
		t.Fatal(err)
	}
	workerFn, _ := machine.GetGlobal("worker")
	// array (nao map): prepareTaskCall valida RuntimeType contra a
	// assinatura da funcao, e um value.NewMap() cru nao carrega o
	// RuntimeType que o checker exige para map[K,V]; um array construido
	// fora do bytecode passa nessa validacao (mesmo padrao usado em
	// TestPreparedTaskCallMarksValueParameterShared).
	m := value.NewArray([]value.Value{value.NewInt(1)})
	before := value.OwnersCount(m)

	spawnTask := requireBuiltin(t, machine, "spawn_task")
	handle, err := spawnTask.Invoke(machine, []value.Value{workerFn, m})
	if err != nil {
		t.Fatal(err)
	}
	if got := value.OwnersCount(m); got != before+1 {
		t.Fatalf("esperado Owners=%d logo apos spawn_task (captura sincrona em prepareTaskCall), veio %d", before+1, got)
	}

	taskAwait := requireBuiltin(t, machine, "task_await")
	if _, err := taskAwait.Invoke(machine, []value.Value{handle}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(statefulBuiltinTimeout)
	for time.Now().Before(deadline) {
		if value.OwnersCount(m) == before {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := value.OwnersCount(m); got != before {
		t.Fatalf("esperado Owners=%d apos a task terminar (release da captura), veio %d", before, got)
	}
}

// Task 6 (Step 1): defer captura o composto (+1) ao preparar a chamada
// adiada; depois que o defer roda (fim da funcao externa, invokePreparedCall
// completo), a captura e liberada (-1) — teste direto e isolado, sem
// misturar com o retain do proprio frame da funcao externa.
func TestDeferCapturesCompositeThenReleasesAfterInvocation(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func cleanup(m: map[string, int]) -> void
end`); err != nil {
		t.Fatal(err)
	}
	callee, ok := machine.GetGlobal("cleanup")
	if !ok {
		t.Fatal("global 'cleanup' nao encontrado")
	}
	m := value.NewMap()
	before := value.OwnersCount(m)

	prepared, err := machine.prepareDeferredCall(callee, []value.Value{m}, SourceLocation{})
	if err != nil {
		t.Fatalf("prepareDeferredCall: %v", err)
	}
	if got := value.OwnersCount(m); got != before+1 {
		t.Fatalf("esperado Owners=%d apos a captura do defer, veio %d", before+1, got)
	}

	if err := machine.invokePreparedCall(prepared); err != nil {
		t.Fatalf("invokePreparedCall: %v", err)
	}
	if got := value.OwnersCount(m); got != before {
		t.Fatalf("esperado Owners=%d apos o defer rodar (release da captura), veio %d", before, got)
	}
}

// Task 6 (Step 1): mesmo cenario acima, mas de ponta a ponta via programa
// Noxy real — confirma que o release da captura acontece junto com o
// unwind normal do frame externo (defer registrado, funcao externa
// retorna, defer roda, captura e liberada).
func TestDeferCaptureEndToEndReleasesAfterOuterFunctionReturns(t *testing.T) {
	machine := New()
	var capturedM value.Value
	var duringOwners int32
	machine.DefineNative("probe_during", func(args []value.Value) value.Value {
		capturedM = args[0]
		duringOwners = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_during")

	src := `
func cleanup(m: map[string, int]) -> void
    probe_during(m)
end

func outer() -> void
    let m: map[string, int] = {"a": 1}
    defer cleanup(m)
end

func main()
    outer()
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// durante a execucao do defer: slot do let em outer (ainda vivo — o
	// Deferred roda antes do release do frame.Owned de outer) + captura do
	// defer + parametro de cleanup (proprio frame retido via
	// callPreparedClosure) = 3.
	if duringOwners != 3 {
		t.Fatalf("esperado Owners=3 durante o defer, veio %d", duringOwners)
	}
	// Apos outer() retornar totalmente, o par do defer (captura +1 / release
	// -1) ja fechou em zero. Antes da Task 2b, sobrava 1: o proprio "let m"
	// de outer nunca soltava seu retain original, porque o OP_POP de fim de
	// bloco que o compilador emite para locais (BlockStatement.endScope)
	// roda ANTES do release em massa de finalizeCurrentFrame — sob o desenho
	// antigo (guard `slot < vm.stackTop`, release por INDICE), o slot ja nao
	// estava mais "vivo" quando o loop de release olhava para ele, entao o
	// release era pulado (gap pre-existente da Task 3, documentado como fora
	// do escopo daquela tarefa). A Task 2b fecha esse gap como consequencia
	// direta de trocar o release por indice por release do OBJETO GRAVADO
	// (sem guard de stackTop): o slot do "let m" de outer agora libera
	// normalmente no fim do frame, entao o total final e 0 — nao sobra mais
	// nada preso.
	if got := value.OwnersCount(capturedM); got != 0 {
		t.Fatalf("esperado Owners=0 (Task 2b libera o slot do let de outer no fim do frame, fechando o gap pre-existente da Task 3; o par do defer tambem fecha em zero), veio %d", got)
	}
}

// Task 6 (Step 1): native sem assinatura fora da allowlist retem
// permanentemente (conservador) — sem release em lugar nenhum. Diferente de
// TestUnlistedNativeStillMarksArgs (que audita so o Shared bit/clone), este
// teste audita o contador Owners.
//
// Usamos um GLOBAL (nao um "let" local) de proposito: um global nunca passa
// por frame.Owned (funil proprio via OP_SET_GLOBAL, Task 4), entao o
// baseline e estavel e conhecido (1, igual
// TestGlobalLetRetainsCompositeAndReassignmentSwaps) sem depender de quando
// a funcao ao redor retorna. (Ate a Task 2b, um "let" local tambem exigia
// esse cuidado: o OP_POP de fim de bloco emitido pelo compilador rodava
// ANTES do release em massa de finalizeCurrentFrame, e o guard por indice
// entao vigente pulava o release do slot — gap pre-existente da Task 3,
// fechado pela Task 2b ao trocar o release por indice por release do objeto
// gravado.)
func TestUnlistedNativeRetainsArgPermanently(t *testing.T) {
	machine := New()
	var captured value.Value
	machine.DefineNative("test_observe_rc", func(args []value.Value) value.Value {
		captured = args[0]
		return value.NewNull()
	})
	// Sem markProbeReadonly de proposito: queremos o comportamento default
	// (native fora da allowlist, ReadonlyArgs=false).
	src := `
let g: map[string, int] = {"a": 1}
test_observe_rc(g)`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// 1 do retain do global (permanece pelo programa todo) + 1 da retencao
	// permanente do native sem assinatura.
	if got := value.OwnersCount(captured); got != 2 {
		t.Fatalf("esperado Owners=2 (1 do global + 1 da retencao permanente do native), veio %d", got)
	}
}

// Task 7 (Step 1): teste de aceite da CHAVE — com a unicidade decidida pelo
// contador de donos (spec §3), um laco que passa um composto POR VALOR a um
// helper nao pode mais clonar por iteracao. Antes da chave, o bit sticky
// Shared ligado pelo bind do parametro por valor ficava para sempre: cada
// put() reclonava db/state/payloads (3 clones por iteracao, ~600 no total, e
// o clone do map cresce com N => quadratico). Depois da chave, o retain do
// parametro e liberado no fim do frame do helper, os donos voltam a 1 e as
// mutacoes seguintes mutam no lugar: O(1) clones no laco inteiro.
func TestByValueCallLoopClonesO1AfterFlip(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	ResetCloneCount()
	src := `
struct State
    payloads: map[string, string]
end

struct Db
    state: State
end

func helper(db: Db) -> int
    return 1
end

func put(db: ref Db, key: string, val: string) -> void
    let x: int = helper(db)
    db.state.payloads[key] = val
end

func main()
    let payloads: map[string, string] = {}
    let db: Db = Db(State(payloads))
    let i: int = 0
    while i < 200 do
        put(ref db, f"k{i}", "v")
        i = i + 1
    end
    test_report(length(keys(db.state.payloads)))
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	if reported.Type != value.VAL_INT || reported.AsInt != 200 {
		t.Fatalf("programa incorreto: %#v", reported)
	}
	if n := CloneCountValue(); n > 8 {
		t.Fatalf("laco por-valor deveria clonar O(1); clonou %d", n)
	}
}

// Task 7 (fallout da chave): um vinculo de tipo `ref` e um EMPRESTIMO
// (borrow), nunca um dono duravel. Parametros ref ja eram pulados no bind
// (callPreparedClosure), mas o `let x: ref T` e o rebind `x = y` de um local
// ref contavam posse — over-count que so ficou observavel depois da chave: um
// no de lista alcancado por `current = current.proximo` passava a ter 2 donos
// (o campo `proximo` do no anterior + o slot ref emprestado), IsShared virava
// true e a mutacao `current.proximo = null` clonava o no em vez de muta-lo no
// lugar, perdendo a escrita (noxy_examples/stack.nx e linked_list.nx
// divergiam do baseline). Este teste e de SEMANTICA: a mutacao atraves do
// emprestimo tem de ser visivel na lista original.
func TestRefLocalBindingIsBorrowNotOwner(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Node
    valor: int
    proximo: ref Node
end

func _append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = Node(valor, null)
    else
        _append(node.proximo, valor)
    end
end

func pop(node: ref Node) -> int
    let current: ref Node = node
    while current.proximo.proximo != null do
        current = current.proximo
    end
    let valor: int = current.proximo.valor
    current.proximo = null
    return valor
end

func count(node: ref Node) -> int
    let n: int = 0
    let current: ref Node = node
    while current != null do
        n = n + 1
        current = current.proximo
    end
    return n
end

func main()
    let head: Node = Node(0, null)
    _append(head, 20)
    _append(head, 30)
    let popped: int = pop(head)
    test_report(count(head) * 100 + popped)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// 2 nos restantes (0, 20) * 100 + valor removido (30).
	if reported.Type != value.VAL_INT || reported.AsInt != 230 {
		t.Fatalf("mutacao atraves do emprestimo ref se perdeu (clone indevido): esperado 230, veio %#v", reported)
	}
}

// Task 7 (fallout da chave): mesma regra do teste acima nos outros dois
// funis de vinculo ref — global de tipo ref (OP_SET_GLOBAL_BORROW) e local
// ref capturado por uma closure que escapa do frame (closeUpvalue so migra a
// posse quando o slot era possuido). Sem os dois, o objeto emprestado ganhava
// um dono a mais e a mutacao atraves do emprestimo clonava, perdendo a
// escrita. Teste de SEMANTICA: a lista original tem de encolher.
func TestRefGlobalAndCapturedRefLocalAreBorrows(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Node
    valor: int
    proximo: ref Node
end

func _append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = Node(valor, null)
    else
        _append(node.proximo, valor)
    end
end

func count(node: ref Node) -> int
    let n: int = 0
    let cur: ref Node = node
    while cur != null do
        n = n + 1
        cur = cur.proximo
    end
    return n
end

func make_dropper(node: ref Node) -> func() -> int
    let u: ref Node = node.proximo
    return func() -> int
        u.proximo = null
        return 1
    end
end

let head_a: Node = Node(0, null)
_append(head_a, 20)
_append(head_a, 30)
let g: ref Node = head_a.proximo
g.proximo = null

let head_b: Node = Node(0, null)
_append(head_b, 20)
_append(head_b, 30)
let dropper: func() -> int = make_dropper(head_b)
let done: int = dropper()

test_report(count(head_a) * 10 + count(head_b))`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// as duas listas encolhem de 3 para 2 nos: 2*10 + 2 = 22.
	if reported.Type != value.VAL_INT || reported.AsInt != 22 {
		t.Fatalf("mutacao atraves de emprestimo ref (global / capturado) se perdeu: esperado 22, veio %#v", reported)
	}
}

// Task 7 (fallout da chave, round 2 — CRITICO achado em review): um slot `ref`
// capturado por closure empresta, e a CAIXA do upvalue herda o emprestimo. Sem
// isso, os funis de escrita da caixa soltavam um objeto que ela nunca reteve
// (dec a menos): OP_SET_UPVALUE (rebind `u = u.proximo`) e OP_GET_UPVALUE_MUT
// (mutacao `u.valor = 77`). O mesmo furo existia no OP_GET_LOCAL_MUT sobre um
// slot local `ref` emprestado.
//
// A forma exige DOIS donos reais vivos no no mutado — o campo `proximo` do no
// anterior E um vinculo por valor (`let second: Node = head.proximo`). Com um
// dono so, o Release a mais e absorvido pelo clamp em zero de value.Release e
// o bug fica invisivel (foi exatamente o ponto cego de
// TestRefGlobalAndCapturedRefLocalAreBorrows). Com dois, o dec a menos leva
// 2 -> 1, o no passa a parecer unico, `second.valor = 99` muta no lugar em vez
// de clonar (CoW) e a escrita vaza para head.proximo.
//
// Oraculo: o binario do merge-base responde 20 nos tres cenarios (o valor
// original do no 20 nunca e alterado, porque toda mutacao passa por clone).
func TestCapturedAndBorrowedRefSlotsNeverReleaseWhatTheyDoNotOwn(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Node
    valor: int
    proximo: ref Node
end

func _append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = Node(valor, null)
    else
        _append(node.proximo, valor)
    end
end

// A: rebind do upvalue ref (OP_SET_UPVALUE sobre caixa emprestada).
func repro_a(head: ref Node) -> int
    let second: Node = head.proximo
    let u: ref Node = head.proximo
    let f: func() -> int = func() -> int
        u = u.proximo
        return 1
    end
    let ignored: int = f()
    second.valor = 99
    return head.proximo.valor
end

// B: mutacao atraves do upvalue ref (OP_GET_UPVALUE_MUT sobre caixa
// emprestada).
func repro_b(head: ref Node) -> int
    let second: Node = head.proximo
    let u: ref Node = head.proximo
    let f: func() -> int = func() -> int
        u.valor = 77
        return 1
    end
    let ignored: int = f()
    second.valor = 99
    return head.proximo.valor
end

// C: mutacao atraves de um local ref emprestado (OP_GET_LOCAL_MUT).
func repro_c(head: ref Node) -> int
    let second: Node = head.proximo
    let u: ref Node = head.proximo
    u.valor = 77
    second.valor = 99
    return head.proximo.valor
end

func main()
    let ha: Node = Node(0, null)
    _append(ha, 20)
    _append(ha, 30)
    let a: int = repro_a(ha)

    let hb: Node = Node(0, null)
    _append(hb, 20)
    _append(hb, 30)
    let b: int = repro_b(hb)

    let hc: Node = Node(0, null)
    _append(hc, 20)
    _append(hc, 30)
    let c: int = repro_c(hc)

    test_report(a * 10000 + b * 100 + c)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// 20 / 20 / 20 — exatamente o que o binario do merge-base responde.
	if reported.Type != value.VAL_INT || reported.AsInt != 202020 {
		t.Fatalf("escrita vazou por dec a menos em slot/caixa emprestada: esperado 202020 (A=20 B=20 C=20), veio %#v", reported)
	}
}

// Task 7 (fallout, round 2): mesma invariante auditada por CONTAGEM, nao por
// saida — o no compartilhado tem de continuar com os seus dois donos reais
// depois de um rebind do upvalue emprestado. Antes da correcao caia para 1.
func TestBorrowedUpvalueRebindKeepsOwnersOfSharedNode(t *testing.T) {
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
struct Node
    valor: int
    proximo: ref Node
end

func _append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = Node(valor, null)
    else
        _append(node.proximo, valor)
    end
end

func run(head: ref Node) -> int
    let second: Node = head.proximo
    probe_before(head.proximo)
    let u: ref Node = head.proximo
    let f: func() -> int = func() -> int
        u = u.proximo
        return 1
    end
    let ignored: int = f()
    probe_after(head.proximo)
    return 1
end

func main()
    let head: Node = Node(0, null)
    _append(head, 20)
    _append(head, 30)
    let ok: int = run(head)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// campo proximo de head + slot do `let second` = 2 donos reais.
	if before != 2 {
		t.Fatalf("esperado Owners(no compartilhado)=2 antes da closure, veio %d", before)
	}
	if after != before {
		t.Fatalf("rebind de upvalue emprestado soltou o que a caixa nunca reteve: Owners %d -> %d", before, after)
	}
}

// Task 7 (fallout, round 2 — Important 4): teste Go direto dos DOIS ramos do
// contrato de posse da caixa. Toda caixa nasce POSSUIDORA; so o
// OP_MARK_UPVALUE_BORROW (marca estatica emitida pelo compilador para slot de
// tipo `ref`) a torna emprestada, e fechar uma caixa emprestada NAO pode reter
// — e o que impede o par retain/release da caixa de existir e, por
// consequencia, o dec a menos nos funis de escrita dela.
//
// Round 3: a condicao NAO vem mais de frame.Owned. Ela errava nas duas
// direcoes — slot possuido com ocupante null/escalar na captura nao aparece na
// lista (retain devido pulado) e indice de slot reusado entre blocos irmaos
// deixa entrada morta (release indevido).
func TestBorrowedUpvalueBoxDoesNotRetainOnClose(t *testing.T) {
	machine := New()

	// possuido: um dono real em outro lugar (campo, global, slot do chamador).
	owned := value.NewMap()
	value.Retain(owned)
	machine.stack[0] = owned
	ownedSlot := &machine.stack[0]

	// emprestado: idem, mas o slot so empresta.
	borrowed := value.NewMap()
	value.Retain(borrowed)
	machine.stack[1] = borrowed
	borrowedSlot := &machine.stack[1]

	ownedBox := machine.captureUpvalue(ownedSlot)
	borrowedBox := machine.captureUpvalue(borrowedSlot)

	if ownedBox.IsBorrowed() || borrowedBox.IsBorrowed() {
		t.Fatal("toda caixa nasce possuidora — o emprestimo e marcado explicitamente")
	}
	// o que o OP_MARK_UPVALUE_BORROW faz, pelo tipo declarado do slot:
	borrowedBox.MarkBorrowed()
	if !borrowedBox.IsBorrowed() {
		t.Fatal("a marca de emprestimo deveria ter pegado")
	}
	if ownedBox.IsBorrowed() {
		t.Fatal("marcar uma caixa nao pode contaminar a outra")
	}

	machine.closeUpvalue(ownedSlot)
	machine.closeUpvalue(borrowedSlot)

	// ramo verdadeiro: a posse migra do slot para a caixa (+1; o release do
	// slot e responsabilidade do frame).
	if got := value.OwnersCount(owned); got != 2 {
		t.Fatalf("caixa possuida deveria reter ao fechar: esperado Owners=2, veio %d", got)
	}
	// ramo falso: a caixa empresta — nada de retain.
	if got := value.OwnersCount(borrowed); got != 1 {
		t.Fatalf("caixa emprestada nao pode reter ao fechar: esperado Owners=1, veio %d", got)
	}
}

// Task 7 (fallout da chave, round 3 — CRITICO achado em review): a condicao de
// EMPRESTIMO tem de ser ESTATICA (tipo declarado do slot, decidida pelo
// compilador). O round 2 a inferia em runtime varrendo frame.Owned
// (ownsSlotIndex), e essa resposta erra nas DUAS direcoes:
//
//   (a) under-count: um slot POSSUIDO (`let x: Node = null`) cujo ocupante era
//       null/escalar na hora da captura nunca entrou em frame.Owned (Retain
//       falha em nao-composto) — a caixa do upvalue era marcada emprestada por
//       engano e o retain que ela devia ao fechar/gravar era PULADO. O no ficava
//       com um dono a menos, parecia unico, e `picked.valor = 99` mutava no
//       lugar: a escrita vazava para head.proximo, quebrando a independencia do
//       vinculo por valor.
//   (b) dec a menos: indices de slot sao REUSADOS entre blocos irmaos e
//       frame.Owned nao e podado no fim do escopo — a entrada morta de um irmao
//       fazia um slot realmente emprestado parecer possuido, e o guard do
//       OP_GET_LOCAL_MUT era derrotado (release indevido, o bug original).
//
// Agora quem decide e o compilador: OP_GET_LOCAL_MUT_BORROW para local `ref` e
// OP_MARK_UPVALUE_BORROW (emitido apos o OP_CLOSURE) para upvalue `ref`.
// Oraculo dos dois cenarios, conferido no binario do merge-base: 20.
func TestBorrowConditionIsStaticNotInferredFromOwnedList(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Node
    valor: int
    proximo: ref Node
end

func _append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = Node(valor, null)
    else
        _append(node.proximo, valor)
    end
end

func get_second(h: ref Node) -> Node
    return h.proximo
end

// (a) slot POSSUIDO que estava null na captura, escrito de dentro da closure
// por OP_SET_UPVALUE. A caixa possui: o retain e devido.
func repro_e(head: ref Node) -> int
    let picked: Node = null
    let f: func() -> int = func() -> int
        picked = get_second(head)
        return 1
    end
    let ignored: int = f()
    picked.valor = 99
    return head.proximo.valor
end

// (a2) controle: mesmo cenario com o slot ja composto na captura (entrada em
// frame.Owned existe). Antes e depois da correcao responde 20 — isola a causa.
func repro_e2(head: ref Node) -> int
    let picked: Node = Node(1, null)
    let f: func() -> int = func() -> int
        picked = get_second(head)
        return 1
    end
    let ignored: int = f()
    picked.valor = 99
    return head.proximo.valor
end

// (b) reuso de indice de slot entre blocos irmaos: o primeiro bloco registra o
// indice em frame.Owned, o segundo reaproveita o MESMO indice para um
// emprestimo (let u: ref Node).
func repro_f(head: ref Node) -> int
    let second: Node = head.proximo
    if 1 == 1 then
        let dead: Node = Node(7, null)
        let touch: int = dead.valor
    end
    if 1 == 1 then
        let u: ref Node = head.proximo
        u.valor = 77
    end
    second.valor = 99
    return head.proximo.valor
end

// (b2) controle: sem o bloco irmao morto, o indice nunca foi possuido.
func repro_f2(head: ref Node) -> int
    let second: Node = head.proximo
    if 1 == 1 then
        let u: ref Node = head.proximo
        u.valor = 77
    end
    second.valor = 99
    return head.proximo.valor
end

func main()
    let ha: Node = Node(0, null)
    _append(ha, 20)
    _append(ha, 30)
    let a: int = repro_e(ha)

    let hb: Node = Node(0, null)
    _append(hb, 20)
    _append(hb, 30)
    let b: int = repro_e2(hb)

    let hc: Node = Node(0, null)
    _append(hc, 20)
    _append(hc, 30)
    let c: int = repro_f(hc)

    let hd: Node = Node(0, null)
    _append(hd, 20)
    _append(hd, 30)
    let d: int = repro_f2(hd)

    test_report(a * 1000000 + b * 10000 + c * 100 + d)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// 20 / 20 / 20 / 20 — os quatro cenarios respondem o oraculo do merge-base.
	if reported.Type != value.VAL_INT || reported.AsInt != 20202020 {
		t.Fatalf("condicao de emprestimo saiu errada (esperado 20202020 = e/e2/f/f2 todos 20), veio %#v", reported)
	}
}

// Task 7 (round 3): contrapartida do teste acima — a escrita ATRAVES DE UM REF
// para um no com um UNICO dono duravel acontece NO LUGAR, e nao num clone. E o
// mesmo principio que torna as escritas via `ref Db` visiveis para o chamador
// (bench_typed_call_map / NoxyDB) e que faz o `pop` de lista encadeada
// funcionar; clonar ali seria justamente a escrita perdida corrigida nos
// rounds anteriores.
//
// Este teste ancora o RASTRO DE CONTAGEM, que e a invariante defensavel:
//
//   p1 = 1  o campo `proximo` do no anterior e o unico dono
//   p2 = 2  `*n = v` grava o no no slot por valor `x` — dois donos reais
//   p3 = 1  `x.valor = 99` clona por independencia (CoW): `x` passa a apontar o
//           clone e o no volta a ter um dono
//   p4 = 1  `u.valor = 77` atravessa um emprestimo e muta no lugar; nenhum dono
//           entra ou sai
//
// NOTA DE DIVERGENCIA CONSCIENTE: o binario pre-chave responde 50 aqui (soma
// 0+20+30), porque o `*n = v` ligava o bit sticky do no PARA SEMPRE e a
// mutacao via `u` clonava, perdendo a escrita. Com a unicidade por contagem o
// resultado e 107 (0+77+30). Nao e bug de contagem — o rastro acima foi medido
// e esta correto em todos os pontos; e exatamente o dead-share que a spec §3
// se propoe a eliminar (a mesma razao do ganho de 13x no
// bench_value_call_mutate). Nenhum exemplo do corpus muda (130/130).
func TestRefWriteToUniquelyOwnedNodeMutatesInPlace(t *testing.T) {
	machine := New()
	counts := map[string]int32{}
	for _, name := range []string{"p1", "p2", "p3", "p4"} {
		probeName := name
		machine.DefineNative("probe_"+probeName, func(args []value.Value) value.Value {
			counts[probeName] = value.OwnersCount(args[0])
			return value.NewNull()
		})
		markProbeReadonly(t, machine, "probe_"+probeName)
	}
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Node
    valor: int
    proximo: ref Node
end

func _append(node: ref Node, valor: int)
    if node.proximo == null then
        node.proximo = Node(valor, null)
    else
        _append(node.proximo, valor)
    end
end

func get_second(h: ref Node) -> Node
    return h.proximo
end

func setit(n: ref Node, v: Node)
    *n = v
end

func sum_list(head: ref Node) -> int
    let total: int = 0
    let cur: ref Node = head
    while cur != null do
        total = total + cur.valor
        cur = cur.proximo
    end
    return total
end

func run(head: ref Node) -> int
    let x: Node = null
    probe_p1(head.proximo)
    setit(ref x, get_second(head))
    probe_p2(head.proximo)
    x.valor = 99
    probe_p3(head.proximo)
    let u: ref Node = head.proximo
    u.valor = 77
    probe_p4(head.proximo)
    return sum_list(head)
end

func main()
    let h: Node = Node(0, null)
    _append(h, 20)
    _append(h, 30)
    test_report(run(h))
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	want := map[string]int32{"p1": 1, "p2": 2, "p3": 1, "p4": 1}
	for _, name := range []string{"p1", "p2", "p3", "p4"} {
		if counts[name] != want[name] {
			t.Fatalf("rastro de donos errado: %s=%d, esperado %d (rastro completo: p1=%d p2=%d p3=%d p4=%d)",
				name, counts[name], want[name], counts["p1"], counts["p2"], counts["p3"], counts["p4"])
		}
	}
	// 0 + 77 + 30: a escrita via ref alcancou o no da lista.
	if reported.Type != value.VAL_INT || reported.AsInt != 107 {
		t.Fatalf("escrita via ref para no de dono unico deveria mutar no lugar (soma 107), veio %#v", reported)
	}
}

// Task 7 (fallout da chave, rounds 3-4): a pergunta dos funis de escrita e
// "este slot RETEM o que guarda?" (Local.Owns, marcado exatamente onde o inc e
// emitido), e os tres consumidores decidem por ela: OP_GET_LOCAL_MUT(_BORROW),
// OP_SET_LOCAL(_BORROW) e OP_REF_LOCAL(_BORROW). No round 3 a variavel de
// for-each e o binding de case do select eram slots NAO-possuidores servidos
// pelos gemeos de emprestimo; o round 4 fechou a classe: esses binds agora
// RETEM desde o nascimento (OP_OWN_LOCAL por iteracao, spec §4.2 — "TODO
// composto"), e os nao-possuidores nomeados voltaram a ser exatamente os slots
// `ref`. Estes cenarios permanecem como ancoras de INDEPENDENCIA: nenhum
// caminho pode soltar o elemento do array que o slot nao retem (round 3) nem
// deixar de reter o que o slot guarda duravelmente (round 4).
//
// Oraculo de todos os cenarios, conferido no binario do merge-base: 1.
func TestNonOwningLocalBindsNeverReleaseWhatTheyNeverRetained(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Box
    v: int
end

func setit(n: ref Box, w: Box)
    *n = w
end

// A: variavel de for-each no caminho MUT (o cenario do review).
func a_foreach_mut() -> int
    let b: Box = Box(1)
    let arr: Box[] = [b]
    for item in arr do
        item.v = 99
    end
    b.v = 55
    return arr[0].v
end

// B: controle — o loop existe mas so le a variavel.
func b_foreach_readonly() -> int
    let b: Box = Box(1)
    let arr: Box[] = [b]
    let acc: int = 0
    for item in arr do
        acc = acc + item.v
    end
    b.v = 55
    return arr[0].v
end

// C: controle — sem loop nenhum.
func c_no_loop() -> int
    let b: Box = Box(1)
    let arr: Box[] = [b]
    b.v = 55
    return arr[0].v
end

// D: rebind da propria variavel de for-each (OP_SET_LOCAL sobre slot
// nao-possuidor soltaria o elemento do array).
func d_foreach_rebind() -> int
    let b: Box = Box(1)
    let arr: Box[] = [b]
    let other: Box = Box(2)
    for item in arr do
        item = other
    end
    b.v = 55
    return arr[0].v
end

// E: ref para a variavel de for-each, escrito via *n = w (round 4: o slot
// possui, a caixa nasce possuidora e o funil reaponta a entrada do frame).
func e_ref_to_foreach_var() -> int
    let b: Box = Box(1)
    let arr: Box[] = [b]
    for item in arr do
        setit(ref item, Box(9))
    end
    b.v = 55
    return arr[0].v
end

// F: binding de case do select mutado no corpo do case.
func f_select_binding_mut() -> int
    let b: Box = Box(1)
    let arr: Box[] = [b]
    let ch: any = make_chan(1)
    chan_send(ch, arr[0])
    when
        case got = chan_recv(ch) then
            got.v = 99
    end
    b.v = 55
    return arr[0].v
end

func main()
    let a: string = to_str(a_foreach_mut())
    let b: string = to_str(b_foreach_readonly())
    let c: string = to_str(c_no_loop())
    let d: string = to_str(d_foreach_rebind())
    let e: string = to_str(e_ref_to_foreach_var())
    let f: string = to_str(f_select_binding_mut())
    test_report(a + "|" + b + "|" + c + "|" + d + "|" + e + "|" + f)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	const want = "1|1|1|1|1|1"
	if reported.Type != value.VAL_OBJ || reported.Obj != want {
		t.Fatalf("independencia quebrada por dec a menos em slot nao-possuidor: esperado %q (a|b|c|d|e|f), veio %#v", want, reported.Obj)
	}
}

// Task 7 (round 4 — CRITICO 2, pre-existente e confirmado observavel): a escrita
// ATRAVES DE UM REF para um slot de pilha POSSUIDO soltava o ocupante velho no
// funil (references.go) mas deixava a entrada (slot, objeto) do frame nomeando o
// objeto VELHO — o release em massa do fim do frame o soltava DE NOVO. Dec a
// mais e a direcao insegura: o objeto velho, ainda vivo em outro dono (aqui dois
// globais), passava a parecer unico e a mutacao seguinte acontecia no lugar,
// vazando para o outro dono.
//
// Correcao (opcao (a) do review): retargetOwnedSlot reaponta a entrada do frame
// para o valor novo, entao o release do velho e pago agora pelo funil e o do
// novo pelo fim do frame — conta fechada, sem inflacao.
//
// Oraculo do merge-base: 1 (a escrita em ga[0] nao pode aparecer em gb[0]).
func TestRefWriteToOwnedSlotRetargetsFrameOwnedEntry(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Box
    v: int
end

let ga: Box[] = []
let gb: Box[] = []
let ha: Box[] = []
let hb: Box[] = []

func setit(n: ref Box, w: Box)
    *n = w
end

// escreve via ref no slot possuido de y depois de compartilhar y com dois
// globais: a entrada de posse do frame precisa passar a nomear o valor novo.
func build_with_ref_write()
    let y: Box = Box(1)
    ga = [y]
    gb = [y]
    setit(ref y, Box(9))
end

// controle: mesmo compartilhamento, sem a escrita via ref.
func build_plain()
    let y: Box = Box(1)
    ha = [y]
    hb = [y]
end

func main()
    build_with_ref_write()
    ga[0].v = 55

    build_plain()
    ha[0].v = 55

    test_report(to_str(gb[0].v) + "|" + to_str(hb[0].v))
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	const want = "1|1"
	if reported.Type != value.VAL_OBJ || reported.Obj != want {
		t.Fatalf("dec a mais pela entrada de posse obsoleta: esperado %q (com escrita via ref | controle), veio %#v", want, reported.Obj)
	}
}

// Task 7 (round 4 — CRITICO 2 do re-review): os binds de for-each e de case do
// select agora RETEM (spec §4.2: "slot de local ... TODO composto"), entao o
// rebind durável do slot passa pelo store CONTADO. Antes, `item = other` (e as
// formas via ref e via select) gravava o objeto de `other` no slot SEM inc
// (gemeo de emprestimo): o slot segurava o objeto duravelmente com um dono a
// menos, a mutacao seguinte via Owners==1 e escrevia NO LUGAR — vazando para o
// dono real (candidato respondia 7; o merge-base e o proprio `let` respondem 2).
//
// Oraculo do merge-base para os cinco cenarios: 2 (independencia por valor).
func TestForEachAndSelectBindingsOwnFromBirth(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Box
    v: int
end

func setit(n: ref Box, w: Box)
    *n = w
end

// U1: rebind da variavel de for-each com valor de outro dono vivo + mutacao.
func u1_rebind_then_mut() -> int
    let other: Box = Box(2)
    let arr: Box[] = [Box(1)]
    for item in arr do
        item = other
        item.v = 7
    end
    return other.v
end

// U2: escrita via ref para a variavel de for-each + mutacao.
func u2_refwrite_then_mut() -> int
    let other: Box = Box(2)
    let arr: Box[] = [Box(1)]
    for item in arr do
        setit(ref item, other)
        item.v = 7
    end
    return other.v
end

// U3: o mesmo padrao no binding de case do select.
func u3_select_rebind_then_mut() -> int
    let other: Box = Box(2)
    let ch: any = make_chan(1)
    chan_send(ch, Box(1))
    when
        case got = chan_recv(ch) then
            got = other
            got.v = 7
    end
    return other.v
end

// W1: referencia de semantica — o MESMO padrao num let (sempre respondeu 2).
func w1_let_rebind_then_mut() -> int
    let other: Box = Box(2)
    let x: Box = Box(1)
    x = other
    x.v = 7
    return other.v
end

// W3: o valor rebindado tambem vive num terceiro dono (elemento de array).
func w3_foreach_rebind_leak_to_array() -> int
    let other: Box = Box(2)
    let keep: Box[] = [other]
    let arr: Box[] = [Box(1)]
    for item in arr do
        item = other
        item.v = 7
    end
    return keep[0].v
end

func main()
    let u1: string = to_str(u1_rebind_then_mut())
    let u2: string = to_str(u2_refwrite_then_mut())
    let u3: string = to_str(u3_select_rebind_then_mut())
    let w1: string = to_str(w1_let_rebind_then_mut())
    let w3: string = to_str(w3_foreach_rebind_leak_to_array())
    test_report(u1 + "|" + u2 + "|" + u3 + "|" + w1 + "|" + w3)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	const want = "2|2|2|2|2"
	if reported.Type != value.VAL_OBJ || reported.Obj != want {
		t.Fatalf("rebind duravel sem retain (under-count) em bind de for-each/select: esperado %q (u1|u2|u3|w1|w3), veio %#v", want, reported.Obj)
	}
}

// Task 7 (round 4 — CRITICO 1 do re-review): a marca de emprestimo da caixa de
// upvalue e por TIPO DECLARADO (emitClosureUpvalues), e a variavel de for-each
// tem tipo nil — a caixa nasce POSSUIDORA. Enquanto o slot nao retinha, a
// closure que o capturava soltava (OP_SET_UPVALUE) ou trocava com release
// (OP_GET_UPVALUE_MUT) um elemento de array que ninguem reteve: dec a menos, o
// elemento parecia unico e a escrita em `b` vazava para arr[0] (candidato
// respondia 55|55). Com o slot possuidor desde o nascimento a marca por tipo
// fica correta POR CONSTRUCAO — sem mecanismo novo.
//
// Oraculo do merge-base: 1|1.
func TestCapturedForEachItemKeepsContainerElementIndependent(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Box
    v: int
end

// A: closure captura a variavel de for-each e muta via OP_GET_UPVALUE_MUT.
func a_closure_mut_foreach() -> int
    let b: Box = Box(1)
    let arr: Box[] = [b]
    for item in arr do
        let f: func = func() -> void
            item.v = 99
        end
        f()
    end
    b.v = 55
    return arr[0].v
end

// B: closure captura a variavel de for-each e REBINDA via OP_SET_UPVALUE.
func b_closure_rebind_foreach() -> int
    let b: Box = Box(1)
    let arr: Box[] = [b]
    let other: Box = Box(2)
    for item in arr do
        let f: func = func() -> void
            item = other
        end
        f()
    end
    b.v = 55
    return arr[0].v
end

func main()
    test_report(to_str(a_closure_mut_foreach()) + "|" + to_str(b_closure_rebind_foreach()))
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	const want = "1|1"
	if reported.Type != value.VAL_OBJ || reported.Obj != want {
		t.Fatalf("caixa possuidora sobre slot de for-each soltou o que ninguem reteve: esperado %q (mut|rebind), veio %#v", want, reported.Obj)
	}
}

// Task 7 (round 4): escrita ATRAVES DA CAIXA DE UPVALUE ABERTA sobre um slot
// possuido e um store contado no slot — a entrada (slot, objeto) do frame dono
// tem de passar a nomear o valor novo, exatamente como na escrita via ref
// (retargetOwnedSlot). Sem o reaponte, o fim do frame soltava o objeto velho
// uma SEGUNDA vez (dec a mais): vivo em dois globais, ele parecia unico e a
// mutacao seguinte vazava de ga[0] para gb[0] (candidato respondia 55|55 —
// bug PRE-EXISTENTE em `let` capturado, exposto de vez pelos itens de for-each
// possuidores e capturaveis).
//
// Oraculo do merge-base: 1|1.
func TestOpenUpvalueWriteRetargetsFrameOwnedEntry(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Box
    v: int
end

let ga: Box[] = []
let gb: Box[] = []
let ha: Box[] = []
let hb: Box[] = []

// V1: rebind de um let possuidor pela caixa ABERTA (OP_SET_UPVALUE).
func v1_build()
    let y: Box = Box(1)
    ga = [y]
    gb = [y]
    let f: func = func() -> void
        y = Box(9)
    end
    f()
end

// V2: mutacao do let possuidor pela caixa ABERTA (OP_GET_UPVALUE_MUT).
func v2_build()
    let y: Box = Box(1)
    ha = [y]
    hb = [y]
    let f: func = func() -> void
        y.v = 3
    end
    f()
end

func main()
    v1_build()
    ga[0].v = 55
    v2_build()
    ha[0].v = 55
    test_report(to_str(gb[0].v) + "|" + to_str(hb[0].v))
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	const want = "1|1"
	if reported.Type != value.VAL_OBJ || reported.Obj != want {
		t.Fatalf("entrada de posse obsoleta apos escrita pela caixa aberta (dec a mais): esperado %q (set_upvalue|get_upvalue_mut), veio %#v", want, reported.Obj)
	}
}

// Task 7 (round 4): aritmetica exata do laco — cada elemento recebe UM retain
// no bind da iteracao (OP_OWN_LOCAL roda dentro do corpo) e UM release no bind
// da iteracao seguinte (bindOwnedSlot paga a entrada anterior do slot) ou no
// fim do frame (ultimo elemento). O mesmo objeto em tres posicoes: durante o
// laco Owners sobe exatamente 1 (o vinculo do item), e depois do retorno da
// funcao que iterou volta EXATAMENTE ao valor de antes — sem o pagamento da
// entrada anterior o replace vazaria um retain por iteracao (Owners inflaria
// para before+2 aqui).
func TestForEachItemRetainReleasePairsPerElement(t *testing.T) {
	machine := New()
	var before, during, after int32
	machine.DefineNative("probe_before", func(args []value.Value) value.Value {
		before = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_during", func(args []value.Value) value.Value {
		during = value.OwnersCount(args[0])
		return value.NewNull()
	})
	machine.DefineNative("probe_after", func(args []value.Value) value.Value {
		after = value.OwnersCount(args[0])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "probe_before")
	markProbeReadonly(t, machine, "probe_during")
	markProbeReadonly(t, machine, "probe_after")
	src := `
struct Box
    v: int
end

func loop_over(arr: Box[])
    for item in arr do
        probe_during(item)
    end
end

func main()
    let b: Box = Box(1)
    let arr: Box[] = [b, b, b]
    probe_before(b)
    loop_over(arr)
    probe_after(b)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	// slot do let b (1) + tres elementos do array (3)
	if before != 4 {
		t.Fatalf("precondicao: esperado Owners 4 antes do laco (let + 3 elementos), veio %d", before)
	}
	// dentro do laco: +1 do vinculo do item — em TODAS as iteracoes (o release
	// da entrada anterior acompanha o retain da seguinte)
	if during != before+1 {
		t.Fatalf("durante o laco esperado Owners %d (bind do item retem), veio %d", before+1, during)
	}
	// depois do retorno: conta fechada, um release por retain
	if after != before {
		t.Fatalf("apos o retorno esperado Owners %d (cada elemento: 1 retain, 1 release), veio %d", before, after)
	}
}

// Task 7 (round 5 — CRITICO 1, pre-existente): os dois helpers de reaponte
// varriam os frames de FORA PARA DENTRO e devolviam o primeiro match por
// endereco. Indices absolutos de slot sao reusados: a regiao de pilha do frame
// chamado sobrepoe os indices onde um bloco irmao MORTO do chamador deixou
// entradas nunca podadas — a entrada morta do chamador casava primeiro, era
// reapontada no lugar da entrada VIVA do frame interno, e o fim do frame
// soltava o objeto velho uma segunda vez (dec a mais: vivo em dois globais,
// parecia unico, a mutacao seguinte vazava). O alinhamento depende do offset do
// bloco morto (rr_alias: D0/D1 escapavam, D2+ mordiam). Correcao: varrer de
// DENTRO PARA FORA — entradas de um frame interno sao todas >= seu LocalBase,
// entao nenhuma entrada interna pode aliasar um slot vivo de frame externo.
//
// Cobre os DOIS funis: caixa de upvalue aberta (retargetOwnedSlotForUpvalue,
// cenarios A*) e escrita via ref (retargetOwnedSlot, cenarios B*), cada um com
// controle sem bloco morto. Oraculo do merge-base: 1 em todos.
func TestRetargetScansFramesInnermostFirst(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Box
    v: int
end

let ga: Box[] = []
let gb: Box[] = []

func setit(n: ref Box, w: Box)
    *n = w
end

// funil da caixa de upvalue aberta (OP_SET_UPVALUE)
func upv_build()
    let y: Box = Box(1)
    ga = [y]
    gb = [y]
    let f: func = func() -> void
        y = Box(9)
    end
    f()
end

// funil da escrita via ref (storeReferenceValue)
func ref_build()
    let y: Box = Box(1)
    ga = [y]
    gb = [y]
    setit(ref y, Box(9))
end

// A0/B0: controles sem bloco morto no chamador.
func a0() -> int
    upv_build()
    ga[0].v = 55
    return gb[0].v
end

func b0() -> int
    ref_build()
    ga[0].v = 55
    return gb[0].v
end

// A2/B2: bloco irmao morto deixa entradas possuidoras do CHAMADOR em indices
// que o frame chamado reusa (o offset 2 e o primeiro que morde no rr_alias).
func a2() -> int
    if true then
        let d0: Box = Box(90)
        let d1: Box = Box(91)
        d0.v = d1.v
    end
    upv_build()
    ga[0].v = 55
    return gb[0].v
end

func b2() -> int
    if true then
        let d0: Box = Box(90)
        let d1: Box = Box(91)
        d0.v = d1.v
    end
    ref_build()
    ga[0].v = 55
    return gb[0].v
end

// A3/B3: um slot morto a mais (outro alinhamento que mordia).
func a3() -> int
    if true then
        let d0: Box = Box(90)
        let d1: Box = Box(91)
        let d2: Box = Box(92)
        d0.v = d1.v + d2.v
    end
    upv_build()
    ga[0].v = 55
    return gb[0].v
end

func b3() -> int
    if true then
        let d0: Box = Box(90)
        let d1: Box = Box(91)
        let d2: Box = Box(92)
        d0.v = d1.v + d2.v
    end
    ref_build()
    ga[0].v = 55
    return gb[0].v
end

func main()
    let ra0: string = to_str(a0())
    let ra2: string = to_str(a2())
    let ra3: string = to_str(a3())
    let rb0: string = to_str(b0())
    let rb2: string = to_str(b2())
    let rb3: string = to_str(b3())
    test_report(ra0 + "|" + ra2 + "|" + ra3 + "|" + rb0 + "|" + rb2 + "|" + rb3)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	const want = "1|1|1|1|1|1"
	if reported.Type != value.VAL_OBJ || reported.Obj != want {
		t.Fatalf("reaponte casou entrada morta do chamador (dec a mais): esperado %q (a0|a2|a3|b0|b2|b3), veio %#v", want, reported.Obj)
	}
}

// Task 7 (round 5 — CRITICO 2, pre-existente): quando o valor novo de um
// OP_SET_LOCAL nao e retivel (ex.: `z = null` num slot possuidor), o site ja
// liberou o ocupante velho, mas o ownSlot fazia early-return no Retain falho e
// a entrada (slot, objeto) continuava nomeando o objeto JA PAGO — entrada
// fantasma. Quem a honrasse (fim do frame, ou o bindOwnedSlot da iteracao
// seguinte) soltava o objeto de novo: dec a mais, vivo em dois globais ele
// parecia unico e a mutacao vazava. Correcao: ownSlot remove a entrada do slot
// (swap-remove) quando o ocupante novo nao e retivel — mesma disciplina do
// bindOwnedSlot.
//
// Oraculo do merge-base: 1 em todos (N4 e o controle sem rebind para null;
// N3 cobre o rebind para null da variavel de for-each).
func TestOwnSlotDropsEntryOnNonRetainableRebind(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	src := `
struct Box
    v: int
end

let ga: Box[] = []
let gb: Box[] = []

// N1: rebind de slot possuidor para null; a entrada fantasma seria honrada
// pelo release em massa do fim do frame.
func n1_build()
    let y: Box = Box(1)
    ga = [y]
    gb = [y]
    let z: Box = y
    z = null
end

func n1() -> int
    n1_build()
    ga[0].v = 55
    return gb[0].v
end

// N2: o mesmo dentro de um laco — a entrada fantasma seria honrada pelo
// bindOwnedSlot da iteracao seguinte, antes do fim do frame.
func n2_build()
    let y: Box = Box(1)
    ga = [y]
    gb = [y]
    let i: int = 0
    while i < 3 do
        let z: Box = y
        z = null
        i = i + 1
    end
end

func n2() -> int
    n2_build()
    ga[0].v = 55
    return gb[0].v
end

// N3: variavel de for-each rebindada para null.
func n3_build(arr: Box[])
    for item in arr do
        item = null
    end
end

func n3() -> int
    let y: Box = Box(1)
    ga = [y]
    gb = [y]
    let arr: Box[] = [y, y, y]
    n3_build(arr)
    ga[0].v = 55
    return gb[0].v
end

// N4: controle — sem rebind para null.
func n4_build()
    let y: Box = Box(1)
    ga = [y]
    gb = [y]
    let z: Box = y
    z.v = z.v
end

func n4() -> int
    n4_build()
    ga[0].v = 55
    return gb[0].v
end

func main()
    let r1: string = to_str(n1())
    let r2: string = to_str(n2())
    let r3: string = to_str(n3())
    let r4: string = to_str(n4())
    test_report(r1 + "|" + r2 + "|" + r3 + "|" + r4)
end

main()`
	if err := interpretVMSource(t, machine, src); err != nil {
		t.Fatalf("programa falhou: %v", err)
	}
	const want = "1|1|1|1"
	if reported.Type != value.VAL_OBJ || reported.Obj != want {
		t.Fatalf("entrada fantasma apos rebind nao-retivel (dec a mais): esperado %q (n1|n2|n3|n4), veio %#v", want, reported.Obj)
	}
}
