package vm

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
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
	if reported.Type != value.VAL_INT || reported.Int() != 10000 {
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
	if reported.Type != value.VAL_INT || reported.Int() != 2 {
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
	if reported.Type != value.VAL_INT || reported.Int() != 3 {
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
		worker.installStack(make([]value.Value, StackMax))
		worker.stackTop = StackMax
		return value.NewNull(), nil
	})
	err := interpretVMSource(t, machine, "fill_stack()\nprint(1)\n")
	if err == nil || !strings.Contains(err.Error(), "stack overflow: operand stack exceeds") {
		t.Fatalf("error=%v, want operand stack overflow", err)
	}
}

// O guard de headroom da chamada diferida mede o TETO, nao a alocacao atual.
// Topo encostado no fim da alocacao inicial: a chamada precisa de dois slots
// (callee + argumento) e so um sobra, entao a pilha tem de CRESCER e a chamada
// tem de rodar. Reprovar aqui seria um "stack overflow" ~512x abaixo do limite
// real, num ponto em que push() cresceria sem reclamar.
func TestDeferredCallGrowsStackInsteadOfFailingBelowTheCap(t *testing.T) {
	machine := New()
	ran := false
	cleanup := value.NewNative("cleanup", func([]value.Value) value.Value {
		ran = true
		return value.NewNull()
	})
	frame := &machine.frames[0]
	*frame = CallFrame{
		StackBase: 0,
		LocalBase: 0,
		Deferred: []PreparedCall{
			{Callee: cleanup, Arguments: []value.Value{value.NewInt(1)}, Registration: SourceLocation{File: "headroom.nx", Line: 4}},
		},
	}
	machine.frameCount = 1
	machine.currentFrame = frame
	machine.stackTop = stackInitial - 1

	outcome := machine.finishFrame(frameOutcome{Result: value.NewNull()})
	if outcome.Err != nil {
		t.Fatalf("err=%v, want the deferred call to grow the stack and run", outcome.Err)
	}
	if !ran {
		t.Fatal("deferred call did not run")
	}
	if len(machine.stack) <= stackInitial {
		t.Fatalf("stack=%d, want it grown past %d", len(machine.stack), stackInitial)
	}
}

// Mesmo cenario visto de cima: uma funcao que consome centenas de slots de
// operandos (literal grande) e ainda tem defer roda normalmente na alocacao
// inicial, crescendo a pilha em vez de falhar.
func TestFunctionWithManyOperandsAndDeferRuns(t *testing.T) {
	machine := New()
	limpou := false
	machine.DefineNative("marca_limpeza", func([]value.Value) value.Value {
		limpou = true
		return value.NewNull()
	})
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	source := "func com_muitos_operandos() -> int\n    let dados: int[] = [" + strings.Repeat("1, ", 399) + "1]\n    defer marca_limpeza()\n    return length(dados)\nend\ntest_report(com_muitos_operandos())"
	if err := interpretVMSource(t, machine, source); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if reported.Type != value.VAL_INT || reported.Int() != 400 {
		t.Fatalf("reported=%v, want 400", reported)
	}
	if !limpou {
		t.Fatal("defer did not run")
	}
}

// So o sentinela e recuperado: um panic estranho (aqui de um native) continua
// subindo por run() com o valor original, nunca virando runtime error.
func TestForeignPanicIsNotRecoveredByRun(t *testing.T) {
	machine := New()
	machine.DefineNative("estoura", func([]value.Value) value.Value {
		panic("panico estranho")
	})
	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		_ = interpretVMSource(t, machine, "estoura()\n")
		return nil
	}()
	if text, _ := recovered.(string); text != "panico estranho" {
		t.Fatalf("recovered=%v, want the original panic to propagate out of run()", recovered)
	}
}

// Dentro de call_result o mesmo panic estranho continua virando o envelope de
// panic da fronteira (nao o runtime error do sentinela).
func TestForeignPanicInsideCallResultStaysAPanicEnvelope(t *testing.T) {
	machine := New()
	reported := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			reported = args[0]
		}
		return value.NewNull()
	})
	machine.DefineNative("estoura", func([]value.Value) value.Value {
		panic("panico estranho")
	})
	if err := interpretVMSource(t, machine, `
use errors select *
func alvo() -> int
    estoura()
    return 1
end
let r: any = call_result(alvo)
test_report(r.failure.kind + "|" + r.failure.message)`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	text, _ := reported.Obj.(string)
	if !strings.HasPrefix(text, "panic|") || !strings.Contains(text, "panico estranho") {
		t.Fatalf("failure=%q, want the boundary panic envelope", text)
	}
}
