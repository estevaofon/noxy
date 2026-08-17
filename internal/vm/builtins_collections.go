package vm

import (
	"unicode/utf8"

	"noxy-vm/internal/value"
)

func (vm *VM) defineCollectionBuiltins() {
	vm.DefineNative("length", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewInt(0)
		}
		arg := args[0]
		if arg.Type == value.VAL_BYTES {
			if str, ok := arg.Obj.(string); ok {
				return value.NewInt(int64(len(str)))
			}
		}
		if arg.Type == value.VAL_OBJ {
			if str, ok := arg.Obj.(string); ok {
				return value.NewInt(int64(utf8.RuneCountInString(str)))
			}
			if arr, ok := arg.Obj.(*value.ObjArray); ok {
				return value.NewInt(int64(len(arr.Elements)))
			}
			if mp, ok := arg.Obj.(*value.ObjMap); ok {
				return value.NewInt(int64(mp.Len()))
			}
		}
		return value.NewInt(0)
	})

	vm.DefineNative("keys", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewArray(nil)
		}
		mapVal := args[0]
		if mapVal.Type == value.VAL_OBJ {
			if m, ok := mapVal.Obj.(*value.ObjMap); ok {
				values := m.Snapshot()
				keys := make([]value.Value, 0, len(values))
				for k := range values {
					if kInt, ok := k.(int64); ok {
						keys = append(keys, value.NewInt(kInt))
					} else if kStr, ok := k.(string); ok {
						keys = append(keys, value.NewString(kStr))
					}
				}
				return value.NewArray(keys)
			}
		}
		return value.NewArray(nil)
	})

	deleteSignature := value.NativeSignature{
		Arity: 2,
		Params: []value.ParamInfo{
			{IsRef: true, TypeName: "ref map"},
			{IsRef: false, TypeName: "any"},
		},
		ReturnType: "void",
	}
	vm.DefineContextualNativeWithSignature("delete", deleteSignature, func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, contextErr := nativeVM(context)
		if contextErr != nil {
			return value.NewNull(), contextErr
		}
		if len(args) != 2 {
			return value.NewNull(), nil
		}
		mapVal, err := machine.unicizeThroughRefValue(args[0])
		if err != nil {
			return value.NewNull(), nil
		}
		keyVal := args[1]
		if mapVal.Type == value.VAL_OBJ {
			if m, ok := mapVal.Obj.(*value.ObjMap); ok {
				var key interface{}
				if keyVal.Type == value.VAL_INT {
					key = keyVal.AsInt
				} else if keyVal.Type == value.VAL_OBJ {
					if str, ok := keyVal.Obj.(string); ok {
						key = str
					}
				}
				if key != nil {
					// RC: busca o valor velho antes de remover; so libera se
					// a chave de fato existia (chave ausente nao mexe em
					// Owners).
					old, existed := m.Get(key)
					m.Delete(key)
					if existed {
						value.Release(old)
					}
				}
			}
		}
		return value.NewNull(), nil
	})
	appendSignature := value.NativeSignature{
		Arity: 2,
		Params: []value.ParamInfo{
			{IsRef: true, TypeName: "ref array"},
		},
		ReturnType: "void",
	}
	vm.DefineContextualNativeWithSignature("append", appendSignature, func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, contextErr := nativeVM(context)
		if contextErr != nil {
			return value.NewNull(), contextErr
		}
		if len(args) != 2 {
			return value.NewNull(), nil
		}
		targetRef, _ := args[0].Obj.(*value.ObjRef)
		arrVal, err := machine.unicizeThroughRefValue(args[0])
		if err != nil {
			return value.NewNull(), nil
		}
		item := args[1]
		if !machine.appendItemCompatible(targetRef, item) {
			return value.NewNull(), nil
		}
		if arrVal.Type == value.VAL_OBJ {
			if arr, ok := arrVal.Obj.(*value.ObjArray); ok {
				value.Retain(item) // RC: o array e dono duravel do item anexado
				arr.Elements = append(arr.Elements, item)
			}
		}
		return value.NewNull(), nil
	})
	popSignature := value.NativeSignature{
		Arity: 1,
		Params: []value.ParamInfo{
			{IsRef: true, TypeName: "ref array"},
		},
		ReturnType: "any",
	}
	vm.DefineContextualNativeWithSignature("pop", popSignature, func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, contextErr := nativeVM(context)
		if contextErr != nil {
			return value.NewNull(), contextErr
		}
		if len(args) != 1 {
			return value.NewNull(), nil
		}
		arrVal, err := machine.unicizeThroughRefValue(args[0])
		if err != nil {
			return value.NewNull(), nil
		}
		if arrVal.Type == value.VAL_OBJ {
			if arr, ok := arrVal.Obj.(*value.ObjArray); ok {
				if len(arr.Elements) == 0 {
					return value.NewNull(), nil
				}
				val := arr.Elements[len(arr.Elements)-1]
				arr.Elements = arr.Elements[:len(arr.Elements)-1]
				value.Release(val) // RC: o array solta a posse duravel do elemento removido
				return val, nil
			}
		}
		return value.NewNull(), nil
	})
	vm.DefineNative("slice", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		}
		seq := args[0]
		start := int(args[1].AsInt)
		end := int(args[2].AsInt)

		// Clamp logic helper
		clamp := func(idx, length int) int {
			if idx < 0 {
				return 0
			}
			if idx > length {
				return length
			}
			return idx
		}

		switch seq.Type {
		case value.VAL_OBJ:
			if str, ok := seq.Obj.(string); ok {
				runes := []rune(str)
				start = clamp(start, len(runes))
				end = clamp(end, len(runes))
				if start > end {
					return value.NewString("")
				}
				return value.NewString(string(runes[start:end]))
			}
			if arr, ok := seq.Obj.(*value.ObjArray); ok {
				start = clamp(start, len(arr.Elements))
				end = clamp(end, len(arr.Elements))
				if start > end {
					return value.NewArray(nil)
				}

				newElems := make([]value.Value, end-start)
				copy(newElems, arr.Elements[start:end])
				return value.NewArray(newElems)
			}
		case value.VAL_BYTES:
			if str, ok := seq.Obj.(string); ok {
				// Bytes stored as string
				start = clamp(start, len(str))
				end = clamp(end, len(str))
				if start > end {
					return value.NewBytes("")
				}
				return value.NewBytes(str[start:end])
			}
		}
		return value.NewNull()
	})
	vm.DefineNative("contains", func(args []value.Value) value.Value {
		if len(args) != 2 {
			return value.NewBool(false)
		}
		arrVal := args[0]
		target := args[1]
		if arrVal.Type == value.VAL_OBJ {
			if arr, ok := arrVal.Obj.(*value.ObjArray); ok {
				for _, el := range arr.Elements {
					if valuesEqual(el, target) {
						return value.NewBool(true)
					}
				}
			}
		}
		return value.NewBool(false)
	})
	vm.DefineNative("has_key", func(args []value.Value) value.Value {
		if len(args) != 2 {
			return value.NewBool(false)
		}
		mapVal := args[0]
		keyVal := args[1]
		if mapVal.Type == value.VAL_OBJ {
			if mapObj, ok := mapVal.Obj.(*value.ObjMap); ok {
				var key interface{}
				if keyVal.Type == value.VAL_INT {
					key = keyVal.AsInt
				} else if keyVal.Type == value.VAL_OBJ {
					if str, ok := keyVal.Obj.(string); ok {
						key = str
					} else {
						return value.NewBool(false)
					}
				} else {
					return value.NewBool(false)
				}
				_, ok := mapObj.Get(key)
				return value.NewBool(ok)
			}
		}
		return value.NewBool(false)
	})
	vm.DefineNative("to_bytes", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewBytes("")
		}
		arg := args[0]
		switch arg.Type {
		case value.VAL_OBJ:
			if str, ok := arg.Obj.(string); ok {
				return value.NewBytes(str)
			}
			if arr, ok := arg.Obj.(*value.ObjArray); ok {
				// Array of ints -> bytes
				bs := make([]byte, len(arr.Elements))
				for i, el := range arr.Elements {
					if el.Type == value.VAL_INT {
						bs[i] = byte(el.AsInt)
					}
				}
				return value.NewBytes(string(bs))
			}
		case value.VAL_INT:
			// Single int to single byte
			return value.NewBytes(string([]byte{byte(arg.AsInt)}))
		}
		return value.NewBytes("")
	})
}
