package vm

import (
	"fmt"
	"runtime/debug"

	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/value"
)

func (vm *VM) defineCallResultBuiltins() {
	vm.DefineContextualNative("call_result", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		// Argumentos ocultos (issue #105 item 2): o compilador prefixa o
		// construtor da instancia errors::Result<R> e o de errors.Failure.
		defs, err := resultDefsFromArgs(args)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 3 {
			return value.NewNull(), fmt.Errorf("call_result expects a callable")
		}
		return machine.runCallBoundary(defs, args[2], args[3:])
	})
}

// prepareBoundaryCall valida sincronamente no chamador (design: misuse nunca
// e capturado). Normaliza ObjFunction sem upvalues para closure — mesmo
// ajuste de prepareTaskCall — e delega o resto a prepareDeferredCall, que ja
// valida closure (aridade+modos), native assinado (assinatura+modos, copia
// ansiosa) e construtor de struct (campos+tipos).
func (vm *VM) prepareBoundaryCall(callee value.Value, args []value.Value) (PreparedCall, error) {
	if callee.Type == value.VAL_FUNCTION {
		if fn, ok := callee.Obj.(*value.ObjFunction); ok && fn != nil && fn.UpvalueCount == 0 {
			callee = value.Value{Type: value.VAL_FUNCTION, Obj: &value.ObjClosure{
				Function:    fn,
				Upvalues:    []*value.ObjUpvalue{},
				Environment: fn.Environment,
			}}
		}
	}
	registration := SourceLocation{File: "?"}
	if frame := vm.currentFrame; frame != nil && frame.Closure != nil && frame.Closure.Function != nil {
		if c, ok := frame.Closure.Function.Chunk.(*chunk.Chunk); ok {
			registration = sourceLocation(c, frame.IP)
		}
	}
	prepared, err := vm.prepareDeferredCall(callee, args, registration)
	if err != nil {
		if callee.Type != value.VAL_FUNCTION && callee.Type != value.VAL_NATIVE {
			if _, isStruct := callee.Obj.(*value.ObjStruct); callee.Type != value.VAL_OBJ || !isStruct {
				return PreparedCall{}, fmt.Errorf("call_result expects a callable, got %s", runtimeValueMode(callee))
			}
		}
		return PreparedCall{}, err
	}
	return prepared, nil
}

// runCallBoundary e a fronteira inteira em tres passos: valida (erro de
// misuse sobe para o chamador — nunca vira envelope), invoca, e envelopa. O
// invariante que a assinatura carrega: depois de prepareBoundaryCall passar,
// esta funcao so devolve erro se a propria fronteira estiver quebrada — toda
// falha do callee vira CallResult{ok: false}.
func (vm *VM) runCallBoundary(defs resultDefs, callee value.Value, args []value.Value) (value.Value, error) {
	prepared, err := vm.prepareBoundaryCall(callee, args)
	if err != nil {
		return value.NewNull(), err
	}
	result, callErr := vm.invokeBoundaryCall(prepared)
	if callErr != nil {
		return defs.failureEnvelope(callErr), nil
	}
	return defs.okEnvelope(result), nil
}

// invokeBoundaryCall espelha invokePreparedCall (defer.go) com duas
// diferencas: captura o resultado (terminalResult para closures; topo da
// pilha para native/construtor) e nao descarta o valor no cleanup — o
// envelope o carrega. O release da retencao de closure e identico.
func (vm *VM) invokeBoundaryCall(call PreparedCall) (result value.Value, err error) {
	base := vm.stackTop
	// Mesma folga de invokePreparedCall (defer.go): obtida crescendo a pilha,
	// nao medida contra a alocacao atual — so StackMax reprova.
	if base < 0 || !vm.ensureStackHeadroom(len(call.Arguments)+1) {
		return value.NewNull(), vm.runtimeErrorAtCurrentFrame("stack overflow while invoking call_result")
	}
	result = value.NewNull()
	temporaryTop := base
	defer func() {
		cleanupTop := vm.stackTop
		if temporaryTop > cleanupTop {
			cleanupTop = temporaryTop
		}
		for i := base; i < cleanupTop; i++ {
			vm.stack[i] = value.Value{}
		}
		vm.stackTop = base
		if call.Callee.Type == value.VAL_FUNCTION {
			if closure, ok := call.Callee.Obj.(*value.ObjClosure); ok && closure != nil && closure.Function != nil {
				vm.releasePreparedArguments(call.Arguments, closure.Function.Params)
			}
		}
	}()

	ownerFrameCount := vm.frameCount
	// A spec promete que Failure.stack cobre "the frames from the failure
	// point down to, and excluding, the call_result frame itself". Quem sabe
	// onde fica esse corte e a fronteira, nao captureNoxyStack: enquanto a
	// chamada capturada roda, o piso vale ownerFrameCount e todo stack
	// capturado la dentro (inclusive os das falhas diferidas montadas durante
	// o unwind) para no primeiro frame acima do chamador. Piso por indice, e
	// nao pos-processamento do texto: captureNoxyStack pula frames sem
	// Closure/Function, entao "corte as N ultimas linhas" nao teria
	// correspondencia confiavel com N frames de chamador. Salvo/restaurado
	// para nao vazar para o chamador (e para aninhar fronteiras); erro fatal
	// de topo segue com a pilha inteira, piso 0.
	previousStackFloor := vm.stackCaptureFloor
	vm.stackCaptureFloor = ownerFrameCount
	defer func() { vm.stackCaptureFloor = previousStackFloor }()
	// Registrado DEPOIS do defer de cleanup acima: defers do Go rodam LIFO,
	// entao num panico este corpo roda PRIMEIRO (hardUnwindTo restaura os
	// frames acima da fronteira) e SO ENTAO o cleanup acima roda (restaura a
	// janela da pilha e solta os argumentos preparados). Se a ordem fosse
	// invertida o cleanup rodaria sobre frames que o unwind ainda nao
	// desfez — vm.stackTop e vm.frameCount ficariam inconsistentes.
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		// Sentinela de push(): a fronteira o converte no runtime error padrao
		// — estouro de pilha e falha de RUNTIME capturavel (Failure.kind
		// "runtime"), nunca um envelope de panic com stack Go. Construido
		// ANTES do hardUnwind para a pilha Noxy ainda conter os frames que
		// estouraram (o piso ja e ownerFrameCount).
		_, isOverflow := recovered.(stackOverflowPanic)
		var overflow error
		if isOverflow {
			overflow = vm.runtimeErrorAtCurrentFrame("stack overflow: operand stack exceeds %d slots", StackMax)
		}
		vm.hardUnwindTo(ownerFrameCount)
		result = value.NewNull()
		if isOverflow {
			err = overflow
			return
		}
		err = &boundaryPanicError{payload: fmt.Sprint(recovered), stack: string(debug.Stack())}
	}()
	vm.push(call.Callee)
	for _, argument := range call.Arguments {
		vm.push(argument)
	}
	temporaryTop = vm.stackTop

	ok, err := vm.callPreparedValue(call.Callee, len(call.Arguments), nil, 0)
	if !ok {
		return value.NewNull(), err
	}
	if vm.frameCount > ownerFrameCount {
		if runErr := vm.run(ownerFrameCount+1, &result); runErr != nil {
			return value.NewNull(), runErr
		}
		// RC: a posse de `result` pelo envelope ok e registrada pelo
		// construtor (value.NewMapWithData retem em callResultOkEnvelope);
		// reter aqui tambem deixaria r.value com 2 donos e IsShared para
		// sempre. Ver TestCallResultOkValueHasExactlyOneOwner.
		return result, nil
	}
	// native/construtor: sem frame novo; resultado no topo da pilha.
	result = vm.peek(0)
	return result, nil
}

// boundaryPanicError transporta um panico de Go recuperado na fronteira; o
// envelope o converte em Failure{kind: "panic"}. Nunca escapa da fronteira.
type boundaryPanicError struct {
	payload string
	stack   string
}

func (err *boundaryPanicError) Error() string { return err.payload }

// hardUnwindTo libera os frames acima de target sem executar defers Noxy —
// depois de um panico de Go o estado desses frames e suspeito; espelha a
// fronteira de task, que tambem nao roda defers no caminho de panico (o VM
// filho e abandonado). Truncar Deferred antes de finalizar reusa o funil
// unico de release (Owned/upvalues) sem rodar codigo Noxy.
//
// Nao rodar os PreparedCall pendentes NAO significa que a captura deles pode
// ser esquecida: prepareDeferredCall ja reteve (retainPreparedArguments,
// defer.go:35/101-110) cada argumento composto nao-ref no REGISTRO do defer —
// o unico lugar que desfaz isso hoje e o cleanup de invokePreparedCall
// (defer.go:165-169), que so roda se a chamada for de fato invocada. Pular a
// invocacao sem soltar essa retencao vaza um dono por argumento composto de
// cada defer pendente nos frames abandonados: o valor fica IsShared para
// sempre (clona a cada mutacao) e, pior, um ref vivo que ainda aponte para o
// slot original diverge do clone. Por isso cada PreparedCall e liberado aqui
// (mesma condicao de invokePreparedCall — so VAL_FUNCTION reteve na captura;
// native usou copia ansiosa, construtor nao reteve nada) antes do slice ser
// truncado; cada entrada tambem e zerada (nao so descartada por indice) para
// nao prender o PreparedCall antigo no array de suporte reusado — o mesmo
// padrao de vazamento que os comentarios de unwind.go documentam para
// frame.Owned.
func (vm *VM) hardUnwindTo(target int) {
	for vm.frameCount > target {
		if frame := vm.currentFrame; frame != nil {
			for i := range frame.Deferred {
				call := frame.Deferred[i]
				if call.Callee.Type == value.VAL_FUNCTION {
					if closure, ok := call.Callee.Obj.(*value.ObjClosure); ok && closure != nil && closure.Function != nil {
						vm.releasePreparedArguments(call.Arguments, closure.Function.Params)
					}
				}
				frame.Deferred[i] = PreparedCall{}
			}
			frame.Deferred = frame.Deferred[:0]
		}
		vm.finalizeCurrentFrame(frameOutcome{Err: errBoundaryPanic})
	}
}

var errBoundaryPanic = fmt.Errorf("call_result: unwinding after Go panic")
