package vm

import (
	"fmt"

	"noxy-vm/internal/value"
)

type PreparedCall struct {
	Callee       value.Value
	Arguments    []value.Value
	Registration SourceLocation
}

func (vm *VM) prepareDeferredCall(callee value.Value, args []value.Value, registration SourceLocation) (PreparedCall, error) {
	prepared := PreparedCall{
		Callee:       callee,
		Arguments:    append([]value.Value(nil), args...),
		Registration: registration,
	}

	switch callee.Type {
	case value.VAL_FUNCTION:
		closure, ok := callee.Obj.(*value.ObjClosure)
		if !ok || closure == nil || closure.Function == nil {
			return PreparedCall{}, fmt.Errorf("invalid function")
		}
		fn := closure.Function
		if len(args) != fn.Arity {
			return PreparedCall{}, fmt.Errorf("expected %d arguments but got %d", fn.Arity, len(args))
		}
		if err := validateParameterModes(fn.Name, fn.Params, args); err != nil {
			return PreparedCall{}, err
		}
		vm.copyPreparedArguments(prepared.Arguments, fn.Params)
		return prepared, nil

	case value.VAL_NATIVE:
		native, ok := callee.Obj.(*value.ObjNative)
		if !ok || native == nil {
			return PreparedCall{}, fmt.Errorf("invalid native function")
		}
		if native.Signature == nil {
			return prepared, nil
		}
		params, err := nativeParameters(native, len(args))
		if err != nil {
			return PreparedCall{}, err
		}
		if err := validateParameterModes(native.Name, params, args); err != nil {
			return PreparedCall{}, err
		}
		vm.copyPreparedArguments(prepared.Arguments, params)
		return prepared, nil

	case value.VAL_OBJ:
		definition, ok := callee.Obj.(*value.ObjStruct)
		if !ok || definition == nil {
			break
		}
		if len(args) != len(definition.Fields) {
			return PreparedCall{}, fmt.Errorf("expected %d arguments for struct %s but got %d", len(definition.Fields), definition.Name, len(args))
		}
		if err := vm.validateStructConstructorArguments(definition, args); err != nil {
			return PreparedCall{}, err
		}
		return prepared, nil
	}

	return PreparedCall{}, fmt.Errorf("can only call functions and classes")
}

func nativeParameters(native *value.ObjNative, argCount int) ([]value.ParamInfo, error) {
	signature := native.Signature
	if !signature.Variadic && argCount != signature.Arity {
		return nil, fmt.Errorf("native '%s' expects %d arguments, got %d", native.Name, signature.Arity, argCount)
	}
	if signature.Variadic && argCount < signature.Arity {
		return nil, fmt.Errorf("native '%s' expects at least %d arguments, got %d", native.Name, signature.Arity, argCount)
	}

	params := signature.Params
	if signature.Variadic && len(params) > 0 && argCount > len(params) {
		expanded := make([]value.ParamInfo, argCount)
		copy(expanded, params)
		for i := len(params); i < argCount; i++ {
			expanded[i] = params[len(params)-1]
		}
		params = expanded
	}
	return params, nil
}

func (vm *VM) copyPreparedArguments(args []value.Value, params []value.ParamInfo) {
	for i, param := range params {
		if i >= len(args) {
			break
		}
		if !param.IsRef {
			args[i] = vm.copyValue(args[i])
		}
	}
}

func (vm *VM) invokePreparedCall(call PreparedCall) (err error) {
	base := vm.stackTop
	if base < 0 || base >= len(vm.stack) || len(call.Arguments) > len(vm.stack)-base-1 {
		return fmt.Errorf("stack overflow while invoking deferred call")
	}
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
	}()

	ownerFrameCount := vm.frameCount
	vm.push(call.Callee)
	for _, argument := range call.Arguments {
		vm.push(argument)
	}
	temporaryTop = vm.stackTop

	ok, err := vm.callPreparedValue(call.Callee, len(call.Arguments), nil, 0)
	if !ok {
		return err
	}
	if vm.frameCount > ownerFrameCount {
		return vm.run(ownerFrameCount + 1)
	}
	return nil
}
