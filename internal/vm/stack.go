package vm

import "noxy-vm/internal/value"

func (vm *VM) readShort() uint16 {
	vm.ip += 2
	return uint16(vm.chunk.Code[vm.ip-2])<<8 | uint16(vm.chunk.Code[vm.ip-1])
}

// isFalsey returns true if the value is false or null
func isFalsey(v value.Value) bool {
	return v.Type == value.VAL_NULL || (v.Type == value.VAL_BOOL && !v.AsBool)
}

func valuesEqual(a, b value.Value) bool {
	if a.Type == b.Type {
		switch a.Type {
		case value.VAL_BOOL:
			return a.AsBool == b.AsBool
		case value.VAL_NULL:
			return true
		case value.VAL_INT:
			return a.AsInt == b.AsInt
		case value.VAL_FLOAT:
			return a.AsFloat == b.AsFloat
		case value.VAL_OBJ:
			// CoW: compostos comparam estruturalmente (identidade de ponteiro
			// ficou instável sob copy-on-write). Demais objetos (strings via
			// interface, closures, canais…) mantêm a comparação direta.
			switch ao := a.Obj.(type) {
			case *value.ObjArray:
				bo, ok := b.Obj.(*value.ObjArray)
				if !ok {
					return false
				}
				if ao == bo {
					return true
				}
				if len(ao.Elements) != len(bo.Elements) {
					return false
				}
				for i := range ao.Elements {
					if !valuesEqual(ao.Elements[i], bo.Elements[i]) {
						return false
					}
				}
				return true
			case *value.ObjMap:
				bo, ok := b.Obj.(*value.ObjMap)
				if !ok {
					return false
				}
				if ao == bo {
					return true
				}
				as, bs := ao.Snapshot(), bo.Snapshot()
				if len(as) != len(bs) {
					return false
				}
				for k, av := range as {
					bv, ok := bs[k]
					if !ok || !valuesEqual(av, bv) {
						return false
					}
				}
				return true
			case *value.ObjInstance:
				bo, ok := b.Obj.(*value.ObjInstance)
				if !ok {
					return false
				}
				if ao == bo {
					return true
				}
				if ao.Struct != bo.Struct || len(ao.Fields) != len(bo.Fields) {
					return false
				}
				for k, av := range ao.Fields {
					bv, ok := bo.Fields[k]
					if !ok || !valuesEqual(av, bv) {
						return false
					}
				}
				return true
			default:
				return a.Obj == b.Obj
			}
		case value.VAL_BYTES:
			return a.Obj.(string) == b.Obj.(string)
		case value.VAL_TASK:
			return a.Obj == b.Obj
		case value.VAL_REF:
			// Refs comparam por identidade de SLOT (não são dereferenciados —
			// o que também impede ciclos na comparação estrutural).
			ar, aok := a.Obj.(*value.ObjRef)
			br, bok := b.Obj.(*value.ObjRef)
			if !aok || !bok || ar == nil || br == nil {
				return a.Obj == b.Obj
			}
			if ar.RefType != br.RefType {
				return false
			}
			switch ar.RefType {
			case value.REF_GLOBAL:
				return ar.GlobalOwner == br.GlobalOwner && ar.Name == br.Name
			case value.REF_UPVALUE:
				return ar.Upvalue == br.Upvalue
			case value.REF_PTR:
				return ar.Ptr == br.Ptr
			case value.REF_PROPERTY:
				return ar.Container.Obj == br.Container.Obj && ar.Name == br.Name
			case value.REF_INDEX:
				return ar.Container.Obj == br.Container.Obj && valuesEqual(ar.Index, br.Index)
			}
			return false
		default:
			return false
		}
	}

	// Mixed types
	if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
		return float64(a.AsInt) == b.AsFloat
	}
	if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
		return a.AsFloat == float64(b.AsInt)
	}

	return false
}

func (vm *VM) readConstant() value.Value {
	// Assumes 1 byte operand for constant index
	index := vm.chunk.Code[vm.ip]
	vm.ip++
	return vm.chunk.Constants[index]
}

func (vm *VM) push(v value.Value) {
	if vm.stackTop >= StackMax {
		panic("Stack overflow")
	}
	vm.stack[vm.stackTop] = v
	vm.stackTop++
}

func (vm *VM) pop() value.Value {
	vm.stackTop--
	val := vm.stack[vm.stackTop]
	vm.stack[vm.stackTop] = value.Value{} // Clear reference to help GC
	return val
}

func (vm *VM) peek(distance int) value.Value {
	return vm.stack[vm.stackTop-1-distance]
}

// ownSlot retém o composto no slot e o registra (slot, objeto) para release
// no fim do frame. O release do fim do frame libera o objeto GRAVADO aqui,
// nunca o ocupante atual do slot no momento do release — reuso de slot por
// temporários nunca retidos (locais de bloco mortos, ex.: OP_POP sem drop)
// tornaria o release por índice unsound (dec a menos, proibido pela spec).
func (f *CallFrame) ownSlot(vm *VM, slot int) {
	v := vm.stack[slot]
	if !value.Retain(v) {
		return
	}
	for i := range f.Owned {
		if f.Owned[i].slot == slot {
			// Sobrescrita do slot: o site ja liberou o ocupante velho; a
			// entrada passa a apontar o objeto novo (retido acima).
			f.Owned[i].obj = v
			return
		}
	}
	f.Owned = append(f.Owned, ownedEntry{slot: slot, obj: v})
}

// ownsSlotIndex informa se o slot e um vinculo POSSUIDO deste frame. Slots de
// tipo `ref` (params IsRef, `let x: ref T`, rebind de ref) sao emprestimos e
// nunca aparecem em Owned — e por isso que a posse nao migra para o box do
// upvalue quando um deles e capturado (ver closeUpvalue).
func (f *CallFrame) ownsSlotIndex(slot int) bool {
	for i := range f.Owned {
		if f.Owned[i].slot == slot {
			return true
		}
	}
	return false
}

// captureUpvalue finds or creates an open upvalue for the given stack slot.
func (vm *VM) captureUpvalue(local *value.Value) *value.ObjUpvalue {
	// var prevUpvalue *value.ObjUpvalue // Unused for now
	upvalue := vm.openUpvalues

	// Walk list
	for upvalue != nil && !upvalue.PointsTo(local) {
		// prevUpvalue = upvalue
		upvalue = upvalue.Next()
	}

	if upvalue != nil {
		return upvalue
	}

	createdUpvalue := value.NewOpenUpvalue(local, vm.openUpvalues)
	vm.openUpvalues = createdUpvalue

	return createdUpvalue
}

// closeUpvalue fecha o box do upvalue aberto sobre o slot. frameOwnedSlot diz
// se o slot era um vinculo POSSUIDO do frame (registrado em frame.Owned): so
// nesse caso a posse migra do slot para o box. Um slot de tipo `ref` e
// EMPRESTIMO (nunca entra em frame.Owned) — reter ao fechar daria um dono a
// mais ao objeto emprestado e faria a mutacao atraves do empréstimo clonar,
// perdendo a escrita.
func (vm *VM) closeUpvalue(slot *value.Value, frameOwnedSlot bool) {
	var prev *value.ObjUpvalue
	curr := vm.openUpvalues

	for curr != nil {
		if curr.Close(slot) {
			// RC: o valor migra do slot do frame (liberado por
			// finalizeCurrentFrame) para o box do upvalue, que passa a ser
			// dono duravel independente do frame. So retem aqui - nunca
			// libera (o release do slot e responsabilidade do frame).
			if frameOwnedSlot {
				value.Retain(*slot)
			}
			next := curr.Next()
			if prev == nil {
				vm.openUpvalues = next
			} else {
				prev.SetNext(next)
			}
			return
		}
		prev = curr
		curr = curr.Next()
	}
}
