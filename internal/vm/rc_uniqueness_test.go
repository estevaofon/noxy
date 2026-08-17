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
