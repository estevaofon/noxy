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
		// ANTES do recover: no re-panico (qualquer panic que nao seja o
		// sentinela) a funcao nao volta mais aqui, e o IP salvo e o que a
		// pilha Noxy de quem tratar o panico vai mostrar. Tambem e o IP que
		// runtimeErrorAtCurrentFrame le logo abaixo.
		if vm.currentFrame == frame {
			frame.IP = ip
		}
		if recovered := recover(); recovered != nil {
			if _, isOverflow := recovered.(stackOverflowPanic); !isOverflow {
				panic(recovered)
			}
			// Sentinela de push(): um unico frame empilhou mais do que restava
			// ate StackMax (ensureCallCapacity cobre o caso comum, a recursao).
			err = vm.runtimeErrorAtCurrentFrame("stack overflow: operand stack exceeds %d slots", StackMax)
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
					Name:            fn.Name,
					Arity:           fn.Arity,
					UpvalueCount:    fn.UpvalueCount,
					Params:          fn.Params,
					Chunk:           fn.Chunk,
					Environment:     frame.Environment,
					RuntimeType:     fn.RuntimeType,
					ParamsUntracked: fn.ParamsUntracked, // issue #66 item 3: sem isto o fast path de OP_CALL_STATIC nunca dispara
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
					Name:            fn.Name,
					Arity:           fn.Arity,
					UpvalueCount:    fn.UpvalueCount,
					Params:          fn.Params,
					Chunk:           fn.Chunk,
					Environment:     frame.Environment,
					RuntimeType:     fn.RuntimeType,
					ParamsUntracked: fn.ParamsUntracked, // issue #66 item 3: sem isto o fast path de OP_CALL_STATIC nunca dispara
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
			if condition.Type != value.VAL_BOOL {
				return vm.runtimeError(c, ip, "condition must be bool, got %s", runtimeTypeName(condition))
			}
			if !condition.Bool() {
				ip += offset
			}

		case chunk.OP_JUMP_IF_TRUE:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			condition := vm.peek(0)
			if condition.Type != value.VAL_BOOL {
				return vm.runtimeError(c, ip, "condition must be bool, got %s", runtimeTypeName(condition))
			}
			if condition.Bool() {
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
			if vm.stack[vm.stackTop].Int() < vm.stack[vm.stackTop+1].Int() {
				ip += offset
			}

		case chunk.OP_JUMP_IF_LE_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].Int() <= vm.stack[vm.stackTop+1].Int() {
				ip += offset
			}

		case chunk.OP_JUMP_IF_GT_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].Int() > vm.stack[vm.stackTop+1].Int() {
				ip += offset
			}

		case chunk.OP_JUMP_IF_GE_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].Int() >= vm.stack[vm.stackTop+1].Int() {
				ip += offset
			}

		case chunk.OP_JUMP_IF_EQ_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].Int() == vm.stack[vm.stackTop+1].Int() {
				ip += offset
			}

		case chunk.OP_JUMP_IF_NE_INT:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			vm.stackTop -= 2
			if vm.stack[vm.stackTop].Int() != vm.stack[vm.stackTop+1].Int() {
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
			slotValue := &vm.stack[frame.LocalBase+int(slot)]
			slotValue.SetInt(slotValue.Int() + int64(delta))

		case chunk.OP_GET_LOCAL_ADD_IMM_INT:
			// perf issue #66 (item 3): GET_LOCAL + CONSTANT + ADD_INT/SUB_INT
			// num despacho so; o compilador garante local int e imediato i8.
			slot := c.Code[ip]
			imm := int8(c.Code[ip+1])
			ip += 2
			vm.push(value.NewInt(vm.stack[frame.LocalBase+int(slot)].Int() + int64(imm)))

		case chunk.OP_GET_LOCAL_2:
			// perf issue #66 (item 3): dois GET_LOCAL num despacho so.
			slotA := c.Code[ip]
			slotB := c.Code[ip+1]
			ip += 2
			vm.push(vm.stack[frame.LocalBase+int(slotA)])
			vm.push(vm.stack[frame.LocalBase+int(slotB)])

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
					addrStr = fmt.Sprintf("<prop %s of %s>", ref.Name, borrowBaseAddr(ref))
				case value.REF_INDEX:
					addrStr = fmt.Sprintf("<index %s of %s>", ref.Index.String(), borrowBaseAddr(ref))
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
			base := vm.pop()

			// issue #83: quando a base é um LUGAR (VAL_REF), ela é guardada
			// como tal e o contêiner é re-resolvido na escrita. Congelar o
			// objeto aqui é o bug: uma cópia feita depois compartilha este
			// mesmo *ObjInstance, e a escrita através do empréstimo vaza para
			// ela. A resolução abaixo é só para as checagens de criação.
			container := base
			if container.Type == value.VAL_REF {
				resolved, err := vm.resolveReferenceValue(container)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				container = resolved
			} else {
				base = value.Value{}
			}

			// Now check container type
			if container.Type != value.VAL_OBJ {
				return vm.runtimeError(c, ip, "Property reference base must be an object")
			}

			// Base que o compilador nao conhecia (any, struct de outro
			// modulo): campo declarado ref T e erro (R1).
			if instance, ok := container.Obj.(*value.ObjInstance); ok && instance != nil && instance.Struct.FieldIsRef(name) {
				return vm.runtimeError(c, ip, "slot '%s' already holds a reference\n  hint: pass it directly, without 'ref'", name)
			}

			// Push a reference wrapping the container and property name,
			// so a later dereference or assignment can resolve this field.

			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType:   value.REF_PROPERTY,
					Container: container,
					Base:      base,
					Name:      name,
				},
			})

		case chunk.OP_REF_INDEX:
			// Pop Index, then Container
			idx := vm.pop()
			base := vm.pop()

			// issue #83: base que é um LUGAR fica guardada como lugar; o
			// contêiner é re-resolvido na escrita (ver OP_REF_PROPERTY). A
			// resolução aqui serve às checagens de criação: se o array/map
			// está etiquetado com elemento/valor `ref T`, é erro (R1) — o slot
			// ja guarda uma referencia, `ref` sobre ele nao encaminha.
			container := base
			if container.Type == value.VAL_REF {
				resolved, err := vm.resolveReferenceValue(container)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				container = resolved
			} else {
				base = value.Value{}
			}
			if container.Type == value.VAL_OBJ {
				switch collection := container.Obj.(type) {
				case *value.ObjArray:
					if arrayElementIsRefSlot(collection) {
						return vm.runtimeError(c, ip, "slot %s already holds a reference\n  hint: pass it directly, without 'ref'", describeRefSlotIndex(idx, false))
					}
				case *value.ObjMap:
					if mapValueIsRefSlot(collection) {
						return vm.runtimeError(c, ip, "slot %s already holds a reference\n  hint: pass it directly, without 'ref'", describeRefSlotIndex(idx, true))
					}
				}
			}

			vm.push(value.Value{
				Type: value.VAL_REF,
				Obj: &value.ObjRef{
					RefType:   value.REF_INDEX,
					Container: container,
					Base:      base,
					Index:     idx,
				},
			})

		// nao emitido pelo compilador desde 0.19 (issue #82); mantido para bytecode/testes de bytecode malformado
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
			// Invariante do slot ref (spec 2026-08-20-ref-slot-invariant):
			// ref ou null e encaminhado como esta (spec §2.3 regra 2, §4.2) —
			// igual a uma variavel `ref T`; valor cru e estado impossivel e
			// erro explicito (o shim da #51 que o embrulhava saiu na #50).
			forwarded, err := forwardRefSlot(stored, "'"+name+"'")
			if err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
			vm.push(forwarded)

		// nao emitido pelo compilador desde 0.19 (issue #82); mantido para bytecode/testes de bytecode malformado
		case chunk.OP_CONTEXT_REF_INDEX:
			idx := vm.pop()
			container := vm.pop()
			if container.Type != value.VAL_OBJ {
				return vm.runtimeError(c, ip, "contextual index reference base must be an array or map")
			}

			var stored value.Value
			slotIsMap := false
			if array, ok := container.Obj.(*value.ObjArray); ok && array != nil {
				if idx.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				arrayIndex := int(idx.Int())
				if arrayIndex < 0 || arrayIndex >= len(array.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				stored = array.Elements[arrayIndex]
			} else if mapObj, ok := container.Obj.(*value.ObjMap); ok && mapObj != nil {
				slotIsMap = true
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

			// Mesmo invariante do OP_CONTEXT_REF_PROPERTY: ref/null
			// encaminha, valor cru e erro explicito.
			forwarded, err := forwardRefSlot(stored, describeRefSlotIndex(idx, slotIsMap))
			if err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
			vm.push(forwarded)

		case chunk.OP_MARK_REF_JSON_DYNAMIC:
			refValue := vm.peek(0)
			// Alvo `ref any` nulo encaminhado (campo/indice): nao ha ref para
			// marcar; deixa o null passar, como OP_MARK_REF_TARGET_TYPE —
			// json_loads devolve false para alvo null.
			if refValue.Type == value.VAL_NULL {
				continue
			}
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
				// I5 (revisao final #82): ler atraves de um ref nulo e erro,
				// nao null silencioso — o tipo estatico da posicao promete um
				// T. Mesma frase de resolveReferenceValue (references.go).
				return vm.runtimeError(c, ip, "cannot dereference null reference")
			} else if refVal.Type != value.VAL_REF {
				// R3 (spec 2026-08-24-explicit-ref): `*x` de nao-ref e erro
				// tambem em runtime (tipo estatico desconhecido).
				return vm.runtimeError(c, ip, "cannot dereference %s", runtimeTypeName(refVal))
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
				vm.push(value.NewInt(a.Int() + b.Int()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(a.Float() + b.Float()))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(float64(a.Int()) + b.Float()))

			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewFloat(a.Float() + float64(b.Int())))
			} else if a.Type == value.VAL_OBJ && b.Type == value.VAL_OBJ {
				// Check if both are strings
				strA, okA := a.Obj.(string)
				strB, okB := b.Obj.(string)
				if okA && okB {
					vm.push(value.NewString(strA + strB))
					continue // Added continue for cleaner flow
				}
				// bytes e VAL_BYTES (nunca VAL_OBJ): o caso bytes+bytes e o
				// ramo seguinte; aqui so sobra objeto nao-string.
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
			vm.stack[vm.stackTop-2] = value.NewInt(vm.stack[vm.stackTop-2].Int() + vm.stack[vm.stackTop-1].Int())
			vm.stack[vm.stackTop-1] = value.Value{}
			vm.stackTop--

		case chunk.OP_ADD_FLOAT:
			// Espelho float de OP_ADD_INT: sem zerar, escalar nao carrega ponteiro.
			vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].Float() + vm.stack[vm.stackTop-1].Float())
			vm.stackTop--

		case chunk.OP_SUBTRACT:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewInt(a.Int() - b.Int()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(a.Float() - b.Float()))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(float64(a.Int()) - b.Float()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewFloat(a.Float() - float64(b.Int())))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers")
			}
		case chunk.OP_SUB_INT:
			b := vm.pop()
			a := vm.pop()
			vm.push(value.NewInt(a.Int() - b.Int()))
		case chunk.OP_SUB_FLOAT:
			// Espelho float de OP_SUB_INT.
			vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].Float() - vm.stack[vm.stackTop-1].Float())
			vm.stackTop--
		case chunk.OP_MULTIPLY:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewInt(a.Int() * b.Int()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(a.Float() * b.Float()))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(float64(a.Int()) * b.Float()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewFloat(a.Float() * float64(b.Int())))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers")
			}
		case chunk.OP_MUL_INT:
			b := vm.pop()
			a := vm.pop()
			vm.push(value.NewInt(a.Int() * b.Int()))
		case chunk.OP_MUL_FLOAT:
			// Espelho float de OP_MUL_INT.
			vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].Float() * vm.stack[vm.stackTop-1].Float())
			vm.stackTop--
		case chunk.OP_DIVIDE:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				if b.Int() == 0 {
					return vm.runtimeError(c, ip, "division by zero")
				}
				vm.push(value.NewInt(a.Int() / b.Int()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				if b.Float() == 0 {
					return vm.runtimeError(c, ip, "division by zero")
				}
				vm.push(value.NewFloat(a.Float() / b.Float()))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				if b.Float() == 0 {
					return vm.runtimeError(c, ip, "division by zero")
				}
				vm.push(value.NewFloat(float64(a.Int()) / b.Float()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				if b.Int() == 0 {
					return vm.runtimeError(c, ip, "division by zero")
				}
				vm.push(value.NewFloat(a.Float() / float64(b.Int())))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers")
			}
		case chunk.OP_DIV_INT:
			b := vm.pop()
			a := vm.pop()
			if b.Int() == 0 {
				return vm.runtimeError(c, ip, "division by zero")
			}
			vm.push(value.NewInt(a.Int() / b.Int()))
		case chunk.OP_DIV_FLOAT:
			// Mesma mensagem de erro do ramo float de OP_DIVIDE (generico):
			// divisor 0.0 e erro de runtime, nao +Inf/NaN.
			if vm.stack[vm.stackTop-1].Float() == 0 {
				return vm.runtimeError(c, ip, "division by zero")
			}
			vm.stack[vm.stackTop-2] = value.NewFloat(vm.stack[vm.stackTop-2].Float() / vm.stack[vm.stackTop-1].Float())
			vm.stackTop--
		case chunk.OP_LEN:
			val := vm.pop()
			if val.Type == value.VAL_REF {
				// I7 (revisao final #82): OP_LEN e o comprimento da colecao
				// de um `for ... in`. Um ref chegando aqui so vem pela
				// fronteira dinamica (o compilador barra o `ref T` estatico);
				// antes caia no default 0 e o laco nao iterava em silencio.
				return vm.runtimeError(c, ip, "cannot iterate over a ref: a ref is never read implicitly\n  hint: use '*r'")
			}
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
				mode := vm.pop().Int()
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
				if b.Int() == 0 {
					return vm.runtimeError(c, ip, "modulo by zero")
				}
				vm.push(value.NewInt(a.Int() % b.Int()))
			} else {
				return vm.runtimeError(c, ip, "operands for %% must be integers")
			}
		case chunk.OP_MOD_INT:
			// Inline pop/pop/push
			b := vm.stack[vm.stackTop-1].Int()
			a := vm.stack[vm.stackTop-2].Int()
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
				vm.push(value.NewInt(a.Int() & b.Int()))
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
				vm.push(value.NewInt(a.Int() | b.Int()))
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
				vm.push(value.NewInt(a.Int() ^ b.Int()))
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
				vm.push(value.NewInt(^a.Int()))
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
				if b.Int() < 0 {
					return vm.runtimeError(c, ip, "negative shift count")
				}
				vm.push(value.NewInt(a.Int() << uint64(b.Int())))
			} else {
				return vm.runtimeError(c, ip, "operands for << must be integers")
			}

		case chunk.OP_SHIFT_RIGHT:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				if b.Int() < 0 {
					return vm.runtimeError(c, ip, "negative shift count")
				}
				vm.push(value.NewInt(a.Int() >> uint64(b.Int())))
			} else {
				return vm.runtimeError(c, ip, "operands for >> must be integers")
			}
		case chunk.OP_NEGATE:
			v := vm.pop()
			if v.Type == value.VAL_INT {
				vm.push(value.NewInt(-v.Int()))
			} else if v.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(-v.Float()))
			} else {
				return vm.runtimeError(c, ip, "operand must be number")
			}
		case chunk.OP_NOT:
			v := vm.pop()
			if v.Type == value.VAL_BOOL {
				vm.push(value.NewBool(!v.Bool()))
			} else {
				return vm.runtimeError(c, ip, "operand must be boolean")
			}
		case chunk.OP_ZEROS:
			countVal := vm.pop()
			if countVal.Type != value.VAL_INT {
				return vm.runtimeError(c, ip, "zeros size must be integer")
			}
			count := int(countVal.Int())
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
		case chunk.OP_ARRAY_FILL:
			countVal := vm.pop()
			fill := vm.pop()
			if countVal.Type != value.VAL_INT {
				return vm.runtimeError(c, ip, "array size must be integer")
			}
			count := int(countVal.Int())
			if count < 0 {
				return vm.runtimeError(c, ip, "array size must be non-negative, got %d", count)
			}
			elements := make([]value.Value, count)
			for i := range elements {
				elements[i] = fill
			}
			// RC: NewArray retem cada slot — um default composto fica com
			// Owners = count e a CoW clona na primeira escrita a um elemento.
			vm.push(value.NewArray(elements))
		case chunk.OP_GREATER:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.Int() > b.Int()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(a.Float() > b.Float()))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(float64(a.Int()) > b.Float()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.Float() > float64(b.Int())))
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
			vm.push(value.NewBool(a.Int() > b.Int()))
		case chunk.OP_GREATER_FLOAT:
			// Espelho float de OP_GREATER_INT.
			vm.stack[vm.stackTop-2] = value.NewBool(vm.stack[vm.stackTop-2].Float() > vm.stack[vm.stackTop-1].Float())
			vm.stackTop--
		case chunk.OP_LESS:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.Int() < b.Int()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(a.Float() < b.Float()))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				vm.push(value.NewBool(float64(a.Int()) < b.Float()))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.Float() < float64(b.Int())))
			} else if strA, strB, ok := stringOperands(a, b); ok {
				// Mesma ordenacao lexicografica de OP_GREATER; `>=`/`<=`
				// compilam como NOT(OP_LESS)/NOT(OP_GREATER) e herdam isto.
				vm.push(value.NewBool(strA < strB))
			} else {
				return vm.runtimeError(c, ip, "operands must be numbers or strings")
			}
		case chunk.OP_LESS_INT:
			// Inline pop/pop/push
			vm.stack[vm.stackTop-2] = value.NewBool(vm.stack[vm.stackTop-2].Int() < vm.stack[vm.stackTop-1].Int())
			vm.stack[vm.stackTop-1] = value.Value{}
			vm.stackTop--
		case chunk.OP_LESS_FLOAT:
			// Espelho float de OP_LESS_INT.
			vm.stack[vm.stackTop-2] = value.NewBool(vm.stack[vm.stackTop-2].Float() < vm.stack[vm.stackTop-1].Float())
			vm.stackTop--
		case chunk.OP_EQUAL:
			b := vm.pop()
			a := vm.pop()
			// Em `==`/`!=` um ref NUNCA e dereferenciado implicitamente
			// (spec 2026-08-24-explicit-ref, R7): dois refs comparam identidade de slot
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
			vm.push(value.NewBool(a.Int() == b.Int()))
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

			// Fast path (perf issue #66, item 3): callee e closure com
			// ParamsUntracked (nenhum parametro pode carregar contador RC —
			// o laco ownSlot de callPreparedClosure so faria Retain no-op) e
			// aridade certa. E callPreparedClosure escrito aqui, menos o
			// laco: a condicao de capacidade e a de ensureCallCapacity (que
			// custa 80 e nao cabe no orcamento de 20 de run()), com
			// growForCall continuando o unico dono das mensagens de overflow.
			// Qualquer outra coisa (aridade errada, native, struct, any,
			// parametro composto) segue por callValueStatic, mesmas mensagens.
			if callee := vm.stack[vm.stackTop-argCount-1]; callee.Type == value.VAL_FUNCTION {
				if closure, ok := callee.Obj.(*value.ObjClosure); ok && closure.Function.ParamsUntracked && argCount == closure.Function.Arity {
					if vm.frameCount == len(vm.frames) || len(vm.stack)-vm.stackTop < stackReserve {
						frame.IP = ip
						if err := vm.growForCall(c, ip); err != nil {
							return err
						}
					}
					frame.IP = ip
					callFrame := &vm.frames[vm.frameCount]
					callFrame.Closure = closure
					callFrame.IP = 0
					callFrame.StackBase = vm.stackTop - argCount - 1
					callFrame.LocalBase = callFrame.StackBase
					callFrame.Environment = closure.Environment
					callFrame.Deferred = callFrame.Deferred[:0]
					callFrame.Owned = callFrame.Owned[:0]
					vm.frameCount++
					vm.currentFrame = callFrame
					frame = callFrame
					c = closure.Function.Chunk.(*chunk.Chunk)
					gcache = c.GlobalCache()
					ip = 0
					continue
				}
			}

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
				Name:            fn.Name,
				Arity:           fn.Arity,
				UpvalueCount:    fn.UpvalueCount,
				Params:          fn.Params,
				Chunk:           fn.Chunk,
				Environment:     frame.Environment,
				RuntimeType:     fn.RuntimeType,
				ParamsUntracked: fn.ParamsUntracked, // issue #66 item 3: sem isto o fast path de OP_CALL_STATIC nunca dispara
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
			// Fast path (perf issue #66, item 3): frame sem defer, sem vinculo
			// RC registrado e sem upvalue aberto em lugar nenhum, retornando
			// para um frame que ainda pertence a este run() — popSimpleFrame
			// (unwind.go) faz so o teardown terminal, sem a copia dupla de
			// frameOutcome nem a segunda chamada. Nada de RC: Owned vazio =
			// nada a soltar; push nao retem. O caso terminal (frameCount-1 <
			// minFrameCount) e quem devolve terminalResult e fica no caminho
			// lento.
			if len(frame.Deferred) == 0 && len(frame.Owned) == 0 && vm.openUpvalues == nil && vm.frameCount-1 >= minFrameCount {
				result := vm.pop()
				vm.popSimpleFrame()
				vm.push(result)
				frame = vm.currentFrame
				c = frame.Closure.Function.Chunk.(*chunk.Chunk)
				gcache = c.GlobalCache()
				ip = frame.IP
				continue
			}
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
			// RC: move — os elementos ja foram retidos acima em nome do array.
			vm.push(value.NewArrayAdopting(elements))

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
					key = keyVal.Int()
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
			if err := vm.getIndexGeneric(c, ip); err != nil {
				return err
			}

		case chunk.OP_SET_INDEX:
			if err := vm.setIndexGeneric(c, ip); err != nil {
				return err
			}

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

			// Auto-dereference if instance is a ref.
			//
			// issue #83: aqui a referência é ALVO DE ESCRITA, então a
			// resolução tem de ser em modo de escrita — unicizar o caminho e
			// gravar os clones de volta. Com `resolveReferenceValue` (modo de
			// leitura) a mutação ia direto no objeto compartilhado e vazava
			// numa cópia posterior, que é o bug do #83 chegando por um site de
			// escrita tipado `any`: `func setx(p: any) -> void  p.x = 99 end`
			// chamada com `setx(ref arr[0])`. É o caminho dinâmico; via base
			// tipada o compilador emite a família *_MUT, que já unicizava.
			if instanceVal.Type == value.VAL_REF {
				resolved, err := vm.unicizeThroughRefValue(instanceVal)
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

			// Struct e nominal, de campos fixos (spec §5): escrever num nome
			// fora da declaracao e o mesmo "undefined property" da leitura
			// (issue #61 item 2 — antes a escrita criava o campo em silencio).
			// Via base tipada o compilador ja rejeitou (`unknown field`); aqui
			// so dispara em fronteira dinamica (`any`). O caminho quente e o
			// lookup em instance.Fields que a troca abaixo ja fazia; a
			// declaracao so e consultada quando o nome nao esta na instancia
			// (native que deixou o campo por preencher, ou nome inexistente).
			old, exists := instance.Fields[name]
			if !exists && !instance.Struct.HasField(name) {
				return vm.runtimeError(c, ip, "undefined property '%s'", name)
			}

			// Guard do slot ref (spec 2026-08-20-ref-slot-invariant §6.3):
			// via base tipada o compilador ja rejeitou; aqui so dispara em
			// fronteira dinamica (`any`). Lookup em mapa nil e gratuito.
			if instance.Struct.FieldIsRef(name) && val.Type != value.VAL_REF && val.Type != value.VAL_NULL {
				return vm.runtimeError(c, ip, "%s", refSlotWriteError(structRefFieldTypeName(instance.Struct, name), val))
			}

			// RC: retain-antes-de-release (campo e dono duravel); Release
			// em campo inexistente (Value{} zero) e no-op (nao e VAL_OBJ)
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

			// Expect Instance (Can be Ref to Instance). Mesmo motivo do
			// OP_SET_PROPERTY: alvo de escrita resolve em modo de escrita
			// (issue #83).
			if instanceVal.Type == value.VAL_REF {
				resolved, err := vm.unicizeThroughRefValue(instanceVal)
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
			vm.push(vm.unicizeOwnedSlot(frame, frame.LocalBase+int(slot)))

		case chunk.OP_GET_LOCAL_MUT_BORROW:
			slot := c.Code[ip]
			ip++
			// RC: gemeo de EMPRESTIMO do acima, emitido quando o tipo declarado
			// do local e `ref T` — ver unicizeBorrowedSlot (cow.go).
			vm.push(vm.unicizeBorrowedSlot(frame.LocalBase + int(slot)))

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
					idx := int(indexVal.Int())
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
			if refVal.Type == value.VAL_NULL {
				return vm.runtimeError(c, ip, "cannot write through a null reference")
			}
			if refVal.Type != value.VAL_REF {
				// R3: slot de tipo estatico ref T guarda ref ou null
				// (invariante do slot, spec 2026-08-20) — outra coisa e erro.
				return vm.runtimeError(c, ip, "cannot dereference %s", runtimeTypeName(refVal))
			}
			v, err := vm.unicizeThroughRefValue(refVal)
			if err != nil {
				return vm.runtimeError(c, ip, "%s", err)
			}
			vm.push(v)

		// OP_MARK_SHARED morto pos-RC (Task 8): compilador nao emite mais;
		// case removido do switch (sem default, opcode nao tratado e no-op).

		case chunk.OP_COPY:
			// CoW: deref para contexto de valor passa o valor adiante sem
			// copiar; unicidade e decidida por Owners (RC), nao por marcacao.
			val := vm.pop()
			vm.push(val)

		// perf issue #66 (item 1): indexacao tipada de array. Caminho rapido
		// grava o resultado NO LUGAR na pilha (sem pop/push); mensagens de erro
		// identicas as do generico; container inesperado cai no generico.
		case chunk.OP_GET_INDEX_ARRAY:
			top := vm.stackTop
			if arr, ok := vm.stack[top-2].Obj.(*value.ObjArray); ok {
				indexVal := vm.stack[top-1]
				if indexVal.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				idx := int(indexVal.Int())
				if idx < 0 || idx >= len(arr.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				vm.stack[top-2] = arr.Elements[idx]
				vm.stack[top-1] = value.Value{}
				vm.stackTop = top - 1
				continue
			}
			if err := vm.getIndexGeneric(c, ip); err != nil {
				return err
			}

		case chunk.OP_GET_LOCAL_INDEX_ARRAY:
			slot := c.Code[ip]
			ip++
			if arr, ok := vm.stack[frame.LocalBase+int(slot)].Obj.(*value.ObjArray); ok {
				top := vm.stackTop
				indexVal := vm.stack[top-1]
				if indexVal.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				idx := int(indexVal.Int())
				if idx < 0 || idx >= len(arr.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				vm.stack[top-1] = arr.Elements[idx]
				continue
			}
			// Fallback: a sequencia generica GET_LOCAL + GET_INDEX.
			indexVal := vm.pop()
			vm.push(vm.stack[frame.LocalBase+int(slot)])
			vm.push(indexVal)
			if err := vm.getIndexGeneric(c, ip); err != nil {
				return err
			}

		case chunk.OP_GET_REF_LOCAL_INDEX_ARRAY:
			// O slot guarda o ref de um parametro `ref T[]`. REF_UPVALUE (a
			// forma que OP_REF_LOCAL cria para todo ref a local) resolve com
			// uma Load() da caixa em vez de referenceStorage (defer + closure
			// do setter + reflect). arr != nil espelha o validateReferencedValue
			// do caminho generico (typed nil cairia no erro de la).
			slot := c.Code[ip]
			ip++
			refVal := vm.stack[frame.LocalBase+int(slot)]
			if refVal.Type == value.VAL_NULL {
				// I5 (revisao final #82): o caminho generico erra no OP_DEREF;
				// a forma fundida tem de dar a MESMA mensagem.
				return vm.runtimeError(c, ip, "cannot dereference null reference")
			}
			if ref, ok := refVal.Obj.(*value.ObjRef); ok && ref.RefType == value.REF_UPVALUE {
				if stored, ok := ref.Upvalue.Load(); ok {
					if arr, ok := stored.Obj.(*value.ObjArray); ok && arr != nil {
						top := vm.stackTop
						indexVal := vm.stack[top-1]
						if indexVal.Type != value.VAL_INT {
							return vm.runtimeError(c, ip, "array index must be integer")
						}
						idx := int(indexVal.Int())
						if idx < 0 || idx >= len(arr.Elements) {
							return vm.runtimeError(c, ip, "array index out of bounds")
						}
						vm.stack[top-1] = arr.Elements[idx]
						continue
					}
				}
			}
			// Fallback: GET_LOCAL + OP_DEREF (nao-ref passa; ref resolve; o
			// null ja errou acima) + GET_INDEX.
			container := refVal
			if refVal.Type == value.VAL_REF {
				resolved, err := vm.resolveReferenceValue(refVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				container = resolved
			}
			indexVal := vm.pop()
			vm.push(container)
			vm.push(indexVal)
			if err := vm.getIndexGeneric(c, ip); err != nil {
				return err
			}

		case chunk.OP_SET_INDEX_ARRAY_NORC:
			// [arr, i, v] -> []. Pula Retain/Release SO se valor novo e velho
			// sao comprovadamente sem contador e o array nao e (ref T)[] —
			// senao o generico (setIndexGeneric) decide, como OP_SET_INDEX +
			// OP_POP fariam.
			top := vm.stackTop
			if arr, ok := vm.stack[top-3].Obj.(*value.ObjArray); ok {
				indexVal := vm.stack[top-2]
				if indexVal.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				idx := int(indexVal.Int())
				if idx < 0 || idx >= len(arr.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				val := vm.stack[top-1]
				if value.NeverTracked(val) && value.NeverTracked(arr.Elements[idx]) && !arrayTagIsRefSlot(arr.RuntimeType.Load()) {
					arr.Elements[idx] = val
					vm.stack[top-1] = value.Value{}
					vm.stack[top-2] = value.Value{}
					vm.stack[top-3] = value.Value{}
					vm.stackTop = top - 3
					continue
				}
			}
			if err := vm.setIndexGeneric(c, ip); err != nil {
				return err
			}
			vm.LastPopped = vm.pop()

		case chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC:
			// [i, v] -> []; container no slot do local possuidor. Caminho
			// rapido = array unico (Owners <= 1 e o teste de IsShared, sem a
			// chamada) + elemento sem contador; senao a sequencia generica
			// GET_LOCAL_MUT + SET_INDEX + POP (unicizeOwnedSlot clona e
			// registra posse exatamente como o case generico).
			slot := c.Code[ip]
			ip++
			localIdx := frame.LocalBase + int(slot)
			top := vm.stackTop
			if arr, ok := vm.stack[localIdx].Obj.(*value.ObjArray); ok && arr.Owners.Load() <= 1 {
				indexVal := vm.stack[top-2]
				if indexVal.Type != value.VAL_INT {
					return vm.runtimeError(c, ip, "array index must be integer")
				}
				idx := int(indexVal.Int())
				if idx < 0 || idx >= len(arr.Elements) {
					return vm.runtimeError(c, ip, "array index out of bounds")
				}
				val := vm.stack[top-1]
				if value.NeverTracked(val) && value.NeverTracked(arr.Elements[idx]) && !arrayTagIsRefSlot(arr.RuntimeType.Load()) {
					arr.Elements[idx] = val
					vm.stack[top-1] = value.Value{}
					vm.stack[top-2] = value.Value{}
					vm.stackTop = top - 2
					continue
				}
			}
			val := vm.pop()
			indexVal := vm.pop()
			vm.push(vm.unicizeOwnedSlot(frame, localIdx))
			vm.push(indexVal)
			vm.push(val)
			if err := vm.setIndexGeneric(c, ip); err != nil {
				return err
			}
			vm.LastPopped = vm.pop()

		case chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC:
			// [i, v] -> []; o slot guarda o ref de um parametro `ref T[]`
			// (slot de emprestimo). Caminho rapido = REF_UPVALUE cujo array e
			// unico + elemento sem contador; senao GET_LOCAL_MUT_BORROW +
			// DEREF_MUT (unicizeThroughRefValue clona e grava de volta pelo
			// setter do ref) + SET_INDEX + POP.
			slot := c.Code[ip]
			ip++
			localIdx := frame.LocalBase + int(slot)
			refVal := vm.stack[localIdx]
			if refVal.Type == value.VAL_NULL {
				// I5: espelha o OP_DEREF_MUT do caminho generico
				// (GET_LOCAL_MUT_BORROW + DEREF_MUT), que recusa escrever
				// atraves de um ref nulo.
				return vm.runtimeError(c, ip, "cannot write through a null reference")
			}
			top := vm.stackTop
			if ref, ok := refVal.Obj.(*value.ObjRef); ok && ref.RefType == value.REF_UPVALUE {
				if stored, ok := ref.Upvalue.Load(); ok {
					if arr, ok := stored.Obj.(*value.ObjArray); ok && arr != nil && arr.Owners.Load() <= 1 {
						indexVal := vm.stack[top-2]
						if indexVal.Type != value.VAL_INT {
							return vm.runtimeError(c, ip, "array index must be integer")
						}
						idx := int(indexVal.Int())
						if idx < 0 || idx >= len(arr.Elements) {
							return vm.runtimeError(c, ip, "array index out of bounds")
						}
						val := vm.stack[top-1]
						if value.NeverTracked(val) && value.NeverTracked(arr.Elements[idx]) && !arrayTagIsRefSlot(arr.RuntimeType.Load()) {
							arr.Elements[idx] = val
							vm.stack[top-1] = value.Value{}
							vm.stack[top-2] = value.Value{}
							vm.stackTop = top - 2
							continue
						}
					}
				}
			}
			val := vm.pop()
			indexVal := vm.pop()
			var container value.Value
			if refVal.Type == value.VAL_REF {
				uniq, err := vm.unicizeThroughRefValue(refVal)
				if err != nil {
					return vm.runtimeError(c, ip, "%s", err)
				}
				container = uniq
			} else {
				container = vm.unicizeBorrowedSlot(localIdx)
			}
			vm.push(container)
			vm.push(indexVal)
			vm.push(val)
			if err := vm.setIndexGeneric(c, ip); err != nil {
				return err
			}
			vm.LastPopped = vm.pop()
		}
	}
}
