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
	frame := &vm.frames[0]
	*frame = CallFrame{Closure: scriptClosure, IP: 0, StackBase: 0, LocalBase: 1, Environment: environment, Deferred: frame.Deferred[:0], Owned: frame.Owned[:0]}
	vm.frameCount = 1
	vm.currentFrame = frame
	return vm.run(1, nil)
}

func (vm *VM) run(minFrameCount int, terminalResult *value.Value) (err error) {
	// Cache current frame values for speed
	frame := vm.currentFrame
	c := frame.Closure.Function.Chunk.(*chunk.Chunk)
	gcache := c.GlobalCache()
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

		// perf fase 1: comparacao int + salto fundidos. Consomem os dois
		// VAL_INT do topo (sem zerar: escalares nao carregam ponteiros para o
		// GC reter) e saltam quando a condicao NOMEADA vale.
		case chunk.OP_JUMP_IF_LT_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].AsInt < vm.stack[vm.stackTop+1].AsInt {
				ip += offset
			}

		case chunk.OP_JUMP_IF_LE_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].AsInt <= vm.stack[vm.stackTop+1].AsInt {
				ip += offset
			}

		case chunk.OP_JUMP_IF_GT_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].AsInt > vm.stack[vm.stackTop+1].AsInt {
				ip += offset
			}

		case chunk.OP_JUMP_IF_GE_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].AsInt >= vm.stack[vm.stackTop+1].AsInt {
				ip += offset
			}

		case chunk.OP_JUMP_IF_EQ_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].AsInt == vm.stack[vm.stackTop+1].AsInt {
				ip += offset
			}

		case chunk.OP_JUMP_IF_NE_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].AsInt != vm.stack[vm.stackTop+1].AsInt {
				ip += offset
			}

		case chunk.OP_INC_LOCAL_INT:
			// perf fase 1: soma o delta direto no slot, sem empilhar/desempilhar
			// nada. RC: int e escalar (Retain/Release de OP_SET_LOCAL sao no-op
			// para VAL_INT — ownersOf so rastreia VAL_OBJ), entao nao ha posse a
			// atualizar aqui — mesma escrita direta que OP_SET_LOCAL faria.
			slot := c.Code[ip]
			delta := int8(c.Code[ip+1])
			ip += 2
			vm.stack[frame.LocalBase+int(slot)].AsInt += int64(delta)

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
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			// Geração lida ANTES do Resolve: uma escrita concorrente entre os
			// dois avança a geração, então a entrada gravada com a geração
			// antiga falha a comparação na próxima leitura e re-resolve — o
			// cache pode sub-cachear, nunca servir valor stale.
			gen := frame.Environment.Generation()
			if entry := gcache[index].Load(); entry != nil && entry.Env == frame.Environment && entry.Gen == gen {
				vm.push(entry.Val)
				continue
			}
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)

			val, ok := frame.Environment.Resolve(name)
			if !ok {
				return vm.runtimeError(c, ip, "undefined global variable '%s'", name)
			}
			gcache[index].Store(&chunk.GlobalCacheEntry{Env: frame.Environment, Gen: gen, Val: val})
			vm.push(val)

		case chunk.OP_SET_GLOBAL:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)
			// RC: troca contada no ambiente (retain-antes-de-release; slots
			// globais nunca sao liberados por finalizeCurrentFrame, entao a
			// bookkeeping precisa acontecer aqui, no proprio funil de escrita).
			if old, ok := frame.Environment.GetLocal(name); ok {
				value.Retain(vm.peek(0))
				value.Release(old)
			} else {
				value.Retain(vm.peek(0))
			}
			frame.Environment.SetLocal(name, vm.peek(0))

		case chunk.OP_SET_GLOBAL_BORROW:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)
			// RC: global de tipo `ref` — empréstimo, não posse. Sem
			// retain/release: quem responde pelo objeto é o dono real
			// (campo, outro global, slot do chamador…). Contar aqui daria um
			// dono a mais e a mutação através do empréstimo clonaria.
			frame.Environment.SetLocal(name, vm.peek(0))

		case chunk.OP_GET_LOCAL:
			slot := c.Code[ip]
			ip++
			val := vm.stack[frame.LocalBase+int(slot)]
			vm.push(val)

		case chunk.OP_SET_LOCAL:
			slot := c.Code[ip]
			ip++
			idx := frame.LocalBase + int(slot)
			old := vm.stack[idx]
			vm.stack[idx] = vm.peek(0)
			// RC: retain-antes-de-release (auto-atribuicao x = x)
			frame.ownSlot(vm, idx)
			value.Release(old)

		case chunk.OP_SET_LOCAL_BORROW:
			slot := c.Code[ip]
			ip++
			// RC: rebind de local `ref` — empréstimo, não posse. Nada de
			// retain/release e nada registrado em frame.Owned: quem responde
			// pelo objeto é o dono real (campo, global, slot do chamador…).
			vm.stack[frame.LocalBase+int(slot)] = vm.peek(0)

		case chunk.OP_OWN_LOCAL:
			// RC: vinculo NOVO no slot — paga a entrada anterior do indice, se
			// houver (reuso entre iteracoes/blocos irmaos); ver bindOwnedSlot.
			frame.bindOwnedSlot(vm, vm.stackTop-1)

		case chunk.OP_REF_LOCAL, chunk.OP_REF_LOCAL_BORROW:
			slot := int(c.Code[ip])
			ip++
			// Reference to a stack slot - Capture it!
			// RC: a caixa nasce possuidora; o gemeo _BORROW (emitido quando o
			// slot capturado NAO retem o que guarda — hoje, apenas slot de tipo
			// `ref`) a marca como emprestada, e os funis de escrita via ref
			// param de contar posse nela. Decisao estatica: nada de consultar
			// frame.Owned.
			upvalue := vm.captureUpvalue(&vm.stack[frame.LocalBase+slot])
			if instruction == chunk.OP_REF_LOCAL_BORROW {
				upvalue.MarkBorrowed()
			}
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
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
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
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
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

			// Push a reference wrapping the container and property name,
			// so a later dereference or assignment can resolve this field.
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
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
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
			// O tipo estatico do campo ja e `ref T`: uma ref existente ou
			// null e encaminhado como esta (spec §2.3 regra 2, §4.2) — igual
			// a uma variavel `ref T`. Antes, null virava ref para o SLOT, o
			// que tornava `n == null` falso para um campo nulo e deixava
			// `*n = ...` gravar um T cru num slot tipado `ref T`.
			if stored.Type == value.VAL_REF || stored.Type == value.VAL_NULL {
				vm.push(stored)
				continue
			}
			// Valor referente cru num slot `ref T` (hoje alcancavel por
			// json_loads com payload compativel e por `campo = T` atraves de
			// base ref) segue embrulhado numa ref para o slot, para continuar
			// passavel adiante como antes. Shim temporario: sai quando a
			// issue #50 fechar o invariante "slot ref T contem ref ou null".
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
				var found bool
				if stored, found = mapObj.Get(key); !found {
					// Chave ausente le como null, igual a leitura plana
					// `m[k]` (o zero de value.Value e VAL_BOOL, nao null).
					stored = value.NewNull()
				}
			} else {
				return vm.runtimeError(c, ip, "contextual index reference base must be an array or map")
			}

			// Elemento/valor de tipo estatico `ref T`: ref existente ou null e
			// encaminhado como esta; valor referente cru segue embrulhado em
			// ref para o slot (ver OP_CONTEXT_REF_PROPERTY acima).
			if stored.Type == value.VAL_REF || stored.Type == value.VAL_NULL {
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

		case chunk.OP_ADD_FLOAT:
			// Espelho float de OP_ADD_INT: sem zerar, escalar nao carrega ponteiro.
			vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].AsFloat + vm.stack[vm.stackTop-1].AsFloat)
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
		case chunk.OP_SUB_FLOAT:
			// Espelho float de OP_SUB_INT.
			vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].AsFloat - vm.stack[vm.stackTop-1].AsFloat)
			vm.stackTop--
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
		case chunk.OP_MUL_FLOAT:
			// Espelho float de OP_MUL_INT.
			vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].AsFloat * vm.stack[vm.stackTop-1].AsFloat)
			vm.stackTop--
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
		case chunk.OP_DIV_FLOAT:
			// Mesma mensagem de erro do ramo float de OP_DIVIDE (generico):
			// divisor 0.0 e erro de runtime, nao +Inf/NaN.
			if vm.stack[vm.stackTop-1].AsFloat == 0 {
				return vm.runtimeError(c, ip, "division by zero")
			}
			vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].AsFloat / vm.stack[vm.stackTop-1].AsFloat)
			vm.stackTop--
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
			// RC: OP_SELECT e o segundo funil do par do buffer de canal (spec
			// §4.2): um send que dispara aqui precisa do MESMO retain que
			// chan_send faz, senao o chan_recv (ou o braco de recv de outro
			// select) do outro lado libera uma retencao que nunca existiu (dec
			// a menos). O retain e feito ANTES do reflect.Select — especulativo,
			// para todos os cases de send — porque um receptor concorrente pode
			// consumir e liberar o valor assim que o send dispara; retain
			// tardio abriria janela de contagem furada. Os sends que NAO
			// dispararem sao desfeitos logo apos o select (retain-antes-de-
			// release: a contagem nunca passa por zero).
			sendVals := make([]value.Value, count)
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
					value.Retain(val) // RC: espelha chan_send (ver comentario acima)
					sendVals[i] = val
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

			// RC: desfaz o retain especulativo dos cases de send que nao
			// dispararam. O do case escolhido (se for send) permanece: o buffer
			// do canal agora e dono duravel, e quem tirar o valor de la
			// (chan_recv ou recv de select) faz o release espelhado.
			for i := range cases {
				if i != chosenIndex && cases[i].Dir == reflect.SelectSend {
					value.Release(sendVals[i])
				}
			}

			vm.push(value.NewInt(int64(chosenIndex)))

			var valToPush value.Value
			if recvOK {
				if v, ok := recvVal.Interface().(value.Value); ok {
					// RC: espelha chan_recv — o valor saiu do buffer do canal,
					// que era dono duravel dele.
					value.Release(v)
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
			if count < 0 {
				// Sem o guard, make() panica no lado Go (makeslice: len out
				// of range) e o panic atravessa a VM — fora do alcance de
				// call_result e sem linha do script.
				return vm.runtimeError(c, ip, "zeros size must be non-negative, got %d", count)
			}
			elements := make([]value.Value, count)
			for i := 0; i < count; i++ {
				elements[i] = value.NewInt(0)
			}
			vm.push(value.NewArray(elements))
		case chunk.OP_GREATER:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.AsInt > b.AsInt))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(a.AsFloat > b.AsFloat))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(float64(a.AsInt) > b.AsFloat))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.AsFloat > float64(b.AsInt)))
			} else if strA, strB, ok := stringOperands(a, b); ok {
				// Ordenacao lexicografica byte a byte — para UTF-8 valido
				// (invariante de toda string Noxy) coincide com a ordem por
				// code point, e casa com a igualdade byte-exata da spec.
				// bytes ficam de fora: a ponte explicita e to_str.
				vm.push(value.NewBool(strA > strB))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers or strings")
			}
		case chunk.OP_GREATER_INT:
			b := vm.pop()
			a := vm.pop()
			vm.push(value.NewBool(a.AsInt > b.AsInt))
		case chunk.OP_GREATER_FLOAT:
			// Espelho float de OP_GREATER_INT.
			vm.stack[vm.stackTop-2] = value.NewBool(vm.stack[vm.stackTop-2].AsFloat > vm.stack[vm.stackTop-1].AsFloat)
			vm.stackTop--
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
			} else if strA, strB, ok := stringOperands(a, b); ok {
				// Mesma ordenacao lexicografica de OP_GREATER; `>=`/`<=`
				// compilam como NOT(OP_LESS)/NOT(OP_GREATER) e herdam isto.
				vm.push(value.NewBool(strA < strB))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers or strings")
			}
		case chunk.OP_LESS_INT:
			// Inline pop/pop/push
			vm.stack[vm.stackTop-2] = value.NewBool(vm.stack[vm.stackTop-2].AsInt < vm.stack[vm.stackTop-1].AsInt)
			vm.stack[vm.stackTop-1] = value.Value{}
			vm.stackTop--
		case chunk.OP_LESS_FLOAT:
			// Espelho float de OP_LESS_INT.
			vm.stack[vm.stackTop-2] = value.NewBool(vm.stack[vm.stackTop-2].AsFloat < vm.stack[vm.stackTop-1].AsFloat)
			vm.stackTop--
		case chunk.OP_EQUAL:
			b := vm.pop()
			a := vm.pop()
			// Em `==`/`!=` um ref NUNCA e dereferenciado implicitamente
			// (spec §2.3, excecao 1): dois refs comparam identidade de slot
			// (§2.2.7), um ref nulo E o proprio VAL_NULL (entao `r == null`
			// pergunta sobre o ref, nao sobre o valor apontado — o que
			// mantem `no.proximo != null` funcionando e torna distinguivel
			// o ref valido para um slot que contem null), e ref vs valor e
			// simplesmente diferente. O caso misto estatico e rejeitado
			// pelo compilador com hint para `*r`; aqui so chega via
			// fronteira dinamica (`any`), onde a resposta honesta e false.
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
			gcache = c.GlobalCache()
			ip = frame.IP

		case chunk.OP_CALL_STATIC:
			argCount := int(c.Code[ip])
			ip++

			frame.IP = ip // Save current instruction pointer to the frame before call

			if ok, err := vm.callValueStatic(vm.peek(argCount), argCount, c, ip); !ok {
				return err
			}
			// Update cached frame
			frame = vm.currentFrame // Switch to new frame
			c = frame.Closure.Function.Chunk.(*chunk.Chunk)
			gcache = c.GlobalCache()
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
			idx := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
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
					// RC: a caixa nasce possuidora; os
					// OP_MARK_UPVALUE_BORROW que o compilador emite logo
					// depois desta tabela marcam as de slot `ref`.
					closure.Upvalues[i] = vm.captureUpvalue(&vm.stack[frame.LocalBase+int(index)])
				} else {
					closure.Upvalues[i] = frame.Closure.Upvalues[index]
				}
			}
			vm.push(value.Value{Type: value.VAL_FUNCTION, Obj: closure})

		case chunk.OP_MARK_UPVALUE_BORROW:
			upvalueIndex := int(c.Code[ip])
			ip++
			// RC: marca estatica emitida pelo compilador logo apos o
			// OP_CLOSURE — a caixa deste upvalue foi aberta sobre um slot de
			// tipo `ref` e portanto EMPRESTA o que guarda (nao retem ao fechar,
			// nao solta ao ser sobrescrita).
			marked, ok := vm.peek(0).Obj.(*value.ObjClosure)
			if !ok || marked == nil || upvalueIndex >= len(marked.Upvalues) {
				return vm.runtimeError(c, ip, "invalid upvalue")
			}
			marked.Upvalues[upvalueIndex].MarkBorrowed()

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
			old, ok := frame.Closure.Upvalues[slot].Load()
			if !ok {
				return vm.runtimeError(c, ip, "invalid upvalue")
			}
			updated := vm.peek(0)
			// RC: retain-antes-de-release (auto-atribuicao via upvalue). Caixa
			// emprestada (slot `ref` capturado) troca sem contar posse: quem
			// responde pelo objeto e o dono real, e soltar o velho aqui seria
			// soltar o que a caixa nunca reteve (dec a menos).
			if frame.Closure.Upvalues[slot].IsBorrowed() {
				if !frame.Closure.Upvalues[slot].Store(updated) {
					return vm.runtimeError(c, ip, "invalid upvalue")
				}
			} else {
				value.Retain(updated)
				// Caixa ABERTA escreve num slot de pilha possuido: a entrada
				// (slot, objeto) do frame dono tem de passar a nomear o valor
				// novo — o velho e pago aqui pelo funil, o novo pelo fim do
				// frame (spec §4.2, mesma regra da escrita via ref).
				vm.retargetOwnedSlotForUpvalue(frame.Closure.Upvalues[slot], updated)
				if !frame.Closure.Upvalues[slot].Store(updated) {
					return vm.runtimeError(c, ip, "invalid upvalue")
				}
				value.Release(old)
			}

		case chunk.OP_CLOSE_UPVALUE:
			// RC: a propria caixa sabe se empresta (marca estatica); a posse
			// so migra para ela quando nao empresta (ver closeUpvalue).
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
			gcache = c.GlobalCache()
			ip = frame.IP

		case chunk.OP_ARRAY:
			count := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2

			elements := make([]value.Value, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
				// RC: elemento pode continuar referenciado pela origem
				value.Retain(elements[i]) // elemento e dono duravel
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
				// RC: valor pode continuar referenciado pela origem
				value.Retain(val) // elemento e dono duravel
				mapping.Set(key, val)
			}
			vm.push(mapObj)

		case chunk.OP_DUP:
			vm.push(vm.peek(0))

		case chunk.OP_IMPORT:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			nameConstant := c.Constants[index]
			moduleName := nameConstant.Obj.(string)

			frame.IP = ip
			mod, err := vm.loadModule(moduleName)
			if err != nil {
				return vm.runtimeErrorCause(c, ip, err, "failed to import module '%s'", moduleName)
			}
			frame = vm.currentFrame
			c = frame.Closure.Function.Chunk.(*chunk.Chunk)
			gcache = c.GlobalCache()
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
					// RC: retain-antes-de-release (elemento e dono duravel)
					old := arr.Elements[idx]
					value.Retain(val)
					arr.Elements[idx] = val
					value.Release(old)
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
					// RC: so libera o velho se a chave ja existia (dec a
					// menos e proibido); retain-antes-de-release quando existe.
					if old, exists := mapObj.Get(key); exists {
						value.Retain(val)
						mapObj.Set(key, val)
						value.Release(old)
					} else {
						value.Retain(val)
						mapObj.Set(key, val)
					}
					vm.push(val)
					continue
				}
			}
			return vm.runtimeError(c, ip, "cannot set index on non-array/map")

		case chunk.OP_GET_PROPERTY:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
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
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
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

			// RC: retain-antes-de-release (campo e dono duravel); Release
			// em campo inexistente (Value{} zero) e no-op (nao e VAL_OBJ)
			old := instance.Fields[name]
			value.Retain(val)
			instance.Fields[name] = val
			value.Release(old)
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

		case chunk.OP_GET_LOCAL_MUT:
			slot := c.Code[ip]
			ip++
			idx := frame.LocalBase + int(slot)
			v := vm.stack[idx]
			if value.IsShared(v) {
				old := v
				v = vm.copyValue(v)
				vm.stack[idx] = v
				// RC: usa ownSlot (mantem o slot registrado em frame.Owned)
				// em vez de Retain cru — mesmo padrao do OP_SET_LOCAL.
				frame.ownSlot(vm, idx)
				value.Release(old)
			}
			vm.push(v)

		case chunk.OP_GET_LOCAL_MUT_BORROW:
			slot := c.Code[ip]
			ip++
			// RC: gemeo de EMPRESTIMO do acima, emitido quando o tipo declarado
			// do local e `ref T`. O slot nao possui o que guarda: nao pode
			// reter o clone nem soltar o velho (soltar o que nunca se reteve e
			// dec a menos, e faria o objeto compartilhado parecer unico). O
			// clone fica no slot emprestado sem dono — a mutacao adiante vai
			// para o clone, exatamente como no comportamento pre-RC.
			idx := frame.LocalBase + int(slot)
			v := vm.stack[idx]
			if value.IsShared(v) {
				v = vm.copyValue(v)
				vm.stack[idx] = v
			}
			vm.push(v)

		case chunk.OP_GET_GLOBAL_MUT:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			name := c.Constants[index].Obj.(string)
			owner, ok := frame.Environment.ResolveOwner(name)
			if !ok {
				return vm.runtimeError(c, ip, "undefined global variable '%s'", name)
			}
			stored, ok := owner.GetLocal(name)
			if !ok {
				return vm.runtimeError(c, ip, "undefined global variable '%s'", name)
			}
			v, changed := vm.unicize(stored)
			if changed {
				// RC: o clone substitui o valor compartilhado no global;
				// retain-antes-de-release em torno da troca.
				value.Retain(v)
				value.Release(stored)
				owner.SetLocal(name, v)
			}
			vm.push(v)

		case chunk.OP_GET_UPVALUE_MUT:
			slot := c.Code[ip]
			ip++
			if int(slot) >= len(frame.Closure.Upvalues) {
				return vm.runtimeError(c, ip, "invalid upvalue")
			}
			upv := frame.Closure.Upvalues[slot]
			stored, ok := upv.Load()
			if !ok {
				return vm.runtimeError(c, ip, "invalid upvalue")
			}
			v, changed := vm.unicize(stored)
			if changed {
				// RC: o clone substitui o valor compartilhado no box do
				// upvalue; retain-antes-de-release em torno da troca. Caixa
				// emprestada (slot `ref` capturado) nao possui o que guarda:
				// soltar o velho ali seria dec a menos.
				if upv.IsBorrowed() {
					upv.Store(v)
				} else {
					value.Retain(v)
					// Mesma regra do OP_SET_UPVALUE: caixa aberta alcanca um
					// slot de pilha possuido — reaponta a entrada do frame dono
					// antes de soltar o velho (spec §4.2).
					vm.retargetOwnedSlotForUpvalue(upv, v)
					value.Release(stored)
					upv.Store(v)
				}
			}
			vm.push(v)

		case chunk.OP_GET_INDEX_MUT:
			indexVal := vm.pop()
			containerVal := vm.pop()
			if containerVal.Type == value.VAL_REF {
				uniq, err := vm.unicizeThroughRefValue(containerVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				containerVal = uniq
			}
			if containerVal.Type == value.VAL_OBJ {
				if arr, ok := containerVal.Obj.(*value.ObjArray); ok {
					if indexVal.Type != value.VAL_INT {
						return vm.runtimeError(c, ip, "array index must be integer")
					}
					idx := int(indexVal.AsInt)
					if idx < 0 || idx >= len(arr.Elements) {
						return vm.runtimeError(c, ip, "array index out of bounds")
					}
					v := arr.Elements[idx]
					if value.IsShared(v) {
						old := v
						v = vm.copyValue(v)
						// RC: retain-antes-de-release em torno da troca
						value.Retain(v)
						arr.Elements[idx] = v
						value.Release(old)
					}
					vm.push(v)
					continue
				}
				if mapObj, ok := containerVal.Obj.(*value.ObjMap); ok {
					key, err := referenceMapKey(indexVal)
					if err != nil {
						return vm.runtimeError(c, ip, "%s", err)
					}
					stored, ok := mapObj.Get(key)
					if !ok {
						return vm.runtimeError(c, ip, "map key not found in mutation path")
					}
					v, changed := vm.unicize(stored)
					if changed {
						// RC: retain-antes-de-release em torno da troca
						value.Retain(v)
						mapObj.Set(key, v)
						value.Release(stored)
					}
					vm.push(v)
					continue
				}
			}
			return vm.runtimeError(c, ip, "cannot index non-array/map in mutation path")

		case chunk.OP_GET_PROP_MUT:
			index := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			name := c.Constants[index].Obj.(string)
			instanceVal := vm.pop()
			if instanceVal.Type == value.VAL_REF {
				uniq, err := vm.unicizeThroughRefValue(instanceVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				instanceVal = uniq
			}
			if instanceVal.Type != value.VAL_OBJ {
				return vm.runtimeError(c, ip, "only instances/maps have properties")
			}
			if instance, ok := instanceVal.Obj.(*value.ObjInstance); ok {
				fieldVal, ok := instance.Fields[name]
				if !ok {
					return vm.runtimeError(c, ip, "undefined property '%s'", name)
				}
				if value.IsShared(fieldVal) {
					old := fieldVal
					fieldVal = vm.copyValue(fieldVal)
					// RC: retain-antes-de-release em torno da troca
					value.Retain(fieldVal)
					instance.Fields[name] = fieldVal
					value.Release(old)
				}
				vm.push(fieldVal)
			} else if mapObj, ok := instanceVal.Obj.(*value.ObjMap); ok {
				// Membros de módulo (e maps acessados como propriedade)
				stored, ok := mapObj.Get(name)
				if !ok {
					return vm.runtimeError(c, ip, "undefined property '%s' in module/map", name)
				}
				v, changed := vm.unicize(stored)
				if changed {
					// RC: retain-antes-de-release em torno da troca
					value.Retain(v)
					mapObj.Set(name, v)
					value.Release(stored)
				}
				vm.push(v)
			} else {
				return vm.runtimeError(c, ip, "only instances and maps have properties")
			}

		case chunk.OP_DEREF_MUT:
			refVal := vm.pop()
			if refVal.Type != value.VAL_REF {
				// Tolerância herdada do auto-deref antigo: slots com tipo
				// estático ref podem conter valores planos (checker leniente
				// pré-0.4). O valor já foi unicizado no nível anterior da
				// cadeia MUT — segue adiante como contêiner.
				vm.push(refVal)
				continue
			}
			v, err := vm.unicizeThroughRefValue(refVal)
			if err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
			vm.push(v)

		// OP_MARK_SHARED morto pos-RC (Task 8): compilador nao emite mais;
		// case removido do switch (sem default, opcode nao tratado e no-op).

		case chunk.OP_SWAP:
			// Swap top two stack elements: [a, b] -> [b, a]
			b := vm.pop()
			a := vm.pop()
			vm.push(b)
			vm.push(a)

		case chunk.OP_COPY:
			// CoW: deref para contexto de valor passa o valor adiante sem
			// copiar; unicidade e decidida por Owners (RC), nao por marcacao.
			val := vm.pop()
			vm.push(val)
		}
	}
}
