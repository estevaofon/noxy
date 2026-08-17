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
		vm.markPreparedArguments(prepared.Arguments, fn.Params)
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

// markPreparedArguments implementa a fronteira de valor do CoW para código
// Noxy (closures, tasks): em vez de copiar ansiosamente, marca os compostos
// não-ref como Shared — a cópia só acontece se alguém mutar (unicize).
// RC: também retém — a captura (PreparedCall.Arguments) é uma posse durável
// entre o registro do defer e sua invocação (janela em que os args ficam
// "guardados" fora da pilha); releasePreparedArguments desfaz isso depois
// que a invocação roda.
func (vm *VM) markPreparedArguments(args []value.Value, params []value.ParamInfo) {
	for i, param := range params {
		if i >= len(args) {
			break
		}
		if !param.IsRef {
			value.MarkShared(args[i])
			value.Retain(args[i])
		}
	}
}

// releasePreparedArguments desfaz exatamente o retain de captura feito por
// markPreparedArguments, usando a mesma condição (!IsRef) para não soltar
// argumentos ref que nunca foram retidos aqui. Chamado depois que a
// invocação preparada (defer ou task) já rodou — a posse durável passou a
// ser a do binding de parâmetro do frame instanciado pela chamada.
func (vm *VM) releasePreparedArguments(args []value.Value, params []value.ParamInfo) {
	for i, param := range params {
		if i >= len(args) {
			break
		}
		if !param.IsRef {
			value.Release(args[i])
		}
	}
}

// copyPreparedArguments mantém a cópia ansiosa para NATIVES com assinatura:
// o corpo em Go pode mutar o argumento sem passar pelo CoW do bytecode, então
// a cópia é a única proteção do chamador.
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
		return vm.runtimeErrorAtCurrentFrame("stack overflow while invoking deferred call")
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

		// RC: solta a retenção de captura feita em markPreparedArguments — a
		// invocação já rodou (sucesso ou erro, este é um defer do Go: sempre
		// executa) e já retomou posse durável via callPreparedClosure (frame
		// próprio da closure chamada). Só se aplica a callee VAL_FUNCTION,
		// que é o único caso em que markPreparedArguments reteve na captura
		// (native usa cópia ansiosa via copyPreparedArguments; struct não
		// marca nada aqui).
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
		return err
	}
	if vm.frameCount > ownerFrameCount {
		return vm.run(ownerFrameCount+1, nil)
	}
	return nil
}
