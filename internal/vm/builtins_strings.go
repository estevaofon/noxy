package vm

import (
	"strings"

	"noxy-vm/internal/value"
)

func (vm *VM) defineStringBuiltins() {
	// Strings Module
	vm.DefineNative("strings_contains", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(strings.Contains(args[0].String(), args[1].String()))
	})
	vm.DefineNative("strings_starts_with", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(strings.HasPrefix(args[0].String(), args[1].String()))
	})
	vm.DefineNative("strings_ends_with", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(strings.HasSuffix(args[0].String(), args[1].String()))
	})
	vm.DefineNative("strings_index_of", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(-1)
		}
		return value.NewInt(int64(strings.Index(args[0].String(), args[1].String())))
	})
	vm.DefineNative("strings_count", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		return value.NewInt(int64(strings.Count(args[0].String(), args[1].String())))
	})
	vm.DefineNative("strings_to_upper", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		return value.NewString(strings.ToUpper(args[0].String()))
	})
	vm.DefineNative("strings_to_lower", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		return value.NewString(strings.ToLower(args[0].String()))
	})
	vm.DefineNative("strings_trim", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		return value.NewString(strings.TrimSpace(args[0].String()))
	})
	vm.DefineNative("strings_reverse", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		s := args[0].String()
		runes := []rune(s)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return value.NewString(string(runes))
	})
	vm.DefineNative("strings_repeat", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewString("")
		}
		return value.NewString(strings.Repeat(args[0].String(), int(args[1].AsInt)))
	})

	vm.DefineNative("strings_replace", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		return value.NewString(strings.ReplaceAll(args[0].String(), args[1].String(), args[2].String()))
	})
	vm.DefineNative("strings_replace_first", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		return value.NewString(strings.Replace(args[0].String(), args[1].String(), args[2].String(), 1))
	})
	vm.DefineNative("strings_pad_left", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		s := args[0].String()
		totalLen := int(args[1].AsInt)
		padChar := args[2].String()
		if len(s) >= totalLen {
			return value.NewString(s)
		}
		padding := totalLen - len(s)
		return value.NewString(strings.Repeat(padChar, padding) + s)
	})
	vm.DefineNative("strings_split", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		}
		s := args[0].String()
		sep := args[1].String()
		structDef, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		parts := strings.Split(s, sep)

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["count"] = value.NewInt(int64(len(parts)))

		partValues := make([]value.Value, len(parts))
		for i, p := range parts {
			partValues[i] = value.NewString(p)
		}
		inst.Fields["parts"] = value.NewArray(partValues)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.DefineNative("strings_join_count", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		arrVal := args[0]
		sep := args[1].String()
		count := int(args[2].AsInt)

		if arrVal.Type == value.VAL_OBJ {
			if arr, ok := arrVal.Obj.(*value.ObjArray); ok {
				var parts []string
				max := len(arr.Elements)
				if count < max {
					max = count
				}
				for i := 0; i < max; i++ {
					parts = append(parts, arr.Elements[i].String())
				}
				return value.NewString(strings.Join(parts, sep))
			}
		}
		return value.NewString("")
	})
	vm.DefineNative("ord", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewInt(0)
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewInt(0)
		}
		return value.NewInt(int64(s[0]))
	})
	vm.DefineNative("strings_contains", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		s := args[0].String()
		substr := args[1].String()
		return value.NewBool(strings.Contains(s, substr))
	})
	vm.DefineNative("strings_replace", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		s := args[0].String()
		old := args[1].String()
		new := args[2].String()
		return value.NewString(strings.ReplaceAll(s, old, new))
	})
	vm.DefineNative("strings_substring", func(args []value.Value) value.Value {
		// args: string, start, length
		if len(args) < 3 {
			return value.NewString("")
		}
		s := args[0].String()
		runes := []rune(s)
		start := int(args[1].AsInt)
		length := int(args[2].AsInt)

		if start < 0 {
			start = 0
		}
		if start >= len(runes) {
			return value.NewString("")
		}

		end := start + length
		if end > len(runes) {
			end = len(runes)
		}

		return value.NewString(string(runes[start:end]))
	})
	vm.DefineNative("strings_is_empty", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(true)
		}
		return value.NewBool(len(args[0].String()) == 0)
	})
	vm.DefineNative("strings_is_digit", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewBool(false)
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return value.NewBool(false)
			}
		}
		return value.NewBool(true)
	})
	vm.DefineNative("strings_is_alpha", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewBool(false)
		}
		for _, r := range s {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
				return value.NewBool(false)
			}
		}
		return value.NewBool(true)
	})
	vm.DefineNative("strings_is_alnum", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewBool(false)
		}
		for _, r := range s {
			isDigit := r >= '0' && r <= '9'
			isAlpha := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			if !isDigit && !isAlpha {
				return value.NewBool(false)
			}
		}
		return value.NewBool(true)
	})
	vm.DefineNative("strings_is_space", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewBool(false)
		}
		for _, r := range s {
			if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
				return value.NewBool(false)
			}
		}
		return value.NewBool(true)
	})
	vm.DefineNative("strings_char_at", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewString("")
		}
		s := args[0].String()
		runes := []rune(s)
		idx := int(args[1].AsInt)
		if idx < 0 || idx >= len(runes) {
			return value.NewString("")
		}
		return value.NewString(string(runes[idx]))
	})
	vm.DefineNative("strings_from_char_code", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		return value.NewString(string(rune(args[0].AsInt)))
	})
}
