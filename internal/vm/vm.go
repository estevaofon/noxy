package vm

import (
	"fmt"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const StackMax = 2048
const FramesMax = 64

type CallFrame struct {
	Function *value.ObjFunction
	IP       int
	Slots    int // Offset in stack where this frame's locals start
}

type VM struct {
	frames       [FramesMax]*CallFrame
	frameCount   int
	currentFrame *CallFrame

	chunk *chunk.Chunk // Removed, accessed via frame
	ip    int          // Removed, accessed via frame (or cached)

	// We need to keep direct ip access for performance, but sync with frame on call/return.
	// For simplicity first: Access via currentFrame? Or Cache?
	// Let's stick to: VM has ip/chunk, they are loaded from frame on Return/Call.

	stack    [StackMax]value.Value
	stackTop int

	globals map[string]value.Value
	modules map[string]value.Value // Cache for loaded modules
	Config  VMConfig
}

type VMConfig struct {
	RootPath string
}

func New() *VM {
	return NewWithConfig(VMConfig{RootPath: "."})
}

func NewWithConfig(cfg VMConfig) *VM {
	vm := &VM{
		globals: make(map[string]value.Value),
		modules: make(map[string]value.Value),
		Config:  cfg,
	}
	// Define 'print' native
	vm.defineNative("print", func(args []value.Value) value.Value {
		for _, arg := range args {
			fmt.Println(arg)
		}
		return value.NewNull()
	})
	vm.defineNative("to_str", func(args []value.Value) value.Value {
		if len(args) != 1 {
			// Should return error or empty?
			return value.NewString("")
		}
		return value.NewString(args[0].String())
	})
	vm.defineNative("time_now_ms", func(args []value.Value) value.Value {
		return value.NewInt(time.Now().UnixMilli())
	})
	vm.defineNative("time_now", func(args []value.Value) value.Value {
		return value.NewInt(time.Now().Unix())
	})
	vm.defineNative("time_now_datetime", func(args []value.Value) value.Value {
		// args[0] is DateTime struct def
		if len(args) < 1 {
			return value.NewNull()
		}
		structDef, ok := args[0].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		t := time.Now()
		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.defineNative("time_format", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewString("")
		}

		// Reconstruct time.Time from fields
		// Minimal fields: year, month, day, hour, minute, second
		y := int(inst.Fields["year"].AsInt)
		m := time.Month(inst.Fields["month"].AsInt)
		d := int(inst.Fields["day"].AsInt)
		h := int(inst.Fields["hour"].AsInt)
		min := int(inst.Fields["minute"].AsInt)
		s := int(inst.Fields["second"].AsInt)

		t := time.Date(y, m, d, h, min, s, 0, time.Local)
		return value.NewString(t.Format("2006-01-02 15:04:05"))
	})
	vm.defineNative("time_format_date", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewString("")
		}
		y := int(inst.Fields["year"].AsInt)
		m := time.Month(inst.Fields["month"].AsInt)
		d := int(inst.Fields["day"].AsInt)
		t := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
		return value.NewString(t.Format("2006-01-02"))
	})
	vm.defineNative("time_format_time", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewString("")
		}
		h := int(inst.Fields["hour"].AsInt)
		min := int(inst.Fields["minute"].AsInt)
		s := int(inst.Fields["second"].AsInt)
		t := time.Date(0, 1, 1, h, min, s, 0, time.Local)
		return value.NewString(t.Format("15:04:05"))
	})
	vm.defineNative("time_make_datetime", func(args []value.Value) value.Value {
		// args: structDef, y, m, d, h, min, s
		if len(args) < 7 {
			return value.NewNull()
		}
		structDef, ok := args[0].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		y := int(args[1].AsInt)
		m := time.Month(args[2].AsInt)
		d := int(args[3].AsInt)
		h := int(args[4].AsInt)
		min := int(args[5].AsInt)
		s := int(args[6].AsInt)

		t := time.Date(y, m, d, h, min, s, 0, time.Local)

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.defineNative("time_to_timestamp", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewInt(0)
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewInt(0)
		}

		val, ok := inst.Fields["timestamp"]
		if ok {
			return val
		}
		return value.NewInt(0)
	})
	vm.defineNative("time_from_timestamp", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		ts := args[0].AsInt
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		t := time.Unix(ts, 0)
		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.defineNative("time_diff", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		ts1 := args[0].AsInt
		ts2 := args[1].AsInt
		return value.NewInt(ts1 - ts2)
	})
	vm.defineNative("time_add_days", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		ts := args[0].AsInt
		days := args[1].AsInt
		return value.NewInt(ts + (days * 86400))
	})
	vm.defineNative("time_before", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(args[0].AsInt < args[1].AsInt)
	})
	vm.defineNative("time_after", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(args[0].AsInt > args[1].AsInt)
	})
	vm.defineNative("time_is_leap_year", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		year := args[0].AsInt
		return value.NewBool(year%4 == 0 && (year%100 != 0 || year%400 == 0))
	})
	vm.defineNative("time_days_in_month", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		year := int(args[0].AsInt)
		month := time.Month(args[1].AsInt)
		// Trick: go to next month day 0
		t := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
		return value.NewInt(int64(t.Day()))
	})
	vm.defineNative("time_weekday_name", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		wd := time.Weekday(args[0].AsInt)
		return value.NewString(wd.String())
	})
	vm.defineNative("time_month_name", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		m := time.Month(args[0].AsInt)
		return value.NewString(m.String())
	})

	vm.defineNative("time_format_custom", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewString("")
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewString("")
		}
		fmtStr := args[1].Obj.(string)

		y := int(inst.Fields["year"].AsInt)
		m := time.Month(inst.Fields["month"].AsInt)
		d := int(inst.Fields["day"].AsInt)
		h := int(inst.Fields["hour"].AsInt)
		min := int(inst.Fields["minute"].AsInt)
		s := int(inst.Fields["second"].AsInt)
		// t := time.Date(y, m, d, h, min, s, 0, time.Local) // Unused in this simple implementation

		// Simplified replacement for strftime
		// Noxy: %Y=ano, %m=mês, %d=dia, %H=hora, %M=min, %S=seg
		res := fmtStr
		res = strings.ReplaceAll(res, "%Y", fmt.Sprintf("%04d", y))
		res = strings.ReplaceAll(res, "%m", fmt.Sprintf("%02d", m))
		res = strings.ReplaceAll(res, "%d", fmt.Sprintf("%02d", d))
		res = strings.ReplaceAll(res, "%H", fmt.Sprintf("%02d", h))
		res = strings.ReplaceAll(res, "%M", fmt.Sprintf("%02d", min))
		res = strings.ReplaceAll(res, "%S", fmt.Sprintf("%02d", s))

		return value.NewString(res)
	})
	vm.defineNative("time_parse", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		str := args[0].Obj.(string)
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		t, err := time.ParseInLocation("2006-01-02 15:04:05", str, time.Local)
		if err != nil {
			return value.NewNull()
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.defineNative("time_parse_date", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		str := args[0].Obj.(string)
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		t, err := time.ParseInLocation("2006-01-02", str, time.Local)
		if err != nil {
			return value.NewNull()
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["year"] = value.NewInt(int64(t.Year()))
		inst.Fields["month"] = value.NewInt(int64(t.Month()))
		inst.Fields["day"] = value.NewInt(int64(t.Day()))
		inst.Fields["hour"] = value.NewInt(int64(t.Hour()))
		inst.Fields["minute"] = value.NewInt(int64(t.Minute()))
		inst.Fields["second"] = value.NewInt(int64(t.Second()))
		inst.Fields["weekday"] = value.NewInt(int64(t.Weekday()))
		inst.Fields["yearday"] = value.NewInt(int64(t.YearDay()))
		inst.Fields["timestamp"] = value.NewInt(t.Unix())

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.defineNative("time_add_seconds", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		ts := args[0].AsInt
		secs := args[1].AsInt
		return value.NewInt(ts + secs)
	})
	vm.defineNative("time_diff_duration", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		}
		ts1 := args[0].AsInt
		ts2 := args[1].AsInt
		structDef, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		diff := ts1 - ts2
		if diff < 0 {
			diff = -diff
		} // Duration is implicitly absolute or signed? Assuming diff(target, now)

		// If we want signed duration, logic depends on requirement. 'diff' usually return difference.
		// Let's assume signed.

		// But logic: diff_duration(natal, agora) -> time until natal.

		totalSecs := ts1 - ts2
		absSecs := totalSecs
		if absSecs < 0 {
			absSecs = -absSecs
		}

		days := absSecs / 86400
		rem := absSecs % 86400
		hours := rem / 3600
		rem = rem % 3600
		mins := rem / 60
		secs := rem % 60

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["days"] = value.NewInt(days)
		inst.Fields["hours"] = value.NewInt(hours)
		inst.Fields["minutes"] = value.NewInt(mins)
		inst.Fields["seconds"] = value.NewInt(secs)
		inst.Fields["total_seconds"] = value.NewInt(totalSecs)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.defineNative("length", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewInt(0)
		}
		arg := args[0]
		if arg.Type == value.VAL_OBJ {
			if arr, ok := arg.Obj.(*value.ObjArray); ok {
				return value.NewInt(int64(len(arr.Elements)))
			}
			if str, ok := arg.Obj.(string); ok {
				return value.NewInt(int64(len(str)))
			}
		}
		return value.NewInt(0)
	})
	vm.defineNative("append", func(args []value.Value) value.Value {
		if len(args) != 2 {
			return value.NewNull()
		}
		arrVal := args[0]
		item := args[1]
		if arrVal.Type == value.VAL_OBJ {
			if arr, ok := arrVal.Obj.(*value.ObjArray); ok {
				arr.Elements = append(arr.Elements, item)
			}
		}
		return value.NewNull()
	})
	vm.defineNative("pop", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		arrVal := args[0]
		if arrVal.Type == value.VAL_OBJ {
			if arr, ok := arrVal.Obj.(*value.ObjArray); ok {
				if len(arr.Elements) == 0 {
					return value.NewNull()
				}
				val := arr.Elements[len(arr.Elements)-1]
				arr.Elements = arr.Elements[:len(arr.Elements)-1]
				return val
			}
		}
		return value.NewNull()
	})
	vm.defineNative("contains", func(args []value.Value) value.Value {
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
	vm.defineNative("has_key", func(args []value.Value) value.Value {
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
				_, ok := mapObj.Data[key]
				return value.NewBool(ok)
			}
		}
		return value.NewBool(false)
	})
	vm.defineNative("to_bytes", func(args []value.Value) value.Value {
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
	return vm
}

func (vm *VM) defineNative(name string, fn value.NativeFunc) {
	vm.globals[name] = value.NewNative(name, fn)
}

func (vm *VM) Interpret(c *chunk.Chunk) error {
	// Wrap top-level script in a function?
	// For now, let's allow Interpret to start with a raw Chunk by creating a dummy "script" function.

	scriptFn := &value.ObjFunction{
		Name:  "script",
		Arity: 0,
		Chunk: c,
	}

	vm.stackTop = 0
	vm.push(value.NewFunction("script", 0, c)) // Push script function to stack slot 0

	// Call frame for script
	frame := &CallFrame{
		Function: scriptFn,
		IP:       0,
		Slots:    0,
	}
	vm.frames[0] = frame
	vm.frameCount = 1
	vm.currentFrame = frame

	return vm.run()
}

func (vm *VM) run() error {
	// Cache current frame values for speed
	frame := vm.currentFrame
	c := frame.Function.Chunk.(*chunk.Chunk)
	ip := frame.IP

	for {
		if ip >= len(c.Code) {
			// Implicit return if end of code?
			// Or error? Scripts usually have OP_RETURN.
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
			vm.push(constant)

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

		case chunk.OP_LOOP:
			offset := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2
			ip -= offset

		case chunk.OP_TRUE:
			vm.push(value.NewBool(true))
		case chunk.OP_FALSE:
			vm.push(value.NewBool(false))
		case chunk.OP_POP:
			vm.pop()

		case chunk.OP_GET_GLOBAL:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)
			val, ok := vm.globals[name]
			if !ok {
				return fmt.Errorf("undefined global variable '%s'", name)
			}
			vm.push(val)

		case chunk.OP_SET_GLOBAL:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)
			vm.globals[name] = vm.peek(0)

		case chunk.OP_GET_LOCAL:
			slot := c.Code[ip]
			ip++
			vm.push(vm.stack[frame.Slots+int(slot)])

		case chunk.OP_SET_LOCAL:
			slot := c.Code[ip]
			ip++
			vm.stack[frame.Slots+int(slot)] = vm.peek(0)

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
				// Check if both are BYTES?
				// VAL_BYTES has Obj as string currently.
				// But Type is VAL_BYTES.
				// Wait, if Type is VAL_BYTES, Obj is string.
				// Logic:
				if a.Type == value.VAL_BYTES && b.Type == value.VAL_BYTES {
					vm.push(value.NewBytes(a.Obj.(string) + b.Obj.(string)))
					continue
				}

				return fmt.Errorf("operands must be numbers, strings or bytes")
			} else if a.Type == value.VAL_BYTES && b.Type == value.VAL_BYTES {
				// Case where types are explicit VAL_BYTES (not VAL_OBJ)
				vm.push(value.NewBytes(a.Obj.(string) + b.Obj.(string)))
			} else {
				return fmt.Errorf("operands must be numbers or strings or bytes")
			}
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
				return fmt.Errorf("operands must be numbers")
			}
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
				return fmt.Errorf("operands must be numbers")
			}
		case chunk.OP_DIVIDE:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				if b.AsInt == 0 {
					return fmt.Errorf("division by zero")
				}
				vm.push(value.NewInt(a.AsInt / b.AsInt))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_FLOAT {
				if b.AsFloat == 0 {
					return fmt.Errorf("division by zero")
				}
				vm.push(value.NewFloat(a.AsFloat / b.AsFloat))
			} else if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
				if b.AsFloat == 0 {
					return fmt.Errorf("division by zero")
				}
				vm.push(value.NewFloat(float64(a.AsInt) / b.AsFloat))
			} else if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
				if b.AsInt == 0 {
					return fmt.Errorf("division by zero")
				}
				vm.push(value.NewFloat(a.AsFloat / float64(b.AsInt)))
			} else {
				return fmt.Errorf("operands must be numbers")
			}
		case chunk.OP_MODULO:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				if b.AsInt == 0 {
					return fmt.Errorf("modulo by zero")
				}
				vm.push(value.NewInt(a.AsInt % b.AsInt))
			} else {
				return fmt.Errorf("operands must be integers")
			}
		case chunk.OP_NEGATE:
			v := vm.pop()
			if v.Type == value.VAL_INT {
				vm.push(value.NewInt(-v.AsInt))
			} else if v.Type == value.VAL_FLOAT {
				vm.push(value.NewFloat(-v.AsFloat))
			} else {
				return fmt.Errorf("operand must be number")
			}
		case chunk.OP_NOT:
			v := vm.pop()
			vm.push(value.NewBool(isFalsey(v)))
		case chunk.OP_AND:
			b := vm.pop()
			a := vm.pop()
			// Assuming strict boolean for & operator as per usage in 'if'
			// Or should we support truthiness?
			// binary_tree.nx usage: if condition | condition. Conditions are bool.
			// Let's coerce to bool if needed or error. Safe to error for now.
			if a.Type == value.VAL_BOOL && b.Type == value.VAL_BOOL {
				vm.push(value.NewBool(a.AsBool && b.AsBool))
			} else {
				return fmt.Errorf("operands for & must be boolean")
			}
		case chunk.OP_OR:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_BOOL && b.Type == value.VAL_BOOL {
				vm.push(value.NewBool(a.AsBool || b.AsBool))
			} else {
				return fmt.Errorf("operands for | must be boolean")
			}
		case chunk.OP_ZEROS:
			countVal := vm.pop()
			if countVal.Type != value.VAL_INT {
				return fmt.Errorf("zeros size must be integer")
			}
			count := int(countVal.AsInt)
			elements := make([]value.Value, count)
			for i := 0; i < count; i++ {
				elements[i] = value.NewInt(0)
			}
			vm.push(value.NewArray(elements))
		case chunk.OP_GREATER:
			b := vm.pop()
			a := vm.pop()
			// Only supporting int/float comparison for now
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.AsInt > b.AsInt))
			} else {
				// TODO: floats
				vm.push(value.NewBool(false))
			}
		case chunk.OP_LESS:
			b := vm.pop()
			a := vm.pop()
			if a.Type == value.VAL_INT && b.Type == value.VAL_INT {
				vm.push(value.NewBool(a.AsInt < b.AsInt))
			} else {
				vm.push(value.NewBool(false))
			}
		case chunk.OP_EQUAL:
			b := vm.pop()
			a := vm.pop()
			vm.push(value.NewBool(valuesEqual(a, b)))
		case chunk.OP_PRINT:
			// v := vm.pop()
			// fmt.Println(v)
			// Replaced by native function 'print' call usually, but for now we keep OP_PRINT for debug?
			// But user wants `print()`. OP_PRINT in Noxy was statement `print x`.
			// If we support `print(x)`, it is a function call.
			// Let's keep OP_PRINT logic for now if compiler emits it for statement `print`.
			// But wait, Noxy doesn't have `print` keyword statement. It's a builtin function.
			// So OP_PRINT opcode might be deprecated or used for `print` keyword if we added one.
			// Reverting to popping and printing for debug.
			v := vm.pop()
			fmt.Println(v)

		case chunk.OP_CALL:
			argCount := int(c.Code[ip])
			ip++

			frame.IP = ip // Save current instruction pointer to the frame before call

			if !vm.callValue(vm.peek(argCount), argCount) {
				return fmt.Errorf("call failed")
			}
			// Update cached frame
			frame = vm.currentFrame // Switch to new frame
			c = frame.Function.Chunk.(*chunk.Chunk)
			ip = frame.IP

		case chunk.OP_RETURN:
			result := vm.pop()
			calleeFrame := vm.currentFrame

			vm.frameCount--
			if vm.frameCount == 0 {
				vm.pop() // Pop script function
				return nil
			}

			vm.currentFrame = vm.frames[vm.frameCount-1] // Restore caller frame
			frame = vm.currentFrame
			vm.stackTop = calleeFrame.Slots
			vm.push(result)

			c = frame.Function.Chunk.(*chunk.Chunk)
			ip = frame.IP

		case chunk.OP_ARRAY:
			count := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2

			elements := make([]value.Value, count)
			for i := count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
			}
			vm.push(value.NewArray(elements))

		case chunk.OP_MAP:
			count := int(c.Code[ip])<<8 | int(c.Code[ip+1])
			ip += 2

			// Map expects keys and values on stack: K1, V1, K2, V2...
			mapObj := value.NewMap()
			mapData := mapObj.Obj.(*value.ObjMap).Data

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
						return fmt.Errorf("map key must be int or string")
					}
				} else {
					return fmt.Errorf("map key must be int or string")
				}
				mapData[key] = val
			}
			vm.push(mapObj)

		case chunk.OP_DUP:
			vm.push(vm.peek(0))

		case chunk.OP_IMPORT:
			index := c.Code[ip]
			ip++
			nameConstant := c.Constants[index]
			moduleName := nameConstant.Obj.(string)

			// Check cache
			if mod, ok := vm.modules[moduleName]; ok {
				vm.push(mod)
			} else {
				mod, err := vm.loadModule(moduleName)
				if err != nil {
					return fmt.Errorf("failed to import module '%s': %v", moduleName, err)
				}
				vm.modules[moduleName] = mod
				vm.push(mod)
			}

		case chunk.OP_IMPORT_FROM_ALL:
			modVal := vm.pop()
			if modVal.Type == value.VAL_OBJ {
				if modMap, ok := modVal.Obj.(*value.ObjMap); ok {
					for k, v := range modMap.Data {
						if keyStr, ok := k.(string); ok {
							vm.globals[keyStr] = v
						}
					}
				} else {
					return fmt.Errorf("import * expected a module (map)")
				}
			} else {
				return fmt.Errorf("import * expected a module object")
			}

		case chunk.OP_GET_INDEX:
			indexVal := vm.pop()
			collectionVal := vm.pop()

			if collectionVal.Type == value.VAL_OBJ {
				if arr, ok := collectionVal.Obj.(*value.ObjArray); ok {
					if indexVal.Type != value.VAL_INT {
						return fmt.Errorf("array index must be integer")
					}
					idx := int(indexVal.AsInt)
					if idx < 0 || idx >= len(arr.Elements) {
						return fmt.Errorf("array index out of bounds")
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
							return fmt.Errorf("map key must be int or string")
						}
					} else {
						return fmt.Errorf("map key must be int or string")
					}

					val, ok := mapObj.Data[key]
					if !ok {
						// Return null or error? Spec says null for missing key? Or error?
						// "has_key" exists. Missing key usually runtime error or null.
						// Let's return Null for now, similar to dynamic languages.
						vm.push(value.NewNull())
					} else {
						vm.push(val)
					}
					continue
				}
			}
			// Check if it's a bytes value
			if collectionVal.Type == value.VAL_BYTES {
				str := collectionVal.Obj.(string)
				if indexVal.Type != value.VAL_INT {
					return fmt.Errorf("bytes index must be integer")
				}
				idx := int(indexVal.AsInt)
				if idx < 0 || idx >= len(str) {
					return fmt.Errorf("bytes index out of bounds")
				}
				vm.push(value.NewInt(int64(str[idx])))
				continue
			}
			return fmt.Errorf("cannot index non-array/map/bytes")

		case chunk.OP_SET_INDEX:
			val := vm.pop()
			indexVal := vm.pop()
			collectionVal := vm.pop() // The array/map itself is on stack (pointer)

			if collectionVal.Type == value.VAL_OBJ {
				if arr, ok := collectionVal.Obj.(*value.ObjArray); ok {
					if indexVal.Type != value.VAL_INT {
						return fmt.Errorf("array index must be integer")
					}
					idx := int(indexVal.AsInt)
					if idx < 0 || idx >= len(arr.Elements) {
						return fmt.Errorf("array index out of bounds")
					}
					arr.Elements[idx] = val
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
							return fmt.Errorf("map key must be int or string")
						}
					} else {
						return fmt.Errorf("map key must be int or string")
					}
					mapObj.Data[key] = val
					vm.push(val)
					continue
				}
			}
			return fmt.Errorf("cannot set index on non-array/map")

		case chunk.OP_GET_PROPERTY:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)

			instanceVal := vm.pop()
			if instanceVal.Type != value.VAL_OBJ {
				return fmt.Errorf("only instances/maps have properties")
			}

			if instance, ok := instanceVal.Obj.(*value.ObjInstance); ok {
				val, ok := instance.Fields[name]
				if !ok {
					return fmt.Errorf("undefined property '%s'", name)
				}
				vm.push(val)
			} else if mapObj, ok := instanceVal.Obj.(*value.ObjMap); ok {
				// Allow accessing map keys as properties (for modules)
				val, ok := mapObj.Data[name]
				if !ok {
					return fmt.Errorf("undefined property '%s' in module/map", name)
				}
				vm.push(val)
			} else {
				return fmt.Errorf("only instances and maps have properties")
			}

		case chunk.OP_SET_PROPERTY:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)

			val := vm.pop()
			instanceVal := vm.pop()

			if instanceVal.Type != value.VAL_OBJ {
				return fmt.Errorf("only instances have properties")
			}
			instance, ok := instanceVal.Obj.(*value.ObjInstance)
			if !ok {
				return fmt.Errorf("only instances have properties")
			}

			instance.Fields[name] = val
			vm.push(val)
		}
	}
}

func (vm *VM) callValue(callee value.Value, argCount int) bool {
	if callee.Type == value.VAL_OBJ {
		if structDef, ok := callee.Obj.(*value.ObjStruct); ok {
			// Instantiate
			if argCount != len(structDef.Fields) {
				fmt.Printf("Expected %d arguments for struct %s but got %d\n", len(structDef.Fields), structDef.Name, argCount)
				return false
			}

			instance := value.NewInstance(structDef)
			instObj := instance.Obj.(*value.ObjInstance)

			// Args are on stack.
			for i := 0; i < argCount; i++ {
				arg := vm.peek(argCount - 1 - i)
				fieldName := structDef.Fields[i]
				instObj.Fields[fieldName] = arg
			}

			// Pop args AND callee (struct def)
			vm.stackTop -= argCount + 1
			// Push instance
			vm.push(instance)
			return true
		}
	}
	if callee.Type == value.VAL_FUNCTION {
		return vm.call(callee.Obj.(*value.ObjFunction), argCount)
	}
	if callee.Type == value.VAL_NATIVE {
		native := callee.Obj.(*value.ObjNative)
		args := vm.stack[vm.stackTop-argCount : vm.stackTop]
		// fmt.Printf("Calling native %s with args: %v\n", native.Name, args)
		result := native.Fn(args)
		vm.stackTop -= argCount + 1 // args + function
		vm.push(result)
		return true
	}
	return false
}

func (vm *VM) call(fn *value.ObjFunction, argCount int) bool {
	// fmt.Printf("Calling function %s, code len: %d\n", fn.Name, len(chunk.Code))

	if argCount != fn.Arity {
		fmt.Printf("Expected %d arguments but got %d\n", fn.Arity, argCount)
		return false
	}

	if vm.frameCount == FramesMax {
		fmt.Printf("Stack overflow\n")
		return false
	}

	frame := &CallFrame{
		Function: fn,
		IP:       0,
		Slots:    vm.stackTop - argCount - 1, // Start of locals window (fn + args)
	}

	vm.frames[vm.frameCount] = frame
	vm.frameCount++
	vm.currentFrame = frame
	return true
}

func (vm *VM) readShort() uint16 {
	vm.ip += 2
	return uint16(vm.chunk.Code[vm.ip-2])<<8 | uint16(vm.chunk.Code[vm.ip-1])
}

// isFalsey returns true if the value is false or null
func isFalsey(v value.Value) bool {
	return v.Type == value.VAL_NULL || (v.Type == value.VAL_BOOL && !v.AsBool)
}

func valuesEqual(a, b value.Value) bool {
	if a.Type != b.Type {
		return false
	}
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
	default:
		return false
	}
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
	return vm.stack[vm.stackTop]
}

func (vm *VM) loadModule(name string) (value.Value, error) {
	// 1. Resolve Path
	// name is dot separated: pkg.mod
	parts := strings.Split(name, ".")
	relativePath := filepath.Join(parts...)

	// Create potential paths
	// 1. Relative to Config.RootPath (Script directory)
	// 2. Relative to CWD
	// 3. Relative to CWD/stdlib (Standard Library)
	searchPaths := []string{
		filepath.Join(vm.Config.RootPath, relativePath),
		relativePath,
		filepath.Join("stdlib", relativePath),
	}

	for _, pathStr := range searchPaths {
		// Try .nx file
		filePath := pathStr + ".nx"
		info, err := os.Stat(filePath)
		if err == nil && !info.IsDir() {
			return vm.loadModuleFromFile(filePath)
		}

		// Try directory (Folder Import)
		dirPath := pathStr
		info, err = os.Stat(dirPath)
		if err == nil && info.IsDir() {
			return vm.loadModuleFromDir(dirPath)
		}
	}

	return value.NewNull(), fmt.Errorf("module '%s' not found", name)
}

func (vm *VM) loadModuleFromDir(dirPath string) (value.Value, error) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return value.NewNull(), err
	}

	// Create a module object (Map)
	modMap := value.NewMap()
	data := modMap.Obj.(*value.ObjMap).Data

	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".nx") {
			modName := strings.TrimSuffix(f.Name(), ".nx")
			// Load this file and merge globals
			subMod, err := vm.loadModuleFromFile(filepath.Join(dirPath, f.Name()))
			if err != nil {
				return value.NewNull(), err
			}

			// Add module itself as a property (namespace)
			data[modName] = subMod

			// Merge globals
			if subMap, ok := subMod.Obj.(*value.ObjMap); ok {
				for k, v := range subMap.Data {
					data[k] = v
				}
			}
		}
	}
	return modMap, nil
}

func (vm *VM) loadModuleFromFile(path string) (value.Value, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return value.NewNull(), err
	}

	// Lexer
	l := lexer.New(string(content))
	// Parser
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return value.NewNull(), fmt.Errorf("parse error in %s: %v", path, p.Errors())
	}

	// Compiler
	c := compiler.New()
	chunk, err := c.Compile(program)
	if err != nil {
		return value.NewNull(), fmt.Errorf("compile error in %s: %v", path, err)
	}

	// Run in NEW VM
	// We want a fresh scope for the module.
	moduleVM := New()
	// Native functions are already defined in New().
	// But if we want to share state (like valid natives), New() does define them again.
	// This is fine for stateless natives.

	// Execute
	if err := moduleVM.Interpret(chunk); err != nil {
		return value.NewNull(), fmt.Errorf("runtime error in %s: %v", path, err)
	}

	// Extract globals
	modMap := value.NewMap()
	data := modMap.Obj.(*value.ObjMap).Data
	for k, v := range moduleVM.globals {
		data[k] = v
	}

	return modMap, nil
}

func (vm *VM) peek(distance int) value.Value {
	return vm.stack[vm.stackTop-1-distance]

}
