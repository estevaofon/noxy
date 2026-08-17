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

// ownSlot retém o composto no slot e o registra para release no fim do
// frame. Idempotente por slot: duplicata causaria release dobrado (dec a
// menos — proibido pela spec §8.2).
func (f *CallFrame) ownSlot(vm *VM, slot int) {
	v := vm.stack[slot]
	if !value.Retain(v) {
		return
	}
	for _, existing := range f.Owned {
		if existing == slot {
			// Slot ja possuido: o retain acima cobre o ocupante novo; o
			// release do ocupante velho e responsabilidade do site que
			// sobrescreveu (Task 3+). Nao registrar de novo.
			return
		}
	}
	f.Owned = append(f.Owned, slot)
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

func (vm *VM) closeUpvalue(slot *value.Value) {
	var prev *value.ObjUpvalue
	curr := vm.openUpvalues

	for curr != nil {
		if curr.Close(slot) {
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
