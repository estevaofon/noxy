package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// depth(n) recursivo: 10000 niveis exigem ~10001 frames e dezenas de milhares
// de slots — bem alem dos 64/2048 iniciais. Deve crescer e devolver o valor.
func TestDeepRecursionGrowsFramesAndStack(t *testing.T) {
	reported := captureVMSource(t, `
func depth(n: int) -> int
    if n == 0 then return 0 end
    return 1 + depth(n - 1)
end
test_report(depth(10000))`)
	if reported.Type != value.VAL_INT || reported.AsInt != 10000 {
		t.Fatalf("depth(10000) = %v, want 10000", reported)
	}
}

// Recursao infinita morre na ENTRADA do frame com erro de runtime limpo
// (nunca panic Go), com a mensagem do teto de frames.
func TestInfiniteRecursionReportsCallDepthOverflow(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
func forever() -> int
    return forever()
end
forever()`)
	if err == nil || !strings.Contains(err.Error(), "stack overflow: call depth exceeds") {
		t.Fatalf("error=%v, want call depth overflow", err)
	}
}

// Um VM novo (inclusive o de cada task) nasce com as capacidades iniciais —
// o crescimento e sob demanda, nao no construtor.
func TestNewVMStartsWithInitialCapacities(t *testing.T) {
	machine := New()
	if len(machine.frames) != framesInitial || len(machine.stack) != stackInitial {
		t.Fatalf("frames=%d stack=%d, want %d/%d", len(machine.frames), len(machine.stack), framesInitial, stackInitial)
	}
	worker := NewWithShared(machine.shared, machine.Config)
	if len(worker.frames) != framesInitial || len(worker.stack) != stackInitial {
		t.Fatalf("worker frames=%d stack=%d, want %d/%d", len(worker.frames), len(worker.stack), framesInitial, stackInitial)
	}
}

// Closure captura um local ANTES de uma recursao que forca a pilha de
// operandos a crescer; depois le e escreve pelo upvalue. Sem Relocate o
// upvalue aberto apontaria para o array velho e a escrita se perderia.
func TestOpenUpvalueSurvivesStackGrowth(t *testing.T) {
	reported := captureVMSource(t, `
func depth(n: int) -> int
    if n == 0 then return 0 end
    return 1 + depth(n - 1)
end
func run() -> int
    let contador: int = 1
    let inc: func() -> void = func() -> void
        contador = contador + 1
    end
    depth(5000)
    inc()
    return contador
end
test_report(run())`)
	if reported.Type != value.VAL_INT || reported.AsInt != 2 {
		t.Fatalf("contador = %v, want 2", reported)
	}
}

// Defer cujo corpo recursa fundo o bastante para realocar vm.frames: o frame
// que esta sendo finalizado precisa ser reobtido por indice — senao a posse e
// o Closure ficariam no array velho e o slot novo guardaria lixo.
func TestDeferThatGrowsFramesIsFinalizedByIndex(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, `
func depth(n: int) -> int
    if n == 0 then return 0 end
    return 1 + depth(n - 1)
end
func com_defer() -> int
    let dados: int[] = [1, 2, 3]
    defer depth(500)
    return length(dados)
end
test_report(com_defer())`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if reported.Type != value.VAL_INT || reported.AsInt != 3 {
		t.Fatalf("reported=%v, want 3", reported)
	}
	if machine.frameCount != 0 || machine.currentFrame != nil {
		t.Fatalf("frameCount=%d current=%p, want 0/nil", machine.frameCount, machine.currentFrame)
	}
	for i := range machine.frames {
		frame := &machine.frames[i]
		if frame.Closure != nil || frame.Environment != nil || len(frame.Owned) != 0 || len(frame.Deferred) != 0 {
			t.Fatalf("frame %d nao foi finalizado: closure=%p env=%p owned=%d deferred=%d", i, frame.Closure, frame.Environment, len(frame.Owned), len(frame.Deferred))
		}
	}
}

// O sentinela de operandos so e alcancavel por um unico frame que empilhe
// mais do que cabe ate o teto de uma vez; aqui forcamos o cenario
// diretamente: pilha no teto, push -> panic do sentinela -> run() converte.
func TestOperandStackAtCapIsRuntimeErrorNotPanic(t *testing.T) {
	machine := New()
	machine.DefineContextualNative("fill_stack", func(context value.NativeContext, _ []value.Value) (value.Value, error) {
		worker, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		worker.stack = make([]value.Value, StackMax)
		worker.stackTop = StackMax
		return value.NewNull(), nil
	})
	err := interpretVMSource(t, machine, "fill_stack()\nprint(1)\n")
	if err == nil || !strings.Contains(err.Error(), "stack overflow: operand stack exceeds") {
		t.Fatalf("error=%v, want operand stack overflow", err)
	}
}
