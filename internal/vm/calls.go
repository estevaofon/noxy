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
		}
		return vm.callNative(native, args, argCount, c, ip)
	}
	return false, vm.runtimeError(c, ip, "can only call functions and classes")
}

func (vm *VM) callPreparedValue(callee value.Value, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	if callee.Type == value.VAL_OBJ {
		if structDef, ok := callee.Obj.(*value.ObjStruct); ok && structDef != nil {
			instance := value.NewInstance(structDef)
			instObj := instance.Obj.(*value.ObjInstance)
			for i := 0; i < argCount; i++ {
				instObj.Fields[structDef.Fields[i]] = vm.peek(argCount - 1 - i)
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
	for i := 0; i < argCount; i++ {
		if i < len(fn.Params) {
			param := fn.Params[i]
			if !param.IsRef {
				// Pass by Value: Copy if mutable object
				val := vm.stack[baseArgs+i]
				vm.stack[baseArgs+i] = vm.copyValue(val)
			}
		}
	}
	return vm.callPreparedClosure(closure, argCount, c, ip)
}

func (vm *VM) callPreparedClosure(closure *value.ObjClosure, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	if vm.frameCount == FramesMax {
		return false, vm.runtimeError(c, ip, "stack overflow")
	}

	frame := &CallFrame{
		Closure:     closure,
		IP:          0,
		StackBase:   vm.stackTop - argCount - 1,
		LocalBase:   vm.stackTop - argCount - 1,
		Environment: closure.Environment,
	}
	// Push new frame
	vm.frames[vm.frameCount] = frame
	vm.frameCount++
	vm.currentFrame = frame
	return true, nil
}

func (vm *VM) copyValue(v value.Value) value.Value {
	if v.Type != value.VAL_OBJ {
		return v
	}
	switch obj := v.Obj.(type) {
	case *value.ObjArray:
		newElems := make([]value.Value, len(obj.Elements))
		copy(newElems, obj.Elements)
		copied := value.NewArray(newElems)
		copied.Obj.(*value.ObjArray).RuntimeType.Store(obj.RuntimeType.Load())
		return copied
	case *value.ObjMap:
		newData := obj.Snapshot()
		copied := value.NewMap()
		copiedMap := copied.Obj.(*value.ObjMap)
		copiedMap.Replace(newData)
		copiedMap.RuntimeType.Store(obj.RuntimeType.Load())
		return copied
	case *value.ObjInstance:
		newFields := make(map[string]value.Value)
		for k, val := range obj.Fields {
			newFields[k] = val
		}
		return value.Value{Type: value.VAL_OBJ, Obj: &value.ObjInstance{Struct: obj.Struct, Fields: newFields}}
	default:
		return v
	}
}
