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
