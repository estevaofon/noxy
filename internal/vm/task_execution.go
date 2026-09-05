package vm

import (
	"fmt"

	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/value"
)

type preparedTaskCall struct {
	Callable  value.Value
	Closure   *value.ObjClosure
	Arguments []value.Value
}

func (vm *VM) prepareTaskCall(callable value.Value, arguments []value.Value) (preparedTaskCall, error) {
	if callable.Type != value.VAL_FUNCTION {
		return preparedTaskCall{}, fmt.Errorf("task expects a script function")
	}

	var closure *value.ObjClosure
	switch object := callable.Obj.(type) {
	case *value.ObjClosure:
		closure = object
	case *value.ObjFunction:
		if object != nil && object.UpvalueCount == 0 {
			closure = &value.ObjClosure{
				Function:    object,
				Upvalues:    []*value.ObjUpvalue{},
				Environment: object.Environment,
			}
			callable = value.Value{Type: value.VAL_FUNCTION, Obj: closure}
		}
	default:
		return preparedTaskCall{}, fmt.Errorf("task received a malformed function value")
	}
	if closure == nil || closure.Function == nil || closure.Environment == nil {
		return preparedTaskCall{}, fmt.Errorf("task received a malformed function value")
	}
	function := closure.Function
	code, ok := function.Chunk.(*chunk.Chunk)
	if !ok || code == nil || function.Arity < 0 || len(function.Params) != function.Arity || len(closure.Upvalues) != function.UpvalueCount {
		return preparedTaskCall{}, fmt.Errorf("task received a malformed function value")
	}
	for _, upvalue := range closure.Upvalues {
		if !upvalue.IsValid() {
			return preparedTaskCall{}, fmt.Errorf("task received a malformed function value")
		}
	}

	if len(arguments) != function.Arity {
		return preparedTaskCall{}, fmt.Errorf("expected %d arguments but got %d", function.Arity, len(arguments))
	}
	if err := validateParameterModes(function.Name, function.Params, arguments); err != nil {
		return preparedTaskCall{}, err
	}
	if schema := function.RuntimeType; schema != nil && schema.Kind == value.TYPE_CALLABLE && len(schema.Params) == len(arguments) {
		for i, expected := range schema.Params {
			if !runtimeTypeComplete(expected, make(map[*value.RuntimeTypeInfo]bool)) {
				continue
			}
			if !vm.runtimeValueMatchesType(arguments[i], expected) {
				return preparedTaskCall{}, fmt.Errorf("function '%s' argument %d: expected %s, got %s", function.Name, i+1, expected.String(), runtimeValueMode(arguments[i]))
			}
		}
	}

	preparedArguments := make([]value.Value, len(arguments))
	copy(preparedArguments, arguments)
	// RC: retain de captura, mesmo padrao/funcao do defer (retainPreparedArguments)
	// — a preparacao roda sincrona, antes do goroutine da task ser lancado; os
	// args ficam "guardados" em preparedTaskCall.Arguments ate a task terminar.
	// Release espelhado em startSupervisedTask.
	vm.retainPreparedArguments(preparedArguments, function.Params)
	return preparedTaskCall{Callable: callable, Closure: closure, Arguments: preparedArguments}, nil
}

func (vm *VM) executePreparedTaskCall(call preparedTaskCall) (value.Value, error) {
	result := value.NewNull()
	vm.push(call.Callable)
	for _, argument := range call.Arguments {
		vm.push(argument)
	}
	frame := &vm.frames[0]
	*frame = CallFrame{
		Closure:     call.Closure,
		IP:          0,
		StackBase:   0,
		LocalBase:   0,
		Environment: call.Closure.Environment,
		Deferred:    frame.Deferred[:0],
		Owned:       frame.Owned[:0],
	}
	// RC: parametros sem ref sao vinculos duraveis do frame da task — mesmo
	// bind que callPreparedClosure faz (spec §4.2, linha da captura de task:
	// os slots de parametro da task "fazem seu proprio inc"). Sem isto, um
	// rebind (OP_SET_LOCAL) ou o clone do caminho MUT dentro do corpo da task
	// dispararia Release(velho) sem retain correspondente (dec a menos), e
	// releasePreparedArguments em startSupervisedTask soltaria a captura DE
	// NOVO. O release espelhado destes binds e o funil unico do frame
	// (finalizeCurrentFrame), tanto no retorno normal quanto no unwind.
	params := call.Closure.Function.Params
	for i := range call.Arguments {
		if i < len(params) && params[i].IsRef {
			continue
		}
		frame.ownSlot(vm, frame.LocalBase+1+i)
	}
	vm.frameCount, vm.currentFrame = 1, frame
	if err := vm.run(1, &result); err != nil {
		return value.NewNull(), err
	}
	return result, nil
}
