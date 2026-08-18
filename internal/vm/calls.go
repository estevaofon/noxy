package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

func (vm *VM) callValue(callee value.Value, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	if callee.Type == value.VAL_OBJ {
		if structDef, ok := callee.Obj.(*value.ObjStruct); ok && structDef != nil {
			if argCount != len(structDef.Fields) {
				return false, vm.runtimeError(c, ip, "expected %d arguments for struct %s but got %d", len(structDef.Fields), structDef.Name, argCount)
			}
			args := vm.stack[vm.stackTop-argCount : vm.stackTop]
			if err := vm.validateStructConstructorArguments(structDef, args); err != nil {
				return false, vm.runtimeError(c, ip, "%s", err)
			}
			return vm.callPreparedValue(callee, argCount, c, ip)
		}
	}
	if callee.Type == value.VAL_FUNCTION {
		return vm.call(callee.Obj.(*value.ObjClosure), argCount, c, ip)
	}
	if callee.Type == value.VAL_NATIVE {
		native := callee.Obj.(*value.ObjNative)
		args := vm.stack[vm.stackTop-argCount : vm.stackTop]
		if native.Signature != nil {
			params, err := nativeParameters(native, argCount)
			if err != nil {
				return false, vm.runtimeError(c, ip, "%s", err)
			}
			if err := validateParameterModes(native.Name, params, args); err != nil {
				return false, vm.runtimeErrorCause(c, ip, err, "native '%s' failed", native.Name)
			}
			callArgs := append([]value.Value(nil), args...)
			vm.copyPreparedArguments(callArgs, params)
			args = callArgs
		} else if !native.ReadonlyArgs {
			// RC: native sem assinatura pode reter/mutar args — retém todos
			// os compostos (conservador; allowlist só-leitura pula isto).
			// Retenção permanente e conservadora — não sabemos se o native
			// guarda o valor além da chamada, então assumimos que sim e
			// nunca soltamos (sem release em lugar nenhum).
			for i := range args {
				value.Retain(args[i])
			}
		}
		return vm.callNative(native, args, argCount, c, ip)
	}
	return false, vm.runtimeError(c, ip, "can only call functions and classes")
}

// callValueStatic é o caminho de OP_CALL_STATIC: o compilador provou os modos
// dos argumentos no call site (isExact), e tipos são estáveis, então closures
// pulam validateParameterModes. Struct constructors e natives seguem pelo
// callValue normal — as validações deles são de outra natureza (aridade de
// struct, assinatura de native) e continuam valendo.
//
// O skip depende de três invariantes do compilador — se qualquer um mudar,
// este opcode deixa de ser sound:
//  1. isExact só nasce de FunctionType exato; bare `func` é PrimitiveType e
//     nunca ativa (function_types.go:23-26);
//  2. areStrictTypesCompatible rejeita `any` como fonte para tipos função
//     (function_types.go:177-178);
//  3. fronteiras dinâmicas validadas comparam ParamIsRef em
//     runtimeTypesEqual (runtime_type_validation.go:481-485).
func (vm *VM) callValueStatic(callee value.Value, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	if callee.Type == value.VAL_FUNCTION {
		closure := callee.Obj.(*value.ObjClosure)
		if argCount != closure.Function.Arity {
			return false, vm.runtimeError(c, ip, "expected %d arguments but got %d", closure.Function.Arity, argCount)
		}
		return vm.callPreparedClosure(closure, argCount, c, ip)
	}
	return vm.callValue(callee, argCount, c, ip)
}

func (vm *VM) callPreparedValue(callee value.Value, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	if callee.Type == value.VAL_OBJ {
		if structDef, ok := callee.Obj.(*value.ObjStruct); ok && structDef != nil {
			instance := value.NewInstance(structDef)
			instObj := instance.Obj.(*value.ObjInstance)
			for i := 0; i < argCount; i++ {
				arg := vm.peek(argCount - 1 - i)
				value.Retain(arg) // RC: campo e dono duravel
				instObj.Fields[structDef.Fields[i]] = arg
			}
			vm.stackTop -= argCount + 1
			vm.push(instance)
			return true, nil
		}
	}
	if callee.Type == value.VAL_FUNCTION {
		return vm.callPreparedClosure(callee.Obj.(*value.ObjClosure), argCount, c, ip)
	}
	if callee.Type == value.VAL_NATIVE {
		native := callee.Obj.(*value.ObjNative)
		args := vm.stack[vm.stackTop-argCount : vm.stackTop]
		return vm.callNative(native, args, argCount, c, ip)
	}
	return false, vm.runtimeError(c, ip, "can only call functions and classes")
}

func (vm *VM) callNative(native *value.ObjNative, args []value.Value, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	result, err := native.Invoke(vm, args)
	if err != nil {
		return false, vm.runtimeErrorCause(c, ip, err, "native '%s' failed", native.Name)
	}
	vm.stackTop -= argCount + 1
	vm.push(result)
	return true, nil
}

func (vm *VM) call(closure *value.ObjClosure, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	fn := closure.Function

	if argCount != fn.Arity {
		return false, vm.runtimeError(c, ip, "expected %d arguments but got %d", fn.Arity, argCount)
	}

	if vm.frameCount == FramesMax {
		return false, vm.runtimeError(c, ip, "stack overflow")
	}

	// Handle Pass-by-Value (Copy) for non-ref parameters
	// Args are at vm.stackTop - argCount
	baseArgs := vm.stackTop - argCount
	args := vm.stack[baseArgs:vm.stackTop]
	if err := validateParameterModes(fn.Name, fn.Params, args); err != nil {
		return false, vm.runtimeError(c, ip, "%s", err)
	}
	// RC: fronteira de valor para parametros nao-ref nao precisa de marcacao
	// aqui — a copia so acontece se alguem mutar (unicize), e a posse do
	// slot novo e decidida por ownSlot/Retain dentro de callPreparedClosure.
	return vm.callPreparedClosure(closure, argCount, c, ip)
}

func (vm *VM) callPreparedClosure(closure *value.ObjClosure, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	if vm.frameCount == FramesMax {
		return false, vm.runtimeError(c, ip, "stack overflow")
	}

	frame := &vm.frames[vm.frameCount]
	frame.Closure = closure
	frame.IP = 0
	frame.StackBase = vm.stackTop - argCount - 1
	frame.LocalBase = vm.stackTop - argCount - 1
	frame.Environment = closure.Environment
	frame.Deferred = frame.Deferred[:0]
	frame.Owned = frame.Owned[:0]

	// RC: parametros sem ref sao vinculos duraveis do frame novo
	params := closure.Function.Params
	for i := 0; i < argCount; i++ {
		if i < len(params) && params[i].IsRef {
			continue
		}
		frame.ownSlot(vm, frame.LocalBase+1+i)
	}

	vm.frameCount++
	vm.currentFrame = frame
	return true, nil
}

// copyValue é o clone raso do CoW: o contêiner novo nasce com Owners=0, e os
// filhos imediatos compostos ganham Retain (passam a ter dois donos duráveis).
func (vm *VM) copyValue(v value.Value) value.Value {
	if v.Type != value.VAL_OBJ {
		return v
	}
	switch obj := v.Obj.(type) {
	case *value.ObjArray:
		cloneCount.Add(1)
		newElems := make([]value.Value, len(obj.Elements))
		copy(newElems, obj.Elements)
		for _, el := range newElems {
			value.Retain(el) // RC: filho ganha dono duravel no clone
		}
		copied := value.NewArray(newElems)
		copied.Obj.(*value.ObjArray).RuntimeType.Store(obj.RuntimeType.Load())
		return copied
	case *value.ObjMap:
		cloneCount.Add(1)
		newData := obj.Snapshot()
		for _, val := range newData {
			value.Retain(val) // RC: filho ganha dono duravel no clone
		}
		copied := value.NewMap()
		copiedMap := copied.Obj.(*value.ObjMap)
		copiedMap.Replace(newData)
		copiedMap.RuntimeType.Store(obj.RuntimeType.Load())
		return copied
	case *value.ObjInstance:
		cloneCount.Add(1)
		newFields := make(map[string]value.Value)
		for k, val := range obj.Fields {
			value.Retain(val) // RC: filho ganha dono duravel no clone
			newFields[k] = val
		}
		return value.Value{Type: value.VAL_OBJ, Obj: &value.ObjInstance{Struct: obj.Struct, Fields: newFields}}
	default:
		return v
	}
}
