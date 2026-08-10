package vm

import "noxy-vm/internal/value"

func (vm *VM) defineTerminalBuiltins() {
	vm.DefineNativeWithSignature("terminal_is_terminal", value.NativeSignature{
		Arity:      0,
		ReturnType: "bool",
	}, func(args []value.Value) value.Value {
		if len(args) != 0 || vm.shared.Terminal == nil {
			return value.NewBool(false)
		}
		return value.NewBool(vm.shared.Terminal.isTerminal())
	})

	terminalResultSignature := value.NativeSignature{
		Arity:      1,
		Params:     []value.ParamInfo{{TypeName: "any"}},
		ReturnType: "any",
	}
	vm.DefineNativeWithSignature("terminal_open_raw", terminalResultSignature, func(args []value.Value) value.Value {
		definition, ok := terminalStructDefinition(args)
		if !ok || vm.shared.Terminal == nil {
			return value.NewNull()
		}

		result := value.NewInstance(definition).Obj.(*value.ObjInstance)
		if err := vm.shared.Terminal.openRaw(); err != nil {
			result.Fields["ok"] = value.NewBool(false)
			result.Fields["error"] = value.NewString(err.Error())
			return value.Value{Type: value.VAL_OBJ, Obj: result}
		}
		result.Fields["ok"] = value.NewBool(true)
		result.Fields["error"] = value.NewString("")
		return value.Value{Type: value.VAL_OBJ, Obj: result}
	})

	vm.DefineNativeWithSignature("terminal_read_key", terminalResultSignature, func(args []value.Value) value.Value {
		definition, ok := terminalStructDefinition(args)
		if !ok || vm.shared.Terminal == nil {
			return value.NewNull()
		}

		result := value.NewInstance(definition).Obj.(*value.ObjInstance)
		key, err := vm.shared.Terminal.readKey()
		if err != nil {
			result.Fields["ok"] = value.NewBool(false)
			result.Fields["key"] = value.NewString("")
			result.Fields["error"] = value.NewString(err.Error())
			return value.Value{Type: value.VAL_OBJ, Obj: result}
		}
		result.Fields["ok"] = value.NewBool(true)
		result.Fields["key"] = value.NewString(key)
		result.Fields["error"] = value.NewString("")
		return value.Value{Type: value.VAL_OBJ, Obj: result}
	})

	vm.DefineNativeWithSignature("terminal_close", value.NativeSignature{
		Arity:      0,
		ReturnType: "bool",
	}, func(args []value.Value) value.Value {
		if len(args) != 0 || vm.shared.Terminal == nil {
			return value.NewBool(false)
		}
		return value.NewBool(vm.shared.Terminal.close() == nil)
	})
}

func terminalStructDefinition(args []value.Value) (*value.ObjStruct, bool) {
	if len(args) != 1 || args[0].Type != value.VAL_OBJ {
		return nil, false
	}
	definition, ok := args[0].Obj.(*value.ObjStruct)
	return definition, ok && definition != nil
}
