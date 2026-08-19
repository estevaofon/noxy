package vm

import (
	"fmt"

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
		return result, nil
	}
	// native/construtor: sem frame novo; resultado no topo da pilha.
	return vm.peek(0), nil
}

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
		causes := make([]value.Value, 0, len(siblings))
		for index := range siblings {
			causes = append(causes, deferredFailureMap(&siblings[index], nil))
		}
		mapping.Set("causes", value.NewArray(causes))
	}
	return failure
}
