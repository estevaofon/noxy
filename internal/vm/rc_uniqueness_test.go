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
