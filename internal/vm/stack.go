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
			return a.Obj == b.Obj // Simple pointer/string comparison
		case value.VAL_BYTES:
			return a.Obj.(string) == b.Obj.(string)
		case value.VAL_TASK:
			return a.Obj == b.Obj
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
