package vm

import (
	"fmt"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
	"reflect"
	"unicode/utf8"
)

func (vm *VM) Interpret(c *chunk.Chunk) error {
	return vm.InterpretWithEnvironment(c, vm.shared.Root)
}

func (vm *VM) InterpretWithGlobals(c *chunk.Chunk, globals map[string]value.Value) (err error) {
	if globals == nil {
		return vm.InterpretWithEnvironment(c, vm.shared.Root)
	}
	environment := value.NewGlobalEnvironmentFrom(globals, vm.shared.Root)
	defer func() {
		for name := range globals {
			delete(globals, name)
		}
		for name, item := range environment.LocalSnapshot() {
			globals[name] = item
		}
	}()
	return vm.InterpretWithEnvironment(c, environment)
}

func (vm *VM) InterpretWithEnvironment(c *chunk.Chunk, environment *value.GlobalEnvironment) error {
	if environment == nil {
		return fmt.Errorf("interpret requires a global environment")
	}
	scriptFunction := &value.ObjFunction{Name: "script", Arity: 0, Chunk: c, Environment: environment}
	scriptClosure := &value.ObjClosure{Function: scriptFunction, Upvalues: []*value.ObjUpvalue{}, Environment: environment}
	vm.stackTop = 0
	vm.push(value.Value{Type: value.VAL_FUNCTION, Obj: scriptClosure})
	frame := &CallFrame{Closure: scriptClosure, IP: 0, StackBase: 0, LocalBase: 1, Environment: environment}
	vm.frames[0] = frame
	vm.frameCount = 1
	vm.currentFrame = frame
	return vm.run(1, nil)
}

func (vm *VM) run(minFrameCount int, terminalResult *value.Value) (err error) {
	// Cache current frame values for speed
	frame := vm.currentFrame
	c := frame.Closure.Function.Chunk.(*chunk.Chunk)
	ip := frame.IP
	defer func() {
		if vm.currentFrame == frame {
			frame.IP = ip
		}
		if err != nil {
			err = vm.unwindTo(minFrameCount-1, frameOutcome{Err: err}).Err
		}
	}()

	for {
		if ip >= len(c.Code) {
			return nil
		}

		instruction := chunk.OpCode(c.Code[ip])
		ip++

		switch instruction {
		case chunk.OP_CONSTANT:
			// Read constant
			index := c.Code[ip]
			ip++
			constant := c.Constants[index]

			// If it's a function, bind it to the current environment.
			if constant.Type == value.VAL_FUNCTION {
				fn := constant.Obj.(*value.ObjFunction)
				// Clone so compiler constants remain unbound and reusable.
				boundFn := &value.ObjFunction{
					Name:         fn.Name,
					Arity:        fn.Arity,
					UpvalueCount: fn.UpvalueCount,
					Params:       fn.Params,
					Chunk:        fn.Chunk,
					Environment:  frame.Environment,
					RuntimeType:  fn.RuntimeType,
				}
				vm.push(value.Value{Type: value.VAL_FUNCTION, Obj: boundFn})
			} else {
				vm.push(constant)
			}

		case chunk.OP_CONSTANT_LONG:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			constant := c.Constants[index]

			if constant.Type == value.VAL_FUNCTION {
				fn := constant.Obj.(*value.ObjFunction)
				boundFn := &value.ObjFunction{
					Name:         fn.Name,
					Arity:        fn.Arity,
					UpvalueCount: fn.UpvalueCount,
					Params:       fn.Params,
					Chunk:        fn.Chunk,
					Environment:  frame.Environment,
					RuntimeType:  fn.RuntimeType,
				}
				vm.push(value.Value{Type: value.VAL_FUNCTION, Obj: boundFn})
			} else {
				vm.push(constant)
			}

		case chunk.OP_NULL:
			vm.push(value.NewNull())

		case chunk.OP_JUMP:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			ip += offset

		case chunk.OP_JUMP_IF_FALSE:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			condition := vm.peek(0)
			if condition.Type == value.VAL_BOOL && !condition.AsBool {
				ip += offset
			}

		case chunk.OP_JUMP_IF_TRUE:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			condition := vm.peek(0)
			if condition.Type == value.VAL_BOOL && condition.AsBool {
				ip += offset
			}

		case chunk.OP_LOOP:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			ip -= offset

		case chunk.OP_TRUE:
			vm.push(value.NewBool(true))
		case chunk.OP_FALSE:
			vm.push(value.NewBool(false))
		case chunk.OP_POP:
			vm.LastPopped = vm.pop()

		case chunk.OP_ADDR:
			val := vm.pop()
			if val.Type == value.VAL_REF {
				ref, err := extractReferenceValue(val)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				if _, _, _, err := vm.referenceStorage(ref); err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				addrStr := ""
				switch ref.RefType {
				case value.REF_PTR:
					addrStr = fmt.Sprintf("%p", ref.Ptr)
				case value.REF_GLOBAL:
					// For global references, display the name as the address proxy.
					addrStr = fmt.Sprintf("<global %s>", ref.Name)
				case value.REF_UPVALUE:
					address, ok := ref.Upvalue.LocationAddress()
					if !ok {
						return vm.runtimeError(c, ip, "invalid upvalue reference")
					}
					addrStr = address
				case value.REF_PROPERTY:
					containerAddr := fmt.Sprintf("%p", ref.Container.Obj)
					addrStr = fmt.Sprintf("<prop %s of %s>", ref.Name, containerAddr)
				case value.REF_INDEX:
					addrStr = fmt.Sprintf("<index %s>", ref.Index.String())
				}
				vm.push(value.NewString(addrStr))
			} else {
				// For non-reference values, return the address of the transient value.
				vm.push(value.NewString(fmt.Sprintf("%p", &val)))
			}

		case chunk.OP_GET_GLOBAL:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)

			val, ok := frame.Environment.Resolve(name)
			if !ok {
				return vm.runtimeError(c, ip, "undefined global variable '%s'", name)
			}
			vm.push(val)

		case chunk.OP_SET_GLOBAL:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)
			frame.Environment.SetLocal(name, vm.peek(0))

		case chunk.OP_GET_LOCAL:
			slot := c.Code[ip]
			ip++
			val := vm.stack[frame.LocalBase+int(slot)]
			vm.push(val)

		case chunk.OP_SET_LOCAL:
			slot := c.Code[ip]
			ip++
			vm.stack[frame.LocalBase+int(slot)] = vm.peek(0)

		case chunk.OP_REF_LOCAL:
			slot := int(c.Code[ip])
			ip++
			// Reference to a stack slot - Capture it!
			upvalue := vm.captureUpvalue(&vm.stack[frame.LocalBase+slot])
			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType: value.REF_UPVALUE,
					Upvalue: upvalue,
				},
			})

		case chunk.OP_REF_UPVALUE:
			slot := int(c.Code[ip])
			ip++
			if slot < 0 || slot >= len(frame.Closure.Upvalues) {
				return vm.runtimeError(c, ip, "upvalue reference index out of bounds: %d", slot)
			}
			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType: value.REF_UPVALUE,
					Upvalue: frame.Closure.Upvalues[slot],
				},
			})

		case chunk.OP_REF_GLOBAL:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)

			owner, ok := frame.Environment.ResolveOwner(name)
			if !ok {
				return vm.runtimeError(c, ip, "undefined global variable '%s'", name)
			}
			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType:     value.REF_GLOBAL,
					Name:        name,
					GlobalOwner: owner,
				},
			})

		case chunk.OP_REF_PROPERTY:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)

			// Pop Container (Object/Struct)
			container := vm.pop()

			// Auto-dereference if container is a ref
			if container.Type == value.VAL_REF {
				resolved, err := vm.resolveReferenceValue(container)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				container = resolved
			}

			// Now check container type
			if container.Type != value.VAL_OBJ {
				return vm.runtimeError(c, ip, "Property reference base must be an object")
			}

			// Debug: Check ID if Node
			/*(
			  if inst, ok := container.Obj.(*value.ObjInstance); ok {
			       if idVal, hasId := inst.Fields["id"]; hasId {
			           fmt.Printf("VM REF_PROPERTY: %s on Node[%v]\n", name, idVal)
			       }
			  }
			*/

			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType:   value.REF_PROPERTY,
					Container: container,
					Name:      name,
				},
			})

		case chunk.OP_REF_INDEX:
			// Pop Index, then Container
			idx := vm.pop()
			container := vm.pop()

			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType:   value.REF_INDEX,
					Container: container,
					Index:     idx,
				},
			})

		case chunk.OP_CONTEXT_REF_PROPERTY:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)
			container := vm.pop()

			instance, ok := container.Obj.(*value.ObjInstance)
			if container.Type != value.VAL_OBJ || !ok || instance == nil {
				return vm.runtimeError(c, ip, "contextual property reference base must be an instance")
			}
			stored, ok := instance.Fields[name]
			if !ok {
				return vm.runtimeError(c, ip, "undefined property '%s'", name)
			}
			if stored.Type == value.VAL_REF {
				vm.push(stored)
				continue
			}
			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType:   value.REF_PROPERTY,
					Container: container,
					Name:      name,
				},
			})

		case chunk.OP_CONTEXT_REF_INDEX:
			idx := vm.pop()
			container := vm.pop()
			if container.Type != value.VAL_OBJ {
				return vm.runtimeError(c, ip, "contextual index reference base must be an array or map")
			}

			var stored value.Value
			if array, ok := container.Obj.(*value.ObjArray); ok && array != nil {
				if idx.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				arrayIndex := int(idx.AsInt)
				if arrayIndex < 0 || arrayIndex >= len(array.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				stored = array.Elements[arrayIndex]
			} else if mapObj, ok := container.Obj.(*value.ObjMap); ok && mapObj != nil {
				key, err := referenceMapKey(idx)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				stored, _ = mapObj.Get(key)
			} else {
				return vm.runtimeError(c, ip, "contextual index reference base must be an array or map")
			}

			if stored.Type == value.VAL_REF {
				vm.push(stored)
				continue
			}
			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType:   value.REF_INDEX,
					Container: container,
					Index:     idx,
				},
			})

		case chunk.OP_MARK_REF_JSON_DYNAMIC:
			refValue := vm.peek(0)
			ref, ok := refValue.Obj.(*value.ObjRef)
			if refValue.Type != value.VAL_REF || !ok || ref == nil {
				return vm.runtimeError(c, ip, "dynamic target marker requires a reference")
			}
			ref.JSONDynamic.Store(true)

		case chunk.OP_MARK_REF_TARGET_TYPE:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			typeValue := c.Constants[index]
			targetType, ok := typeValue.Obj.(*value.RuntimeTypeInfo)
			if typeValue.Type != value.VAL_OBJ || !ok || targetType == nil {
				return vm.runtimeError(c, ip, "reference target marker requires runtime type metadata")
			}
			refValue := vm.peek(0)
			if refValue.Type == value.VAL_NULL {
				continue
			}
			ref, ok := refValue.Obj.(*value.ObjRef)
			if refValue.Type != value.VAL_REF || !ok || ref == nil {
				return vm.runtimeError(c, ip, "reference target marker requires a reference")
			}
			if !markReferenceTargetType(ref, targetType) {
				return vm.runtimeError(c, ip, "reference target metadata conflicts with static context")
			}

		case chunk.OP_MARK_RUNTIME_VALUE_TYPE:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			typeValue := c.Constants[index]
			runtimeType, ok := typeValue.Obj.(*value.RuntimeTypeInfo)
			if typeValue.Type != value.VAL_OBJ || !ok || runtimeType == nil {
				return vm.runtimeError(c, ip, "runtime value marker requires type metadata")
			}
			if !vm.markRuntimeValueType(vm.peek(0), runtimeType) {
				return vm.runtimeError(c, ip, "runtime value metadata conflicts with static context")
			}

		case chunk.OP_DEREF:
			refVal := vm.pop()
			if refVal.Type == value.VAL_NULL {
				vm.push(refVal) // Passthrough null
			} else if refVal.Type != value.VAL_REF {
				// Not a ref - pass through as-is (already dereferenced)
				vm.push(refVal)
			} else {
				resolved, err := vm.resolveReferenceValue(refVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				vm.push(resolved)
			}
		case chunk.OP_STORE_VIA_REF:
			slot := int(c.Code[ip])
			ip++
			val := vm.pop() // Value to assign

			// The reference itself is in a local variable (e.g., parameter 'x')
			refVal := vm.stack[frame.LocalBase+slot]

			if err := vm.storeReferenceValue(refVal, val); err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
		case chunk.OP_STORE_REF:
			val := vm.pop()    // Value to assign
			refVal := vm.pop() // The reference itself (popped from stack)

			if err := vm.storeReferenceValue(refVal, val); err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}

		case chunk.OP_ADD:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewInt(a.AsInt + b.AsInt))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(a.AsFloat + b.AsFloat))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(float64(a.AsInt) + b.AsFloat))

			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewFloat(a.AsFloat + float64(b.AsInt)))
			} else if a.Type == value.VAL_OBJ && b.Type == value.VAL_OBJ {
				// Check if both are strings
				strA, okA := a.Obj.(string)
				strB, okB := b.Obj.(string)
				if okA && okB {
					vm.push(value.NewString(strA + strB))
					continue // Added continue for cleaner flow
				}
				// VAL_BYTES types are stored internally as strings.
				if a.Type == value.VAL_BYTES && b.Type == value.VAL_BYTES {
					vm.push(value.NewBytes(a.Obj.(string) + b.Obj.(string)))
					continue
				}

				return vm.runtimeError(c, ip, "operands must be numbers, strings or bytes")
			} else if a.Type == value.VAL_BYTES && b.Type == value.VAL_BYTES {
				// Case where types are explicit VAL_BYTES (not VAL_OBJ)
				vm.push(value.NewBytes(a.Obj.(string) + b.Obj.(string)))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers or strings or bytes")
			}

		case chunk.OP_ADD_INT:
			// Inline pop/pop/push for optimization
			// b is at stackTop-1, a is at stackTop-2
			// result replaces a (at stackTop-2)
			// b (at stackTop-1) is cleared
			// stackTop decrements by 1
			vm.stack[vm.stackTop-2] = value.NewInt(vm.stack[vm.stackTop-2].AsInt + vm.stack[vm.stackTop-1].AsInt)
			vm.stack[vm.stackTop-1] = value.Value{}
			vm.stackTop--

		case chunk.OP_SUBTRACT:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewInt(a.AsInt - b.AsInt))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(a.AsFloat - b.AsFloat))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(float64(a.AsInt) - b.AsFloat))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewFloat(a.AsFloat - float64(b.AsInt)))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers")
			}
		case chunk.OP_SUB_INT:
			b := vm.pop()
			a := vm.pop()
			vm.push(value.NewInt(a.AsInt - b.AsInt))
		case chunk.OP_MULTIPLY:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewInt(a.AsInt * b.AsInt))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(a.AsFloat * b.AsFloat))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(float64(a.AsInt) * b.AsFloat))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewFloat(a.AsFloat * float64(b.AsInt)))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers")
			}
		case chunk.OP_MUL_INT:
			b := vm.pop()
			a := vm.pop()
			vm.push(value.NewInt(a.AsInt * b.AsInt))
		case chunk.OP_DIVIDE:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				if b.AsInt == 0 {
					return vm.runtimeError(c, ip, "division by zero")
				}
				vm.push(value.NewInt(a.AsInt / b.AsInt))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				if b.AsFloat == 0 {
					return vm.runtimeError(c, ip, "division by zero")
				}
				vm.push(value.NewFloat(a.AsFloat / b.AsFloat))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				if b.AsFloat == 0 {
					return vm.runtimeError(c, ip, "division by zero")
				}
				vm.push(value.NewFloat(float64(a.AsInt) / b.AsFloat))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				if b.AsInt == 0 {
					return vm.runtimeError(c, ip, "division by zero")
				}
				vm.push(value.NewFloat(a.AsFloat / float64(b.AsInt)))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers")
			}
		case chunk.OP_DIV_INT:
			b := vm.pop()
			a := vm.pop()
			if b.AsInt == 0 {
				return vm.runtimeError(c, ip, "division by zero")
			}
			vm.push(value.NewInt(a.AsInt / b.AsInt))
		case chunk.OP_LEN:
			val := vm.pop()
			if val.Type == value.VAL_OBJ {
				if arr, ok := val.Obj.(*value.ObjArray); ok {
					vm.push(value.NewInt(int64(len(arr.Elements))))
				} else if m, ok := val.Obj.(*value.ObjMap); ok {
					vm.push(value.NewInt(int64(m.Len())))
				} else if s, ok := val.Obj.(string); ok {
					vm.push(value.NewInt(int64(utf8.RuneCountInString(s))))
				} else {
					vm.push(value.NewInt(0)) // Or error?
				}
			} else if val.Type == value.VAL_BYTES {
				// Bytes stored as string in Obj
				s := val.Obj.(string)
				vm.push(value.NewInt(int64(len(s))))
			} else {
				vm.push(value.NewInt(0))
			}

		case chunk.OP_SELECT:
			count := int(c.Code[ip])
			ip++
			cases := make([]reflect.SelectCase, count)
			// Stack layout: [... Case0_Chan, Case0_Val, Case0_Mode ... CaseN_Chan, CaseN_Val, CaseN_Mode]
			// Top is CaseN_Mode.
			// Iterating i from count-1 down to 0:
			for i := count - 1; i >= 0; i-- {
				mode := vm.pop().AsInt
				val := vm.pop()
				chVal := vm.pop()

				if mode == 2 { // Default
					cases[i] = reflect.SelectCase{Dir: reflect.SelectDefault}
				} else if mode == 1 { // Send
					if chVal.Type != value.VAL_CHANNEL {
						return vm.runtimeError(c, ip, "select case expects channel")
					}
					ch := chVal.Obj.(*value.ObjChannel).Chan
					cases[i] = reflect.SelectCase{Dir: reflect.SelectSend, Chan: reflect.ValueOf(ch), Send: reflect.ValueOf(val)}
				} else { // Recv
					if chVal.Type != value.VAL_CHANNEL {
						return vm.runtimeError(c, ip, "select case expects channel")
					}
					ch := chVal.Obj.(*value.ObjChannel).Chan
					cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
				}
			}

			chosenIndex, recvVal, recvOK := reflect.Select(cases)

			vm.push(value.NewInt(int64(chosenIndex)))

			var valToPush value.Value
			if recvOK {
				if v, ok := recvVal.Interface().(value.Value); ok {
					valToPush = v
				} else {
					valToPush = value.NewNull()
				}
			} else {
				valToPush = value.NewNull()
			}
			vm.push(valToPush)
			vm.push(value.NewBool(recvOK))

		case chunk.OP_MODULO:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				if b.AsInt == 0 {
					return vm.runtimeError(c, ip, "modulo by zero")
				}
				vm.push(value.NewInt(a.AsInt % b.AsInt))
			} else {
				return vm.runtimeError(c, ip, "operands for %% must be integers")
			}
		case chunk.OP_MOD_INT:
			// Inline pop/pop/push
			b := vm.stack[vm.stackTop-1].AsInt
			a := vm.stack[vm.stackTop-2].AsInt
			if b == 0 {
				return vm.runtimeError(c, ip, "modulo by zero")
			}
			vm.stack[vm.stackTop-2] = value.NewInt(a % b)
			vm.stack[vm.stackTop-1] = value.Value{}
			vm.stackTop--

		case chunk.OP_BIT_AND:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewInt(a.AsInt & b.AsInt))
			} else if a.Type == value.VAL_BYTES && b.Type == value.VAL_BYTES {
				sA := a.Obj.(string)
				sB := b.Obj.(string)
				if len(sA) != len(sB) {
					return vm.runtimeError(c, ip, "operands for & must have same length")
				}
				res := make([]byte, len(sA))
				for i := 0; i < len(sA); i++ {
					res[i] = sA[i] & sB[i]
				}
				vm.push(value.NewBytes(string(res)))
			} else {
				return vm.runtimeError(c, ip, "operands for & must be integers or bytes")
			}

		case chunk.OP_BIT_OR:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewInt(a.AsInt | b.AsInt))
			} else if a.Type == value.VAL_BYTES && b.Type == value.VAL_BYTES {
				sA := a.Obj.(string)
				sB := b.Obj.(string)
				if len(sA) != len(sB) {
					return vm.runtimeError(c, ip, "operands for | must have same length")
				}
				res := make([]byte, len(sA))
				for i := 0; i < len(sA); i++ {
					res[i] = sA[i] | sB[i]
				}
				vm.push(value.NewBytes(string(res)))
			} else {
				return vm.runtimeError(c, ip, "operands for | must be integers or bytes")
			}

		case chunk.OP_BIT_XOR:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewInt(a.AsInt ^ b.AsInt))
			} else if a.Type == value.VAL_BYTES && b.Type == value.VAL_BYTES {
				sA := a.Obj.(string)
				sB := b.Obj.(string)
				if len(sA) != len(sB) {
					return vm.runtimeError(c, ip, "operands for ^ must have same length")
				}
				res := make([]byte, len(sA))
				for i := 0; i < len(sA); i++ {
					res[i] = sA[i] ^ sB[i]
				}
				vm.push(value.NewBytes(string(res)))
			} else {
				return vm.runtimeError(c, ip, "operands for ^ must be integers or bytes")
			}

		case chunk.OP_BIT_NOT:
			a := vm.pop()
			if a.Type == value.VAL_INT {
				vm.push(value.NewInt(^a.AsInt))
			} else if a.Type == value.VAL_BYTES {
				sA := a.Obj.(string)
				res := make([]byte, len(sA))
				for i := 0; i < len(sA); i++ {
					res[i] = ^sA[i]
				}
				vm.push(value.NewBytes(string(res)))
			} else {
				return vm.runtimeError(c, ip, "operand for ~ must be integer or bytes")
			}

		case chunk.OP_SHIFT_LEFT:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				if b.AsInt < 0 {
					return vm.runtimeError(c, ip, "negative shift count")
				}
				vm.push(value.NewInt(a.AsInt << uint64(b.AsInt)))
			} else {
				return vm.runtimeError(c, ip, "operands for << must be integers")
			}

		case chunk.OP_SHIFT_RIGHT:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				if b.AsInt < 0 {
					return vm.runtimeError(c, ip, "negative shift count")
				}
				vm.push(value.NewInt(a.AsInt >> uint64(b.AsInt)))
			} else {
				return vm.runtimeError(c, ip, "operands for >> must be integers")
			}
		case chunk.OP_NEGATE:
			v := vm.pop()
			if v.Type == value.VAL_INT {
				vm.push(value.NewInt(-v.AsInt))
			} else if v.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(-v.AsFloat))
			} else {
				return vm.runtimeError(c, ip, "operand must be number")
			}
		case chunk.OP_NOT:
			v := vm.pop()
			if v.Type == value.VAL_BOOL {
				vm.push(value.NewBool(!v.AsBool))
			} else {
				return vm.runtimeError(c, ip, "operand must be boolean")
			}
		case chunk.OP_AND:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_BOOL && b.Type == value.VAL_BOOL {
				vm.push(value.NewBool(a.AsBool && b.AsBool))
			} else {
				return vm.runtimeError(c, ip, "operands for & must be boolean")
			}
		case chunk.OP_OR:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_BOOL && b.Type == value.VAL_BOOL {
				vm.push(value.NewBool(a.AsBool || b.AsBool))
			} else {
				return vm.runtimeError(c, ip, "operands for | must be boolean")
			}
		case chunk.OP_ZEROS:
			countVal := vm.pop()
			if countVal.Type != value.VAL_INT {
				return vm.runtimeError(c, ip, "zeros size must be integer")
			}
			count := int(countVal.AsInt)
			elements := make([]value.Value, count)
			for i := 0; i < count; i++ {
				elements[i] = value.NewInt(0)
			}
			vm.push(value.NewArray(elements))
		case chunk.OP_GREATER:
			b := vm.pop()
			a := vm.pop()
			// Only supporting int/float comparison for now
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.AsInt > b.AsInt))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(a.AsFloat > b.AsFloat))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(float64(a.AsInt) > b.AsFloat))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.AsFloat > float64(b.AsInt)))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers")
			}
		case chunk.OP_GREATER_INT:
			b := vm.pop()
			a := vm.pop()
			vm.push(value.NewBool(a.AsInt > b.AsInt))
		case chunk.OP_LESS:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.AsInt < b.AsInt))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(a.AsFloat < b.AsFloat))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(float64(a.AsInt) < b.AsFloat))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.AsFloat < float64(b.AsInt)))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers")
			}
		case chunk.OP_LESS_INT:
			// Inline pop/pop/push
			vm.stack[vm.stackTop-2] = value.NewBool(vm.stack[vm.stackTop-2].AsInt < vm.stack[vm.stackTop-1].AsInt)
			vm.stack[vm.stackTop-1] = value.Value{}
			vm.stackTop--
		case chunk.OP_EQUAL:
			b := vm.pop()
			a := vm.pop()
			vm.push(value.NewBool(valuesEqual(a, b)))
		case chunk.OP_EQUAL_INT:
			b := vm.pop()
			a := vm.pop()
			vm.push(value.NewBool(a.AsInt == b.AsInt))
		case chunk.OP_PRINT:
			v := vm.pop()
			if v.Type == value.VAL_REF {
				ref, err := extractReferenceValue(v)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				if _, _, _, err := vm.referenceStorage(ref); err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
			}
			fmt.Println(v)

		case chunk.OP_CALL:
			argCount := int(c.Code[ip])
			ip++

			frame.IP = ip // Save current instruction pointer to the frame before call

			if ok, err := vm.callValue(vm.peek(argCount), argCount, c, ip); !ok {
				return err
			}
			// Update cached frame
			frame = vm.currentFrame // Switch to new frame
			c = frame.Closure.Function.Chunk.(*chunk.Chunk)
			ip = frame.IP

		case chunk.OP_DEFER:
			registration := sourceLocation(c, ip)
			argCount := int(c.Code[ip])
			ip++

			frame.IP = ip
			callee := vm.peek(argCount)
			arguments := vm.stack[vm.stackTop-argCount : vm.stackTop]
			prepared, err := vm.prepareDeferredCall(callee, arguments, registration)
			if err != nil {
				return &RuntimeError{
					Location: registration,
					Message:  "failed to register defer",
					Cause:    err,
					Stack:    vm.captureNoxyStack(c, ip),
				}
			}

			operandBase := vm.stackTop - argCount - 1
			for index := operandBase; index < vm.stackTop; index++ {
				vm.stack[index] = value.Value{}
			}
			vm.stackTop = operandBase
			frame.Deferred = append(frame.Deferred, prepared)

		case chunk.OP_CLOSURE:
			idx := c.Code[ip]
			ip++
			fnVal := c.Constants[idx]
			fn := fnVal.Obj.(*value.ObjFunction)
			boundFn := &value.ObjFunction{
				Name:         fn.Name,
				Arity:        fn.Arity,
				UpvalueCount: fn.UpvalueCount,
				Params:       fn.Params,
				Chunk:        fn.Chunk,
				Environment:  frame.Environment,
				RuntimeType:  fn.RuntimeType,
			}

			closure := &value.ObjClosure{
				Function:    boundFn,
				Upvalues:    make([]*value.ObjUpvalue, fn.UpvalueCount),
				Environment: frame.Environment,
			}

			for i := 0; i < fn.UpvalueCount; i++ {
				isLocal := c.Code[ip]
				ip++
				index := c.Code[ip]
				ip++

				if isLocal == 1 {
					closure.Upvalues[i] = vm.captureUpvalue(&vm.stack[frame.LocalBase+int(index)])
				} else {
					closure.Upvalues[i] = frame.Closure.Upvalues[index]
				}
			}
			vm.push(value.Value{Type: value.VAL_FUNCTION, Obj: closure})

		case chunk.OP_GET_UPVALUE:
			slot := c.Code[ip]
			ip++
			val, ok := frame.Closure.Upvalues[slot].Load()
			if !ok {
				return vm.runtimeError(c, ip, "invalid upvalue")
			}
			vm.push(val)

		case chunk.OP_SET_UPVALUE:
			slot := c.Code[ip]
			ip++
			if !frame.Closure.Upvalues[slot].Store(vm.peek(0)) {
				return vm.runtimeError(c, ip, "invalid upvalue")
			}

		case chunk.OP_CLOSE_UPVALUE:
			vm.closeUpvalue(&vm.stack[vm.stackTop-1])
			vm.pop()

		case chunk.OP_RETURN:
			result := vm.pop()
			frame.IP = ip
			outcome := vm.finishFrame(frameOutcome{Result: result})
			if outcome.Err != nil {
				return outcome.Err
			}

			if vm.frameCount < minFrameCount {
				if terminalResult != nil {
					*terminalResult = outcome.Result
				}
				return nil
			}

			frame = vm.currentFrame
			c = frame.Closure.Function.Chunk.(*chunk.Chunk)
			ip = frame.IP

		case chunk.OP_ARRAY:
			count := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2

			elements := make([]value.Value, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
			}
			vm.push(value.NewArray(elements))

		case chunk.OP_MAP:
			count := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2

			// Map expects keys and values on stack: K1, V1, K2, V2...
			mapObj := value.NewMap()
			mapping := mapObj.Obj.(*value.ObjMap)

			for i := 0; i < count; i++ {
				val := vm.pop()
				keyVal := vm.pop()

				var key interface{}
				if keyVal.Type == value.VAL_INT {
					key = keyVal.AsInt
				} else if keyVal.Type == value.VAL_OBJ {
					if str, ok := keyVal.Obj.(string); ok {
						key = str
					} else {
						return vm.runtimeError(c, ip, "map key must be int or string")
					}
				} else {
					return vm.runtimeError(c, ip, "map key must be int or string")
				}
				mapping.Set(key, val)
			}
			vm.push(mapObj)

		case chunk.OP_DUP:
			vm.push(vm.peek(0))

		case chunk.OP_IMPORT:
			index := c.Code[ip]
			ip++
			nameConstant := c.Constants[index]
			moduleName := nameConstant.Obj.(string)

			frame.IP = ip
			mod, err := vm.loadModule(moduleName)
			if err != nil {
				return vm.runtimeErrorCause(c, ip, err, "failed to import module '%s'", moduleName)
			}
			frame = vm.currentFrame
			c = frame.Closure.Function.Chunk.(*chunk.Chunk)
			ip = frame.IP
			vm.push(mod)

		case chunk.OP_IMPORT_FROM_ALL:
			modVal := vm.pop()
			if modVal.Type == value.VAL_OBJ {
				if modMap, ok := modVal.Obj.(*value.ObjMap); ok {
					for k, v := range modMap.Snapshot() {
						if keyStr, ok := k.(string); ok {
							frame.Environment.SetLocal(keyStr, v)
						}
					}
				} else {
					return vm.runtimeError(c, ip, "import * expected a module (map)")
				}
			} else {
				return vm.runtimeError(c, ip, "import * expected a module object")
			}

		case chunk.OP_GET_INDEX:
			indexVal := vm.pop()
			collectionVal := vm.pop()

			if collectionVal.Type == value.VAL_OBJ {
				if arr, ok := collectionVal.Obj.(*value.ObjArray); ok {
					if indexVal.Type != value.VAL_INT {
						return vm.runtimeError(c, ip, "array index must be integer")
					}
					idx := int(indexVal.AsInt)
					if idx < 0 || idx >= len(arr.Elements) {
						return vm.runtimeError(c, ip, "array index out of bounds")
					}
					vm.push(arr.Elements[idx])
					continue
				} else if mapObj, ok := collectionVal.Obj.(*value.ObjMap); ok {
					var key interface{}
					if indexVal.Type == value.VAL_INT {
						key = indexVal.AsInt
					} else if indexVal.Type == value.VAL_OBJ {
						if str, ok := indexVal.Obj.(string); ok {
							key = str
						} else {
							return vm.runtimeError(c, ip, "map key must be int or string")
						}
					} else {
						return vm.runtimeError(c, ip, "map key must be int or string")
					}

					val, ok := mapObj.Get(key)
					if !ok {
						vm.push(value.NewNull())
					} else {
						vm.push(val)
					}
					continue
				} else if str, ok := collectionVal.Obj.(string); ok {
					// String indexing
					if indexVal.Type != value.VAL_INT {
						return vm.runtimeError(c, ip, "string index must be integer")
					}
					idx := int(indexVal.AsInt)
					runes := []rune(str) // Expensive but correct for now
					if idx < 0 || idx >= len(runes) {
						return vm.runtimeError(c, ip, "string index out of bounds")
					}
					vm.push(value.NewString(string(runes[idx])))
					continue
				}
			}
			// Check if it's a bytes value
			if collectionVal.Type == value.VAL_BYTES {
				str := collectionVal.Obj.(string)
				if indexVal.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "bytes index must be integer")
				}
				idx := int(indexVal.AsInt)
				if idx < 0 || idx >= len(str) {
					return vm.runtimeError(c, ip, "bytes index out of bounds")
				}
				vm.push(value.NewInt(int64(str[idx])))
				continue
			}
			return vm.runtimeError(c, ip, "cannot index non-array/map/bytes")

		case chunk.OP_SET_INDEX:
			val := vm.pop()
			indexVal := vm.pop()
			collectionVal := vm.pop() // The array/map itself is on stack (pointer)

			if collectionVal.Type == value.VAL_OBJ {
				if arr, ok := collectionVal.Obj.(*value.ObjArray); ok {
					if indexVal.Type != value.VAL_INT {
						return vm.runtimeError(c, ip, "array index must be integer")
					}
					idx := int(indexVal.AsInt)
					if idx < 0 || idx >= len(arr.Elements) {
						return vm.runtimeError(c, ip, "array index out of bounds")
					}
					arr.Elements[idx] = val
					vm.push(val) // Assignment expression result
					continue
				} else if mapObj, ok := collectionVal.Obj.(*value.ObjMap); ok {
					var key interface{}
					if indexVal.Type == value.VAL_INT {
						key = indexVal.AsInt
					} else if indexVal.Type == value.VAL_OBJ {
						if str, ok := indexVal.Obj.(string); ok {
							key = str
						} else {
							return vm.runtimeError(c, ip, "map key must be int or string")
						}
					} else {
						return vm.runtimeError(c, ip, "map key must be int or string")
					}
					mapObj.Set(key, val)
					vm.push(val)
					continue
				}
			}
			return vm.runtimeError(c, ip, "cannot set index on non-array/map")

		case chunk.OP_GET_PROPERTY:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)

			instanceVal := vm.pop()

			// Auto-dereference if instance is a ref
			if instanceVal.Type == value.VAL_REF {
				resolved, err := vm.resolveReferenceValue(instanceVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				instanceVal = resolved
			}

			if instanceVal.Type != value.VAL_OBJ {
				return vm.runtimeError(c, ip, "only instances/maps have properties")
			}

			if instance, ok := instanceVal.Obj.(*value.ObjInstance); ok {
				val, ok := instance.Fields[name]
				if !ok {
					return vm.runtimeError(c, ip, "undefined property '%s'", name)
				}
				vm.push(val)
			} else if mapObj, ok := instanceVal.Obj.(*value.ObjMap); ok {
				// Allow accessing map keys as properties (for modules)
				val, ok := mapObj.Get(name)
				if !ok {
					return vm.runtimeError(c, ip, "undefined property '%s' in module/map", name)
				}
				vm.push(val)
			} else {
				return vm.runtimeError(c, ip, "only instances and maps have properties")
			}

		case chunk.OP_SET_PROPERTY:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)

			val := vm.pop()
			instanceVal := vm.pop()

			// Auto-dereference if instance is a ref

			if instanceVal.Type == value.VAL_REF {
				resolved, err := vm.resolveReferenceValue(instanceVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				instanceVal = resolved
			}

			if instanceVal.Type != value.VAL_OBJ {
				return vm.runtimeError(c, ip, "only instances have properties")
			}
			instance, ok := instanceVal.Obj.(*value.ObjInstance)
			if !ok {
				return vm.runtimeError(c, ip, "only instances have properties")
			}

			instance.Fields[name] = val
			vm.push(val)

		case chunk.OP_SET_PROPERTY_DEREF:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)

			val := vm.pop()
			instanceVal := vm.pop()

			// Expect Instance (Can be Ref to Instance)
			if instanceVal.Type == value.VAL_REF {
				resolved, err := vm.resolveReferenceValue(instanceVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				instanceVal = resolved
			}

			if instanceVal.Type != value.VAL_OBJ {
				return vm.runtimeError(c, ip, "only instances have properties")
			}
			instance, ok := instanceVal.Obj.(*value.ObjInstance)
			if !ok {
				return vm.runtimeError(c, ip, "only instances have properties")
			}

			// Get Field - EXPECTING REFERENCE
			fieldVal, ok := instance.Fields[name]
			if !ok {
				return vm.runtimeError(c, ip, "undefined property '%s'", name)
			}

			if fieldVal.Type != value.VAL_REF {
				return vm.runtimeError(c, ip, "property '%s' is not a reference", name)
			}
			if err := vm.storeReferenceValue(fieldVal, val); err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
			vm.push(val)

		case chunk.OP_SWAP:
			// Swap top two stack elements: [a, b] -> [b, a]
			b := vm.pop()
			a := vm.pop()
			vm.push(b)
			vm.push(a)

		case chunk.OP_COPY:
			val := vm.pop()
			vm.push(vm.copyValue(val))
		}
	}
}
