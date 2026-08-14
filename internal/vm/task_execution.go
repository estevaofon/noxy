package vm

import (
	"fmt"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
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
		if upvalue == nil || upvalue.Location == nil {
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
	for i, parameter := range function.Params {
		if i >= len(preparedArguments) {
			break
		}
		if !parameter.IsRef {
			preparedArguments[i] = vm.copyValue(preparedArguments[i])
		}
	}
	return preparedTaskCall{Callable: callable, Closure: closure, Arguments: preparedArguments}, nil
}

func (vm *VM) executePreparedTaskCall(call preparedTaskCall) (value.Value, error) {
	result := value.NewNull()
	vm.push(call.Callable)
	for _, argument := range call.Arguments {
		vm.push(argument)
	}
	frame := &CallFrame{
		Closure:     call.Closure,
		IP:          0,
		Slots:       0,
		Environment: call.Closure.Environment,
	}
	vm.frames[0], vm.frameCount, vm.currentFrame = frame, 1, frame
	if err := vm.run(1, &result); err != nil {
		return value.NewNull(), err
	}
	return result, nil
}
