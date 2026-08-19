package vm

import (
	"fmt"
	"runtime/debug"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

func (vm *VM) defineCallResultBuiltins() {
	vm.DefineContextualNative("call_result", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), fmt.Errorf("call_result expects a callable")
		}
		return machine.runCallBoundary(args[0], args[1:])
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

// runCallBoundary: corpo completado nas Tasks 4-7. Nesta task, invoca a
// chamada preparada e envelopa o resultado no caminho ok; o mapeamento de
// falha real chega na Task 5 (placeholder abaixo mantem o pacote compilando).
func (vm *VM) runCallBoundary(callee value.Value, args []value.Value) (value.Value, error) {
	prepared, err := vm.prepareBoundaryCall(callee, args)
	if err != nil {
		return value.NewNull(), err
	}
	result, callErr := vm.invokeBoundaryCall(prepared)
	if callErr != nil {
		return callResultFailureEnvelope(callErr), nil // Task 5
	}
	return callResultOkEnvelope(result), nil
}

// invokeBoundaryCall espelha invokePreparedCall (defer.go) com duas
// diferencas: captura o resultado (terminalResult para closures; topo da
// pilha para native/construtor) e nao descarta o valor no cleanup — o
// envelope o carrega. O release da retencao de closure e identico.
func (vm *VM) invokeBoundaryCall(call PreparedCall) (result value.Value, err error) {
	base := vm.stackTop
	if base < 0 || base >= len(vm.stack) || len(call.Arguments) > len(vm.stack)-base-1 {
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
	// Registrado DEPOIS do defer de cleanup acima: defers do Go rodam LIFO,
	// entao num panico este corpo roda PRIMEIRO (hardUnwindTo restaura os
	// frames acima da fronteira) e SO ENTAO o cleanup acima roda (restaura a
	// janela da pilha e solta os argumentos preparados). Se a ordem fosse
	// invertida o cleanup rodaria sobre frames que o unwind ainda nao
	// desfez — vm.stackTop e vm.frameCount ficariam inconsistentes.
	defer func() {
		if recovered := recover(); recovered != nil {
			vm.hardUnwindTo(ownerFrameCount)
			result = value.NewNull()
			err = &boundaryPanicError{payload: fmt.Sprint(recovered), stack: string(debug.Stack())}
		}
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
		// RC: o envelope ok (callResultOkEnvelope) guarda `result` no campo
		// "value" via NewMapWithData, que — ao contrario de OP_MAP/OP_ARRAY —
		// NAO retem os valores que recebe. Sem este retain, uma composta
		// devolvida sem dono previo (Owners=0, caso comum de um retorno
		// fresco) apareceria com Owners=1 apos o PRIMEIRO `let` no lado Noxy
		// que capturar r.value; IsShared ficaria falso e uma mutacao nesse
		// primeiro binding aconteceria in-place, vazando para qualquer outra
		// leitura de r.value (mesmo objeto, guardado por referencia no mapa)
		// — corrupcao comprovada por TestCallResultValueSemantics antes deste
		// retain ("100|100|3" em vez de "1|100|3"). O retain aqui da ao
		// envelope o mesmo dono-duravel que OP_MAP teria registrado se este
		// valor tivesse sido escrito por bytecode Noxy comum.
		value.Retain(result)
		return result, nil
	}
	// native/construtor: sem frame novo; resultado no topo da pilha. Mesmo
	// retain do ramo acima, mesma justificativa.
	result = vm.peek(0)
	value.Retain(result)
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

func callResultOkEnvelope(result value.Value) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":      value.NewBool(true),
		"value":   result,
		"failure": value.NewNull(),
	})
}

func callResultFailureEnvelope(err error) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":      value.NewBool(false),
		"value":   value.NewNull(),
		"failure": failureMap(err),
	})
}

// failureMap converte a arvore de erro do unwinding no shape Failure.
// UnwindError com Primary vira a falha primaria com cada DeferredError em
// causes (ordem LIFO ja garantida por finalizeCurrentFrame); cleanup-first
// (Primary nil) promove a PRIMEIRA falha diferida a primaria e agrega as
// demais sob as causes dela (design §2, "Cleanup as first failure").
func failureMap(err error) value.Value {
	if panicErr, ok := err.(*boundaryPanicError); ok {
		return value.NewMapWithData(map[string]value.Value{
			"kind":    value.NewString("panic"),
			"message": value.NewString(panicErr.payload),
			"stack":   value.NewString(panicErr.stack),
			"causes":  value.NewArray([]value.Value{}),
		})
	}
	if unwind, ok := err.(*UnwindError); ok {
		if unwind.Primary != nil {
			return failureMapWithCauses(unwind.Primary, unwind.Deferred)
		}
		if len(unwind.Deferred) > 0 {
			primary := deferredFailureMap(&unwind.Deferred[0], unwind.Deferred[1:])
			return primary
		}
	}
	if deferred, ok := err.(*DeferredError); ok {
		return deferredFailureMap(deferred, nil)
	}
	if deferred, ok := err.(DeferredError); ok {
		return deferredFailureMap(&deferred, nil)
	}
	return failureMapWithCauses(err, nil)
}

func failureMapWithCauses(primary error, deferred []DeferredError) value.Value {
	causes := make([]value.Value, 0, len(deferred))
	for index := range deferred {
		causes = append(causes, deferredFailureMap(&deferred[index], nil))
	}
	message := ""
	if primary != nil {
		message = primary.Error()
	}
	return value.NewMapWithData(map[string]value.Value{
		"kind":    value.NewString("runtime"),
		"message": value.NewString(message),
		"stack":   value.NewString(deepestRuntimeStack(primary)),
		"causes":  value.NewArray(causes),
	})
}

// deferredFailureMap constroi a Failure de uma falha diferida: a causa vira a
// falha (aninhando as proprias causes dela recursivamente via failureMap) e a
// localizacao de REGISTRO do defer entra como frame mais externo do stack —
// forma-envelope da promessa da spec de defer ("with its registration
// location"). siblings sao falhas diferidas posteriores promovidas para as
// causes desta (apenas no caso cleanup-first).
func deferredFailureMap(deferred *DeferredError, siblings []DeferredError) value.Value {
	failure := failureMap(deferred.Cause)
	mapping := failure.Obj.(*value.ObjMap)

	stackValue, _ := mapping.Get("stack")
	stack, _ := stackValue.Obj.(string)
	registrationFrame := fmt.Sprintf("[%s] defer registration", deferred.Registration)
	if stack == "" {
		stack = registrationFrame
	} else {
		stack = stack + "\n" + registrationFrame
	}
	mapping.Set("stack", value.NewString(stack))

	if len(siblings) > 0 {
		// merge, nao substitui: a falha promovida ja pode ter causes proprias
		// (o Cause dela era um *UnwindError com seu proprio Deferred) —
		// preserva-las primeiro e so entao anexa os siblings (falhas
		// diferidas posteriores no mesmo frame, cleanup-first) por cima.
		inner := existingCauses(mapping)
		causes := make([]value.Value, 0, len(inner)+len(siblings))
		causes = append(causes, inner...)
		for index := range siblings {
			causes = append(causes, deferredFailureMap(&siblings[index], nil))
		}
		mapping.Set("causes", value.NewArray(causes))
	}
	return failure
}

// existingCauses le o array "causes" ja presente num mapa Failure recem
// construido por failureMap — sempre presente (failureMapWithCauses e
// deferredFailureMap ambos o preenchem, mesmo vazio) mas lido com defesa
// contra shape inesperado.
func existingCauses(mapping *value.ObjMap) []value.Value {
	causesValue, ok := mapping.Get("causes")
	if !ok {
		return nil
	}
	array, ok := causesValue.Obj.(*value.ObjArray)
	if !ok || array == nil {
		return nil
	}
	return array.Elements
}
