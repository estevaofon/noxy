package vm

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"noxy-vm/internal/value"
)

func (vm *VM) defineCoreBuiltins() {
	// Define 'print' native
	vm.DefineNative("print", func(args []value.Value) value.Value {
		var parts []string
		for _, arg := range args {
			parts = append(parts, arg.String())
		}
		fmt.Println(strings.Join(parts, " "))
		return value.NewNull()
	})

	// Define 'iprint' native (inline print)
	vm.DefineNative("iprint", func(args []value.Value) value.Value {
		var parts []string
		for _, arg := range args {
			parts = append(parts, arg.String())
		}
		fmt.Print(strings.Join(parts, " "))
		return value.NewNull()
	})

	vm.DefineNative("to_str", func(args []value.Value) value.Value {
		if len(args) != 1 {
			// Should return error or empty?
			return value.NewString("")
		}
		if args[0].Type == value.VAL_BYTES {
			return value.NewString(args[0].Obj.(string))
		}
		return value.NewString(args[0].String())
	})
	vm.DefineNative("to_int", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewInt(0)
		}
		v := args[0]
		if v.Type == value.VAL_INT {
			return value.NewInt(v.AsInt)
		}
		if v.Type == value.VAL_FLOAT {
			return value.NewInt(int64(v.AsFloat))
		}
		if v.Type == value.VAL_OBJ {
			if s, ok := v.Obj.(string); ok {
				if i, err := strconv.ParseInt(s, 10, 64); err == nil {
					return value.NewInt(i)
				}
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					return value.NewInt(int64(f))
				}
			}
		}
		return value.NewInt(0)
	})
	vm.DefineNative("to_float", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewFloat(0.0)
		}
		v := args[0]
		if v.Type == value.VAL_FLOAT {
			return value.NewFloat(v.AsFloat)
		}
		if v.Type == value.VAL_INT {
			return value.NewFloat(float64(v.AsInt))
		}
		if v.Type == value.VAL_OBJ {
			if s, ok := v.Obj.(string); ok {
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					return value.NewFloat(f)
				}
			}
		}
		return value.NewFloat(0.0)
	})
	vm.DefineNative("hex", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		if args[0].Type == value.VAL_INT {
			return value.NewString(fmt.Sprintf("0x%x", args[0].AsInt))
		}
		if args[0].Type == value.VAL_BYTES {
			return value.NewString(fmt.Sprintf("%x", args[0].Obj.(string)))
		}
		return value.NewString(args[0].String())
	})

	vm.DefineNative("hex_encode", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewString("")
		}
		arg := args[0]
		var data string
		if arg.Type == value.VAL_BYTES {
			data = arg.Obj.(string)
		} else {
			data = arg.String()
		}
		return value.NewString(hex.EncodeToString([]byte(data)))
	})

	vm.DefineNative("hex_decode", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewBytes("")
		}
		decoded, err := hex.DecodeString(args[0].String())
		if err != nil {
			return value.NewBytes("") // Or null/error? Returning empty bytes for simplicity
		}
		return value.NewBytes(string(decoded))
	})

	vm.DefineNative("base64_encode", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewString("")
		}
		arg := args[0]
		var data string
		if arg.Type == value.VAL_BYTES {
			data = arg.Obj.(string)
		} else {
			data = arg.String()
		}
		return value.NewString(base64.StdEncoding.EncodeToString([]byte(data)))
	})

	vm.DefineNative("base64_decode", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewBytes("")
		}
		decoded, err := base64.StdEncoding.DecodeString(args[0].String())
		if err != nil {
			return value.NewBytes("")
		}
		return value.NewBytes(string(decoded))
	})

	const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	vm.DefineNative("base62_encode", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewString("")
		}
		if args[0].Type != value.VAL_INT {
			return value.NewString("")
		}
		num := args[0].AsInt
		if num == 0 {
			return value.NewString("0")
		}

		var coded []byte
		neg := false
		if num < 0 {
			neg = true
			num = -num
		}

		for num > 0 {
			rem := num % 62
			coded = append(coded, base62Chars[rem])
			num = num / 62
		}

		if neg {
			coded = append(coded, '-')
		}

		// Reverse
		for i, j := 0, len(coded)-1; i < j; i, j = i+1, j-1 {
			coded[i], coded[j] = coded[j], coded[i]
		}

		return value.NewString(string(coded))
	})

	vm.DefineNative("base62_decode", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewInt(0)
		}
		str := args[0].String()
		if str == "" {
			return value.NewInt(0)
		}
		var num int64
		var neg bool
		if strings.HasPrefix(str, "-") {
			neg = true
			str = str[1:]
		}

		for _, char := range str {
			idx := strings.IndexRune(base62Chars, char)
			if idx == -1 {
				return value.NewInt(0) // Error
			}
			num = num*62 + int64(idx)
		}
		if neg {
			num = -num
		}
		return value.NewInt(num)
	})

	// Precompile regex for fmt verbs
	// Matches % [flags] [width] [.prec] verb
	// Flags: [-+ #0]
	// Width: \d+ or *
	// Prec: \. followed by \d+ or *
	// Verb: [a-zA-Z%]
	fmtVerbRe := regexp.MustCompile(`%([-+ #0]*)(?:(\d+|\*)?)(?:\.(\d+|\*))?([a-zA-Z%])`)

	vm.DefineNative("fmt", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		formatStr := args[0].String()

		// Parse fmt string to handle %T specifically
		// We need to rebuild args and format string

		// Find all verbs
		matches := fmtVerbRe.FindAllStringSubmatchIndex(formatStr, -1)

		var newArgs []interface{}
		var newFormatBuilder strings.Builder

		argIdx := 0 // Index into args[1:]
		lastPos := 0

		argsData := args[1:]

		for _, match := range matches {
			// match indices: [start, end, f_start, f_end, w_start, w_end, p_start, p_end, v_start, v_end]
			start, end := match[0], match[1]
			// verb := formatStr[match[8]:match[9]]
			// We can get verb char easily
			verb := formatStr[match[8]] // byte

			// Append text before match
			newFormatBuilder.WriteString(formatStr[lastPos:start])

			// Determine if we need to consume args
			if verb == '%' {
				newFormatBuilder.WriteString("%%")
				lastPos = end
				continue
			}

			// Check if width uses arg
			// Groups are 1-indexed in concept, but indices array:
			// 0,1 whole
			// 2,3 flags (group 1)
			// 4,5 width (group 2)
			// 6,7 prec (group 3)
			// 8,9 verb (group 4)

			widthHasStar := false
			if match[4] != -1 {
				wStr := formatStr[match[4]:match[5]]
				if strings.Contains(wStr, "*") {
					widthHasStar = true
				}
			}

			precHasStar := false
			if match[6] != -1 {
				pStr := formatStr[match[6]:match[7]]
				if strings.Contains(pStr, "*") {
					precHasStar = true
				}
			}

			// Consume args for width/prec
			if widthHasStar {
				if argIdx < len(argsData) {
					newArgs = append(newArgs, argsData[argIdx].AsInt) // Assume int for width
					argIdx++
				}
			}
			if precHasStar {
				if argIdx < len(argsData) {
					newArgs = append(newArgs, argsData[argIdx].AsInt) // Assume int for prec
					argIdx++
				}
			}

			// Now handle the verb and the main arg
			if argIdx < len(argsData) {
				val := argsData[argIdx]
				argIdx++

				if verb == 'T' {
					// Replace %T with %s and supply type name string
					newFormatBuilder.WriteString("%s")

					typeName := "unknown"
					switch val.Type {
					case value.VAL_INT:
						typeName = "int"
					case value.VAL_FLOAT:
						typeName = "float"
					case value.VAL_BOOL:
						typeName = "bool"
					case value.VAL_NULL:
						typeName = "null"
					case value.VAL_BYTES:
						typeName = "bytes"
					case value.VAL_FUNCTION:
						typeName = "function"
					case value.VAL_NATIVE:
						typeName = "function"
					case value.VAL_OBJ:
						// Determine specific object type
						if _, ok := val.Obj.(*value.ObjArray); ok {
							typeName = "array"
						} else if _, ok := val.Obj.(*value.ObjMap); ok {
							typeName = "map"
						} else if inst, ok := val.Obj.(*value.ObjInstance); ok {
							typeName = inst.Struct.Name
						} else if _, ok := val.Obj.(*value.ObjStruct); ok {
							typeName = "struct" // Class definition
						} else if _, ok := val.Obj.(string); ok {
							typeName = "string"
						} else {
							typeName = fmt.Sprintf("%T", val.Obj)
						}
					}
					newArgs = append(newArgs, typeName)
				} else {
					// Keep original verb sequence (including flags/width/prec)
					newFormatBuilder.WriteString(formatStr[start:end])

					// Add argument, potentially wrapped or raw
					switch val.Type {
					case value.VAL_INT:
						newArgs = append(newArgs, val.AsInt)
					case value.VAL_FLOAT:
						newArgs = append(newArgs, val.AsFloat)
					case value.VAL_BOOL:
						newArgs = append(newArgs, val.AsBool)
					case value.VAL_NULL:
						newArgs = append(newArgs, nil)
					case value.VAL_OBJ:
						// Pass raw object
						newArgs = append(newArgs, val.Obj)
					case value.VAL_BYTES:
						newArgs = append(newArgs, value.BytesWrapper{Str: val.Obj.(string)})
					default:
						newArgs = append(newArgs, val.String())
					}
				}
			} else {
				// Not enough args? Just append remainder as literal?
				// Or Go fmt will print %!(MISSING)
				newFormatBuilder.WriteString(formatStr[start:end])
			}

			lastPos = end
		}

		// Append remaining format
		newFormatBuilder.WriteString(formatStr[lastPos:])

		return value.NewString(fmt.Sprintf(newFormatBuilder.String(), newArgs...))
	})
}
