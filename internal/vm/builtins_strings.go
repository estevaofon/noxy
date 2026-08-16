package vm

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"noxy-vm/internal/value"
)

// requireTextArgument rejects a bytes value where text is expected. Value.String()
// renders bytes as its display form b"...", so accepting it would make the
// function operate on text the caller never wrote.
func requireTextArgument(function string, args []value.Value, index int) error {
	if index >= len(args) {
		return nil
	}
	if args[index].Type == value.VAL_BYTES {
		return fmt.Errorf("%s: expected string, got bytes; use to_str(value) to convert explicitly", function)
	}
	return nil
}

func (vm *VM) defineStringBuiltins() {
	// Strings Module
	vm.DefineContextualNative("strings_contains", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 2 {
			return value.NewBool(false), nil
		}
		for _, index := range []int{0, 1} {
			if err := requireTextArgument("strings.contains", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		return value.NewBool(strings.Contains(args[0].String(), args[1].String())), nil
	})
	vm.DefineContextualNative("strings_starts_with", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 2 {
			return value.NewBool(false), nil
		}
		for _, index := range []int{0, 1} {
			if err := requireTextArgument("strings.starts_with", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		return value.NewBool(strings.HasPrefix(args[0].String(), args[1].String())), nil
	})
	vm.DefineContextualNative("strings_ends_with", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 2 {
			return value.NewBool(false), nil
		}
		for _, index := range []int{0, 1} {
			if err := requireTextArgument("strings.ends_with", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		return value.NewBool(strings.HasSuffix(args[0].String(), args[1].String())), nil
	})
	vm.DefineContextualNative("strings_index_of", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 2 {
			return value.NewInt(-1), nil
		}
		for _, index := range []int{0, 1} {
			if err := requireTextArgument("strings.index_of", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		subject := args[0].String()
		byteOffset := strings.Index(subject, args[1].String())
		if byteOffset < 0 {
			return value.NewInt(-1), nil
		}
		// Noxy indexes strings by character, so translate the byte offset
		// that strings.Index reports into a character index.
		return value.NewInt(int64(utf8.RuneCountInString(subject[:byteOffset]))), nil
	})
	vm.DefineContextualNative("strings_count", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 2 {
			return value.NewInt(0), nil
		}
		for _, index := range []int{0, 1} {
			if err := requireTextArgument("strings.count", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		return value.NewInt(int64(strings.Count(args[0].String(), args[1].String()))), nil
	})
	vm.DefineContextualNative("strings_to_upper", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewString(""), nil
		}
		if err := requireTextArgument("strings.to_upper", args, 0); err != nil {
			return value.NewNull(), err
		}
		return value.NewString(strings.ToUpper(args[0].String())), nil
	})
	vm.DefineContextualNative("strings_to_lower", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewString(""), nil
		}
		if err := requireTextArgument("strings.to_lower", args, 0); err != nil {
			return value.NewNull(), err
		}
		return value.NewString(strings.ToLower(args[0].String())), nil
	})
	vm.DefineContextualNative("strings_trim", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewString(""), nil
		}
		if err := requireTextArgument("strings.trim", args, 0); err != nil {
			return value.NewNull(), err
		}
		return value.NewString(strings.TrimSpace(args[0].String())), nil
	})
	vm.DefineContextualNative("strings_reverse", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewString(""), nil
		}
		if err := requireTextArgument("strings.reverse", args, 0); err != nil {
			return value.NewNull(), err
		}
		s := args[0].String()
		runes := []rune(s)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return value.NewString(string(runes)), nil
	})
	vm.DefineContextualNative("strings_repeat", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 2 {
			return value.NewString(""), nil
		}
		if err := requireTextArgument("strings.repeat", args, 0); err != nil {
			return value.NewNull(), err
		}
		return value.NewString(strings.Repeat(args[0].String(), int(args[1].AsInt))), nil
	})

	vm.DefineContextualNative("strings_replace", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 3 {
			return value.NewString(""), nil
		}
		for _, index := range []int{0, 1, 2} {
			if err := requireTextArgument("strings.replace", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		return value.NewString(strings.ReplaceAll(args[0].String(), args[1].String(), args[2].String())), nil
	})
	vm.DefineContextualNative("strings_replace_first", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 3 {
			return value.NewString(""), nil
		}
		for _, index := range []int{0, 1, 2} {
			if err := requireTextArgument("strings.replace_first", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		return value.NewString(strings.Replace(args[0].String(), args[1].String(), args[2].String(), 1)), nil
	})
	vm.DefineContextualNative("strings_pad_left", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 3 {
			return value.NewString(""), nil
		}
		for _, index := range []int{0, 2} {
			if err := requireTextArgument("strings.pad_left", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		s := args[0].String()
		totalLen := int(args[1].AsInt)
		padChar := args[2].String()
		if len(s) >= totalLen {
			return value.NewString(s), nil
		}
		padding := totalLen - len(s)
		return value.NewString(strings.Repeat(padChar, padding) + s), nil
	})
	vm.DefineContextualNative("strings_split", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 3 {
			return value.NewNull(), nil
		}
		for _, index := range []int{0, 1} {
			if err := requireTextArgument("strings.split", args, index); err != nil {
				return value.NewNull(), err
			}
		}
		s := args[0].String()
		sep := args[1].String()
		structDef, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}

		parts := strings.Split(s, sep)

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["count"] = value.NewInt(int64(len(parts)))

		partValues := make([]value.Value, len(parts))
		for i, p := range parts {
			partValues[i] = value.NewString(p)
		}
		inst.Fields["parts"] = value.NewArray(partValues)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}, nil
	})
	vm.DefineContextualNative("strings_join_count", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 3 {
			return value.NewString(""), nil
		}
		if err := requireTextArgument("strings.join_count", args, 1); err != nil {
			return value.NewNull(), err
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
				return value.NewString(strings.Join(parts, sep)), nil
			}
		}
		return value.NewString(""), nil
	})
	vm.DefineContextualNative("ord", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewInt(0), nil
		}
		if err := requireTextArgument("ord", args, 0); err != nil {
			return value.NewNull(), err
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewInt(0), nil
		}
		return value.NewInt(int64(s[0])), nil
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
	vm.DefineContextualNative("strings_substring", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		// args: string, start, end_idx (exclusive, rune-based)
		// Returns runes in [start, end_idx). Indices are clamped to [0, len].
		if len(args) < 3 {
			return value.NewString(""), nil
		}
		if err := requireTextArgument("strings.substring", args, 0); err != nil {
			return value.NewNull(), err
		}
		s := args[0].String()
		runes := []rune(s)
		n := len(runes)
		start := int(args[1].AsInt)
		end := int(args[2].AsInt)

		// Negative indices count from the end (Python-style)
		if start < 0 {
			start = n + start
		}
		if end < 0 {
			end = n + end
		}
		// Clamp to valid range [0, n]
		if start < 0 {
			start = 0
		}
		if end < 0 {
			end = 0
		}
		if start > n {
			start = n
		}
		if end > n {
			end = n
		}
		if start >= end {
			return value.NewString(""), nil
		}

		return value.NewString(string(runes[start:end])), nil
	})
	vm.DefineContextualNative("strings_is_empty", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewBool(true), nil
		}
		if err := requireTextArgument("strings.is_empty", args, 0); err != nil {
			return value.NewNull(), err
		}
		return value.NewBool(len(args[0].String()) == 0), nil
	})
	vm.DefineContextualNative("strings_is_digit", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewBool(false), nil
		}
		if err := requireTextArgument("strings.is_digit", args, 0); err != nil {
			return value.NewNull(), err
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewBool(false), nil
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return value.NewBool(false), nil
			}
		}
		return value.NewBool(true), nil
	})
	vm.DefineContextualNative("strings_is_alpha", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewBool(false), nil
		}
		if err := requireTextArgument("strings.is_alpha", args, 0); err != nil {
			return value.NewNull(), err
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewBool(false), nil
		}
		for _, r := range s {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
				return value.NewBool(false), nil
			}
		}
		return value.NewBool(true), nil
	})
	vm.DefineContextualNative("strings_is_alnum", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewBool(false), nil
		}
		if err := requireTextArgument("strings.is_alnum", args, 0); err != nil {
			return value.NewNull(), err
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewBool(false), nil
		}
		for _, r := range s {
			isDigit := r >= '0' && r <= '9'
			isAlpha := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			if !isDigit && !isAlpha {
				return value.NewBool(false), nil
			}
		}
		return value.NewBool(true), nil
	})
	vm.DefineContextualNative("strings_is_space", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 1 {
			return value.NewBool(false), nil
		}
		if err := requireTextArgument("strings.is_space", args, 0); err != nil {
			return value.NewNull(), err
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewBool(false), nil
		}
		for _, r := range s {
			if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
				return value.NewBool(false), nil
			}
		}
		return value.NewBool(true), nil
	})
	vm.DefineContextualNative("strings_char_at", func(_ value.NativeContext, args []value.Value) (value.Value, error) {
		if len(args) < 2 {
			return value.NewString(""), nil
		}
		if err := requireTextArgument("strings.char_at", args, 0); err != nil {
			return value.NewNull(), err
		}
		s := args[0].String()
		runes := []rune(s)
		idx := int(args[1].AsInt)
		if idx < 0 || idx >= len(runes) {
			return value.NewString(""), nil
		}
		return value.NewString(string(runes[idx])), nil
	})
	vm.DefineNative("strings_from_char_code", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		return value.NewString(string(rune(args[0].AsInt)))
	})
}
