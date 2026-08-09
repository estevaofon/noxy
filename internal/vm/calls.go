package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

func (vm *VM) callValue(callee value.Value, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	if callee.Type == value.VAL_OBJ {
		if structDef, ok := callee.Obj.(*value.ObjStruct); ok && structDef != nil {
			// Instantiate
			if argCount != len(structDef.Fields) {
				return false, vm.runtimeError(c, ip, "expected %d arguments for struct %s but got %d", len(structDef.Fields), structDef.Name, argCount)
			}
			args := vm.stack[vm.stackTop-argCount : vm.stackTop]
			if err := vm.validateStructConstructorArguments(structDef, args); err != nil {
				return false, vm.runtimeError(c, ip, "%s", err)
			}

			instance := value.NewInstance(structDef)
			instObj := instance.Obj.(*value.ObjInstance)

			// Args are on stack.
			for i := 0; i < argCount; i++ {
				arg := vm.peek(argCount - 1 - i)
				fieldName := structDef.Fields[i]
				instObj.Fields[fieldName] = arg
			}

			// Pop args AND callee (struct def)
			vm.stackTop -= argCount + 1
			// Push instance
			vm.push(instance)
			return true, nil
		}
	}
	if callee.Type == value.VAL_FUNCTION {
		return vm.call(callee.Obj.(*value.ObjClosure), argCount, c, ip)
	}
	if callee.Type == value.VAL_NATIVE {
		native := callee.Obj.(*value.ObjNative)
		args := vm.stack[vm.stackTop-argCount : vm.stackTop]
		if native.Signature != nil {
			sig := native.Signature
			if !sig.Variadic && argCount != sig.Arity {
				return false, vm.runtimeError(c, ip, "native '%s' expects %d arguments, got %d", native.Name, sig.Arity, argCount)
			}
			if sig.Variadic && argCount < sig.Arity {
				return false, vm.runtimeError(c, ip, "native '%s' expects at least %d arguments, got %d", native.Name, sig.Arity, argCount)
			}

			params := sig.Params
			if sig.Variadic && len(params) > 0 && argCount > len(params) {
				expanded := make([]value.ParamInfo, argCount)
				copy(expanded, params)
				for i := len(params); i < argCount; i++ {
					expanded[i] = params[len(params)-1]
				}
				params = expanded
			}
			if err := validateParameterModes(native.Name, params, args); err != nil {
				return false, vm.runtimeError(c, ip, "%s", err)
			}

			callArgs := make([]value.Value, len(args))
			copy(callArgs, args)
			for i, param := range params {
				if i >= len(callArgs) {
					break
				}
				if !param.IsRef {
					callArgs[i] = vm.copyValue(callArgs[i])
				}
			}
			args = callArgs
		}
		// fmt.Printf("Calling native %s with args: %v\n", native.Name, args)
		result := native.Fn(args)
		vm.stackTop -= argCount + 1 // args + function
		vm.push(result)
		return true, nil
	}
	return false, vm.runtimeError(c, ip, "can only call functions and classes")
}

func (vm *VM) call(closure *value.ObjClosure, argCount int, c *chunk.Chunk, ip int) (bool, error) {
	// fmt.Printf("Calling function %s, code len: %d\n", fn.Name, len(chunk.Code))

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

	frame := &CallFrame{
		Closure: closure,
		IP:      0,
		Slots:   vm.stackTop - argCount - 1, // Start of locals window (fn + args)
		Globals: closure.Globals,
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
		newData := make(map[interface{}]value.Value)
		for k, val := range obj.Data {
			newData[k] = val
		}
		copied := value.NewMap()
		copiedMap := copied.Obj.(*value.ObjMap)
		copiedMap.Data = newData
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
