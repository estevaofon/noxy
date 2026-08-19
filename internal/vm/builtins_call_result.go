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

// placeholder ate a Task 5 (mantem o pacote compilando):
func callResultFailureEnvelope(err error) value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"ok":      value.NewBool(false),
		"value":   value.NewNull(),
		"failure": value.NewNull(),
	})
}
