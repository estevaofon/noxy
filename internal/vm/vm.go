package vm

import (
	"database/sql"
	"fmt"
	"net"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/stdlib"
	"noxy-vm/internal/value"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const StackMax = 2048
const FramesMax = 64

type CallFrame struct {
	Function *value.ObjFunction
	IP       int
	Slots    int                    // Offset in stack where this frame's locals start
	Globals  map[string]value.Value // Globals visible to this frame
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

	globals map[string]value.Value // Global variables/functions
	modules map[string]value.Value // Cached modules (Name -> ObjMap)
	Config  VMConfig

	// IO Management
	openFiles map[int64]*os.File
	nextFD    int64

	dbHandles   map[int]*sql.DB
	stmtHandles map[int]*sql.Stmt
	stmtParams  map[int]map[int]interface{}
	nextDbID    int
	nextStmtID  int

	// Net Management
	netListeners     map[int]net.Listener
	netConns         map[int]net.Conn
	netBufferedData  map[int][]byte   // For peeked data during select
	netBufferedConns map[int]net.Conn // For peeked accepts
	nextNetID        int
}

type VMConfig struct {
	RootPath string
}

func New() *VM {
	return NewWithConfig(VMConfig{RootPath: "."})
}

func NewWithConfig(cfg VMConfig) *VM {
	vm := &VM{
		globals:          make(map[string]value.Value),
		modules:          make(map[string]value.Value),
		Config:           cfg,
		openFiles:        make(map[int64]*os.File),
		nextFD:           1,
		dbHandles:        make(map[int]*sql.DB),
		stmtHandles:      make(map[int]*sql.Stmt),
		stmtParams:       make(map[int]map[int]interface{}),
		nextDbID:         1,
		nextStmtID:       1,
		netListeners:     make(map[int]net.Listener),
		netConns:         make(map[int]net.Conn),
		netBufferedData:  make(map[int][]byte),
		netBufferedConns: make(map[int]net.Conn),
		nextNetID:        1,
	}
	// Define 'print' native
	vm.defineNative("print", func(args []value.Value) value.Value {
		var parts []string
		for _, arg := range args {
			parts = append(parts, arg.String())
		}
		fmt.Println(strings.Join(parts, " "))
		return value.NewNull()
	})
	vm.defineNative("to_str", func(args []value.Value) value.Value {
		if len(args) != 1 {
			// Should return error or empty?
			return value.NewString("")
		}
		if args[0].Type == value.VAL_BYTES {
			return value.NewString(args[0].Obj.(string))
		}
		return value.NewString(args[0].String())
	})
	vm.defineNative("to_int", func(args []value.Value) value.Value {
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
	vm.defineNative("to_float", func(args []value.Value) value.Value {
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

		names := []string{
			"Domingo", "Segunda-feira", "Terça-feira", "Quarta-feira",
			"Quinta-feira", "Sexta-feira", "Sábado",
		}
		if int(wd) >= 0 && int(wd) < len(names) {
			return value.NewString(names[wd])
		}
		return value.NewString(wd.String())
	})
	vm.defineNative("time_month_name", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		m := time.Month(args[0].AsInt)
		names := map[time.Month]string{
			time.January: "Janeiro", time.February: "Fevereiro", time.March: "Março",
			time.April: "Abril", time.May: "Maio", time.June: "Junho",
			time.July: "Julho", time.August: "Agosto", time.September: "Setembro",
			time.October: "Outubro", time.November: "Novembro", time.December: "Dezembro",
		}
		if name, ok := names[m]; ok {
			return value.NewString(name)
		}
		return value.NewString(m.String())
	})
	vm.defineNative("io_open", func(args []value.Value) value.Value {
		// args: path, mode, FileStructDef
		if len(args) < 3 {
			return value.NewNull()
		}
		path := args[0].String()
		mode := args[1].String()

		structDef, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		flag := os.O_RDONLY
		if mode == "w" {
			flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		} else if mode == "a" {
			flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
		} else if mode == "rw" || mode == "r+" {
			flag = os.O_RDWR | os.O_CREATE
		}

		f, err := os.OpenFile(path, flag, 0644)
		isOpen := true
		var fd int64 = 0

		if err != nil {
			isOpen = false
		} else {
			fd = vm.nextFD
			vm.nextFD++
			vm.openFiles[fd] = f
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["fd"] = value.NewInt(fd)
		inst.Fields["path"] = value.NewString(path)
		inst.Fields["mode"] = value.NewString(mode)
		inst.Fields["open"] = value.NewBool(isOpen)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.defineNative("io_close", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}

		fd := inst.Fields["fd"].AsInt
		if f, exists := vm.openFiles[fd]; exists {
			f.Close()
			delete(vm.openFiles, fd)
			inst.Fields["open"] = value.NewBool(false)
		}
		return value.NewNull()
	})
	vm.defineNative("io_write", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		content := args[1].String()

		fd := inst.Fields["fd"].AsInt
		if f, exists := vm.openFiles[fd]; exists {
			f.WriteString(content)
		}
		return value.NewNull()
	})
	vm.defineNative("io_read", func(args []value.Value) value.Value {
		// args: fileInst, IOResultStructDef
		if len(args) < 2 {
			return value.NewNull()
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct, ok := args[1].Obj.(*value.ObjStruct) // IOResult
		if !ok {
			return value.NewNull()
		}

		fd := inst.Fields["fd"].AsInt
		var contentStr string
		var errorStr string
		var isOk bool = false

		if f, exists := vm.openFiles[fd]; exists {
			// Read all
			stat, _ := f.Stat()
			if stat.Size() > 0 {
				buf := make([]byte, stat.Size())
				f.Seek(0, 0)
				n, err := f.Read(buf)
				if err == nil || (err != nil && n > 0) { // simple read
					contentStr = string(buf[:n])
					isOk = true
				} else {
					errorStr = err.Error()
				}
			} else {
				isOk = true // empty file
			}
		} else {
			errorStr = "File not open"
		}

		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["ok"] = value.NewBool(isOk)
		resInst.Fields["data"] = value.NewString(contentStr)
		resInst.Fields["error"] = value.NewString(errorStr)
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})
	vm.defineNative("io_exists", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		path := args[0].String()
		_, err := os.Stat(path)
		return value.NewBool(err == nil)
	})
	vm.defineNative("io_remove", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		path := args[0].String()
		err := os.Remove(path)
		return value.NewBool(err == nil)
	})
	vm.defineNative("io_read_line", func(args []value.Value) value.Value {
		// Implement line reading logic if needed, strictly speaking stdlib just wraps io_read usually but if separate native is needed:
		// For simplicity, let's just make it null/not implemented or same as read for MVP if line reading is hard on raw FD without bufio persistence
		// Or re-open? No.
		// Let's implement full read for now as read_line on whole file is weird.
		// Actually, without buffering state, readline is hard.
		// Return error 'Function not implemented yet'
		return value.NewNull()
	})
	vm.defineNative("io_stat", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		path := args[0].String()
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		info, err := os.Stat(path)
		exists := (err == nil)
		size := int64(0)
		isDir := false
		if exists {
			size = info.Size()
			isDir = info.IsDir()
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["exists"] = value.NewBool(exists)
		inst.Fields["size"] = value.NewInt(size)
		inst.Fields["is_dir"] = value.NewBool(isDir)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})
	vm.defineNative("io_mkdir", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		path := args[0].String()
		err := os.MkdirAll(path, 0755)
		return value.NewBool(err == nil)
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

	// Strings Module
	vm.defineNative("strings_contains", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(strings.Contains(args[0].String(), args[1].String()))
	})
	vm.defineNative("strings_starts_with", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(strings.HasPrefix(args[0].String(), args[1].String()))
	})
	vm.defineNative("strings_ends_with", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(strings.HasSuffix(args[0].String(), args[1].String()))
	})
	vm.defineNative("strings_index_of", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(-1)
		}
		return value.NewInt(int64(strings.Index(args[0].String(), args[1].String())))
	})
	vm.defineNative("strings_count", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewInt(0)
		}
		return value.NewInt(int64(strings.Count(args[0].String(), args[1].String())))
	})
	vm.defineNative("strings_to_upper", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		return value.NewString(strings.ToUpper(args[0].String()))
	})
	vm.defineNative("strings_to_lower", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		return value.NewString(strings.ToLower(args[0].String()))
	})
	vm.defineNative("strings_trim", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		return value.NewString(strings.TrimSpace(args[0].String()))
	})
	vm.defineNative("strings_reverse", func(args []value.Value) value.Value {
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
	vm.defineNative("strings_repeat", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewString("")
		}
		return value.NewString(strings.Repeat(args[0].String(), int(args[1].AsInt)))
	})
	vm.defineNative("strings_substring", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		s := args[0].String()
		start := int(args[1].AsInt)
		end := int(args[2].AsInt)
		if start < 0 {
			start = 0
		}
		if end > len(s) {
			end = len(s)
		}
		if start > end {
			return value.NewString("")
		}
		return value.NewString(s[start:end])
	})
	vm.defineNative("strings_replace", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		return value.NewString(strings.ReplaceAll(args[0].String(), args[1].String(), args[2].String()))
	})
	vm.defineNative("strings_replace_first", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		return value.NewString(strings.Replace(args[0].String(), args[1].String(), args[2].String(), 1))
	})
	vm.defineNative("strings_pad_left", func(args []value.Value) value.Value {
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
	vm.defineNative("strings_split", func(args []value.Value) value.Value {
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
	vm.defineNative("strings_join_count", func(args []value.Value) value.Value {
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
	vm.defineNative("ord", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewInt(0)
		}
		s := args[0].String()
		if len(s) == 0 {
			return value.NewInt(0)
		}
		return value.NewInt(int64(s[0]))
	})
	vm.defineNative("strings_contains", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		s := args[0].String()
		substr := args[1].String()
		return value.NewBool(strings.Contains(s, substr))
	})
	vm.defineNative("strings_replace", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		s := args[0].String()
		old := args[1].String()
		new := args[2].String()
		return value.NewString(strings.ReplaceAll(s, old, new))
	})
	vm.defineNative("strings_substring", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewString("")
		}
		s := args[0].String()
		start := int(args[1].AsInt)
		end := int(args[2].AsInt)

		if start < 0 {
			start = 0
		}
		if end > len(s) {
			end = len(s)
		}
		if start >= end {
			return value.NewString("")
		}

		return value.NewString(s[start:end])
	})
	vm.defineNative("strings_is_empty", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(true)
		}
		return value.NewBool(len(args[0].String()) == 0)
	})
	vm.defineNative("strings_is_digit", func(args []value.Value) value.Value {
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
	vm.defineNative("strings_is_alpha", func(args []value.Value) value.Value {
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
	vm.defineNative("strings_is_alnum", func(args []value.Value) value.Value {
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
	vm.defineNative("strings_is_space", func(args []value.Value) value.Value {
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
	vm.defineNative("strings_char_at", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewString("")
		}
		s := args[0].String()
		idx := int(args[1].AsInt)
		if idx < 0 || idx >= len(s) {
			return value.NewString("")
		}
		return value.NewString(string(s[idx]))
	})
	vm.defineNative("strings_from_char_code", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("")
		}
		return value.NewString(string(rune(args[0].AsInt)))
	})

	// Sys Module
	vm.defineNative("sys_exec", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		cmdStr := args[0].String()
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		var cmd *exec.Cmd
		if os.PathSeparator == '\\' {
			cmd = exec.Command("cmd", "/C", cmdStr)
		} else {
			cmd = exec.Command("sh", "-c", cmdStr)
		}

		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Run()
		exitCode := 0
		okVal := true

		var outputStr string = "" // No captured output for sys_exec

		if err != nil {
			okVal = false
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["exit_code"] = value.NewInt(int64(exitCode))
		inst.Fields["output"] = value.NewString(outputStr)
		inst.Fields["ok"] = value.NewBool(okVal)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.defineNative("sys_exec_output", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		cmdStr := args[0].String()
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		var cmd *exec.Cmd
		if os.PathSeparator == '\\' {
			cmd = exec.Command("cmd", "/C", cmdStr)
		} else {
			cmd = exec.Command("sh", "-c", cmdStr)
		}

		outBytes, err := cmd.CombinedOutput()
		outputStr := string(outBytes)

		exitCode := 0
		okVal := true // Logic: if exit code 0 also? or just ran? usually ok implies success.

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
			okVal = false
		} else {
			// On success err is nil
			okVal = true
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["exit_code"] = value.NewInt(int64(exitCode))
		inst.Fields["output"] = value.NewString(strings.TrimSpace(outputStr))
		inst.Fields["ok"] = value.NewBool(okVal)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.defineNative("sys_getenv", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		key := args[0].String()
		structDef, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		val, found := os.LookupEnv(key)

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["value"] = value.NewString(val)
		inst.Fields["ok"] = value.NewBool(found)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.defineNative("sys_setenv", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		key := args[0].String()
		val := args[1].String()
		err := os.Setenv(key, val)
		return value.NewBool(err == nil)
	})

	vm.defineNative("sys_getcwd", func(args []value.Value) value.Value {
		dir, err := os.Getwd()
		if err != nil {
			return value.NewString("")
		}
		return value.NewString(dir)
	})

	vm.defineNative("sys_argv", func(args []value.Value) value.Value {
		// Convert os.Args to string[]
		vals := make([]value.Value, len(os.Args))
		for i, a := range os.Args {
			vals[i] = value.NewString(a)
		}
		return value.NewArray(vals)
	})

	vm.defineNative("sys_sleep", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		ms := args[0].AsInt
		time.Sleep(time.Duration(ms) * time.Millisecond)
		return value.NewNull()
	})

	vm.defineNative("sys_exit", func(args []value.Value) value.Value {
		code := 0
		if len(args) > 0 {
			code = int(args[0].AsInt)
		}
		os.Exit(code)
		return value.NewNull()
	})

	vm.defineNative("length", func(args []value.Value) value.Value {
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
				return value.NewInt(int64(len(str)))
			}
			if arr, ok := arg.Obj.(*value.ObjArray); ok {
				return value.NewInt(int64(len(arr.Elements)))
			}
			if mp, ok := arg.Obj.(*value.ObjMap); ok {
				return value.NewInt(int64(len(mp.Data)))
			}
		}
		return value.NewInt(0)
	})

	vm.defineNative("keys", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewArray(nil)
		}
		mapVal := args[0]
		if mapVal.Type == value.VAL_OBJ {
			if m, ok := mapVal.Obj.(*value.ObjMap); ok {
				keys := make([]value.Value, 0, len(m.Data))
				for k := range m.Data {
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

	vm.defineNative("delete", func(args []value.Value) value.Value {
		if len(args) != 2 {
			return value.NewNull()
		}
		mapVal := args[0]
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
					delete(m.Data, key)
				}
			}
		}
		return value.NewNull()
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
	vm.defineNative("slice", func(args []value.Value) value.Value {
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

				// Deep copy? Or slice? Go slices reference underlying array.
				// For immutability or safety in Noxy, usually we might want copy if Noxy arrays are mutable refs.
				// But slicing usually shares backing store in languages like Go/Python? Python slices are copies.
				// Go slices share.
				// Let's create a new array with copied elements to be safe/consistent with Python-style likely expected.
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

	// Net Native Functions
	vm.defineNative("net_listen", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		host := args[0].String()
		port := int(args[1].AsInt)
		addr := fmt.Sprintf("%s:%d", host, port)

		listener, err := net.Listen("tcp", addr)
		if err != nil {
			// Return Socket with open=false
			socketFields := map[string]value.Value{
				"fd":   value.NewInt(-1),
				"addr": value.NewString(host),
				"port": value.NewInt(int64(port)),
				"open": value.NewBool(false),
			}
			return value.NewMapWithData(socketFields)
		}

		id := vm.nextNetID
		vm.nextNetID++
		vm.netListeners[id] = listener

		socketFields := map[string]value.Value{
			"fd":   value.NewInt(int64(id)),
			"addr": value.NewString(host),
			"port": value.NewInt(int64(port)),
			"open": value.NewBool(true),
		}
		return value.NewMapWithData(socketFields)
	})

	vm.defineNative("net_accept", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		sockMap, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull()
		}
		fdVal, exists := sockMap.Data["fd"]
		if !exists {
			return value.NewNull()
		}
		fd := int(fdVal.AsInt)

		listener, ok := vm.netListeners[fd]
		if !ok {
			socketFields := map[string]value.Value{
				"fd":   value.NewInt(-1),
				"addr": value.NewString(""),
				"port": value.NewInt(0),
				"open": value.NewBool(false),
			}
			return value.NewMapWithData(socketFields)
		}

		// Check buffered connection from select
		var conn net.Conn
		var err error

		if bufferedConn, ok := vm.netBufferedConns[fd]; ok {
			conn = bufferedConn
			delete(vm.netBufferedConns, fd)
		} else {
			conn, err = listener.Accept()
		}

		if err != nil {
			socketFields := map[string]value.Value{
				"fd":   value.NewInt(-1),
				"addr": value.NewString(""),
				"port": value.NewInt(0),
				"open": value.NewBool(false),
			}
			return value.NewMapWithData(socketFields)
		}

		id := vm.nextNetID
		vm.nextNetID++
		vm.netConns[id] = conn

		remoteAddr := conn.RemoteAddr().String()
		socketFields := map[string]value.Value{
			"fd":   value.NewInt(int64(id)),
			"addr": value.NewString(remoteAddr),
			"port": value.NewInt(0),
			"open": value.NewBool(true),
		}
		return value.NewMapWithData(socketFields)
	})

	vm.defineNative("net_connect", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		host := args[0].String()
		port := int(args[1].AsInt)
		addr := fmt.Sprintf("%s:%d", host, port)

		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			socketFields := map[string]value.Value{
				"fd":   value.NewInt(-1),
				"addr": value.NewString(host),
				"port": value.NewInt(int64(port)),
				"open": value.NewBool(false),
			}
			return value.NewMapWithData(socketFields)
		}

		id := vm.nextNetID
		vm.nextNetID++
		vm.netConns[id] = conn

		socketFields := map[string]value.Value{
			"fd":   value.NewInt(int64(id)),
			"addr": value.NewString(host),
			"port": value.NewInt(int64(port)),
			"open": value.NewBool(true),
		}
		return value.NewMapWithData(socketFields)
	})

	vm.defineNative("net_recv", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		sockMap, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull()
		}
		fdVal, _ := sockMap.Data["fd"]
		fd := int(fdVal.AsInt)
		size := int(args[1].AsInt)

		conn, ok := vm.netConns[fd]
		if !ok {
			resultFields := map[string]value.Value{
				"ok":    value.NewBool(false),
				"data":  value.NewBytes(""),
				"count": value.NewInt(0),
				"error": value.NewString("invalid socket"),
			}
			return value.NewMapWithData(resultFields)
		}

		var n int
		buf := make([]byte, size)

		// Check buffered data from select
		if buffered, ok := vm.netBufferedData[fd]; ok {
			// Copy buffered data
			copy(buf, buffered)
			n = len(buffered)
			delete(vm.netBufferedData, fd)
		}

		// Try to read more if space available
		if n < size {
			// Set short deadline to avoid blocking event loop
			conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
			n2, err2 := conn.Read(buf[n:])
			if n2 > 0 {
				n += n2
			}
			// Reset deadline
			conn.SetReadDeadline(time.Time{})

			// Ignore timeout errors if we have at least some data
			if err2 != nil {
				// If we have data, we return it. If we have no data (n==0), we might return error.
				// But buffer might have given us data.
				// If err2 is EOF, we still want to return data we have.
				if n == 0 {
					// Only return error if we really got nothing
					if err2 != nil && n2 == 0 {
						// If it's just timeout and n=0? Logic above handles buffer.
						// If n=0 and read failed, return failure.
						resultFields := map[string]value.Value{
							"ok":    value.NewBool(false),
							"data":  value.NewBytes(""),
							"count": value.NewInt(0),
							"error": value.NewString(err2.Error()),
						}
						return value.NewMapWithData(resultFields)
					}
				}
			}
		}

		resultFields := map[string]value.Value{
			"ok":    value.NewBool(true),
			"data":  value.NewBytes(string(buf[:n])),
			"count": value.NewInt(int64(n)),
			"error": value.NewString(""),
		}
		return value.NewMapWithData(resultFields)
	})

	vm.defineNative("net_send", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		sockMap, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull()
		}
		fdVal, _ := sockMap.Data["fd"]
		fd := int(fdVal.AsInt)
		data := args[1].String() // bytes are stored as strings internally

		conn, ok := vm.netConns[fd]
		if !ok {
			resultFields := map[string]value.Value{
				"ok":    value.NewBool(false),
				"data":  value.NewBytes(""),
				"count": value.NewInt(0),
				"error": value.NewString("invalid socket"),
			}
			return value.NewMapWithData(resultFields)
		}

		n, err := conn.Write([]byte(data))
		if err != nil {
			resultFields := map[string]value.Value{
				"ok":    value.NewBool(false),
				"data":  value.NewBytes(""),
				"count": value.NewInt(0),
				"error": value.NewString(err.Error()),
			}
			return value.NewMapWithData(resultFields)
		}

		resultFields := map[string]value.Value{
			"ok":    value.NewBool(true),
			"data":  value.NewBytes(""),
			"count": value.NewInt(int64(n)),
			"error": value.NewString(""),
		}
		return value.NewMapWithData(resultFields)
	})

	vm.defineNative("net_close", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		sockMap, ok := args[0].Obj.(*value.ObjMap)
		if !ok {
			return value.NewNull()
		}
		fdVal, _ := sockMap.Data["fd"]
		fd := int(fdVal.AsInt)

		// Try closing as listener
		if listener, ok := vm.netListeners[fd]; ok {
			listener.Close()
			delete(vm.netListeners, fd)
			return value.NewNull()
		}

		// Try closing as connection
		if conn, ok := vm.netConns[fd]; ok {
			conn.Close()
			delete(vm.netConns, fd)
		}

		return value.NewNull()
	})

	vm.defineNative("net_setblocking", func(args []value.Value) value.Value {
		// For TCP in Go, blocking is handled at a different level
		// This is a no-op for now, as Go handles timeouts via SetDeadline
		return value.NewNull()
	})

	vm.defineNative("net_select", func(args []value.Value) value.Value {
		// args: read, write (ignored), err (ignored), timeout
		if len(args) < 4 {
			return value.NewNull() // Or error map
		}

		timeoutMs := int(args[3].AsInt)
		// Minimum 1ms to allow polling
		if timeoutMs < 1 {
			timeoutMs = 1
		}

		// Prepare Result Data
		readyRead := make([]value.Value, 0)

		// Parse Read Set
		readArrVal := args[0]
		if readArrVal.Type == value.VAL_OBJ {
			if arr, ok := readArrVal.Obj.(*value.ObjArray); ok {
				for _, el := range arr.Elements {
					if el.Type == value.VAL_OBJ { // Check if socket (Map or Instance)
						// Extract FD
						var fd int64 = -1

						if m, ok := el.Obj.(*value.ObjMap); ok {
							if f, ok := m.Data["fd"]; ok {
								fd = f.AsInt
							}
						} else if inst, ok := el.Obj.(*value.ObjInstance); ok {
							if f, ok := inst.Fields["fd"]; ok {
								fd = f.AsInt
							}
						}

						if fd != -1 {
							isReady := false
							id := int(fd)

							// 1. Check buffers first
							if _, ok := vm.netBufferedConns[id]; ok {
								isReady = true
							} else if _, ok := vm.netBufferedData[id]; ok {
								isReady = true
							} else {
								// 2. Poll
								if l, ok := vm.netListeners[id]; ok {
									// Set short deadline to peek
									// Cast to TCPListener to set deadline
									// net.Listener interface doesn't have SetDeadline, specific implementations do.
									// But Accept() blocks.
									// We can only use SetDeadline if we have access to underlying FD or type assertion.
									// Assuming TCPListener
									if tcpL, ok := l.(*net.TCPListener); ok {
										tcpL.SetDeadline(time.Now().Add(time.Millisecond * time.Duration(timeoutMs)))
										conn, err := l.Accept()
										if err == nil {
											isReady = true
											vm.netBufferedConns[id] = conn
										}
										// Reset deadline?
										tcpL.SetDeadline(time.Time{})
									}
								} else if conn, ok := vm.netConns[id]; ok {
									conn.SetReadDeadline(time.Now().Add(time.Millisecond * time.Duration(timeoutMs)))
									buf := make([]byte, 1) // Peek 1 byte
									n, err := conn.Read(buf)
									if err == nil && n > 0 {
										isReady = true
										// Buffer the data
										vm.netBufferedData[id] = buf[:n]
									}
									// Reset deadline
									conn.SetReadDeadline(time.Time{})
								}
							}

							if isReady {
								readyRead = append(readyRead, el)
							}
						}
					}
				}
			}
		}

		// Construct SelectResult Map
		// struct SelectResult { read: Socket[64], read_count: int, ... }

		// Fill read array up to 64
		resReadArr := make([]value.Value, 64)
		for i := 0; i < 64; i++ {
			if i < len(readyRead) {
				resReadArr[i] = readyRead[i]
			} else {
				resReadArr[i] = value.NewNull()
			}
		}

		// Empties for others
		emptyArr := make([]value.Value, 64)
		for i := 0; i < 64; i++ {
			emptyArr[i] = value.NewNull()
		}

		resFields := map[string]value.Value{
			"read":        value.NewArray(resReadArr),
			"read_count":  value.NewInt(int64(len(readyRead))),
			"write":       value.NewArray(emptyArr),
			"write_count": value.NewInt(0),
			"error":       value.NewArray(emptyArr),
			"error_count": value.NewInt(0),
		}
		return value.NewMapWithData(resFields)
	})

	// SQLite Native Functions
	vm.defineNative("sqlite_open", func(args []value.Value) value.Value {
		if len(args) != 2 {
			return value.NewNull()
		} // path, wrapper struct
		path := args[0].String()
		structInst, ok := args[1].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		structDef := structInst.Struct

		db, err := sql.Open("sqlite", path)
		openVal := true
		if err != nil {
			openVal = false
		} else {
			if err = db.Ping(); err != nil {
				openVal = false
			}
		}

		id := vm.nextDbID
		vm.nextDbID++
		vm.dbHandles[id] = db

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		inst.Fields["handle"] = value.NewInt(int64(id))
		inst.Fields["open"] = value.NewBool(openVal)

		return value.Value{Type: value.VAL_OBJ, Obj: inst}
	})

	vm.defineNative("sqlite_close", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		dbInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}

		handle := int(dbInst.Fields["handle"].AsInt)
		if db, ok := vm.dbHandles[handle]; ok {
			db.Close()
			delete(vm.dbHandles, handle)
			dbInst.Fields["open"] = value.NewBool(false)
		}
		return value.NewNull()
	})

	vm.defineNative("sqlite_exec", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		}
		dbInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		sqlStr := args[1].String()

		resTmplInst, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct := resTmplInst.Struct

		handle := int(dbInst.Fields["handle"].AsInt)
		if db, ok := vm.dbHandles[handle]; ok {
			result, err := db.Exec(sqlStr)
			resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
			if err != nil {
				resInst.Fields["ok"] = value.NewBool(false)
				resInst.Fields["error"] = value.NewString(err.Error())
				resInst.Fields["rows_affected"] = value.NewInt(0)
				resInst.Fields["last_insert_id"] = value.NewInt(0)
			} else {
				rowsAffected, _ := result.RowsAffected()
				lastId, _ := result.LastInsertId()
				resInst.Fields["ok"] = value.NewBool(true)
				resInst.Fields["error"] = value.NewString("")
				resInst.Fields["rows_affected"] = value.NewInt(rowsAffected)
				resInst.Fields["last_insert_id"] = value.NewInt(lastId)
			}
			return value.Value{Type: value.VAL_OBJ, Obj: resInst}
		}
		// Invalid handle
		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["ok"] = value.NewBool(false)
		resInst.Fields["error"] = value.NewString("invalid database handle")
		resInst.Fields["rows_affected"] = value.NewInt(0)
		resInst.Fields["last_insert_id"] = value.NewInt(0)
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})

	vm.defineNative("sqlite_prepare", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		} // db, sql, stmt wrapper
		dbInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		sqlStr := args[1].String()
		stmtInst, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		stmtStructDef := stmtInst.Struct

		handle := int(dbInst.Fields["handle"].AsInt)
		if db, ok := vm.dbHandles[handle]; ok {
			stmt, err := db.Prepare(sqlStr)
			if err == nil {
				id := vm.nextStmtID
				vm.nextStmtID++
				vm.stmtHandles[id] = stmt
				vm.stmtParams[id] = make(map[int]interface{})

				inst := value.NewInstance(stmtStructDef).Obj.(*value.ObjInstance)
				inst.Fields["handle"] = value.NewInt(int64(id))
				return value.Value{Type: value.VAL_OBJ, Obj: inst}
			}
		}
		return value.NewNull()
	})

	bindFunc := func(args []value.Value, val interface{}) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		}
		stmtInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		idx := int(args[1].AsInt)

		handle := int(stmtInst.Fields["handle"].AsInt)
		if _, ok := vm.stmtHandles[handle]; ok {
			if vm.stmtParams[handle] == nil {
				vm.stmtParams[handle] = make(map[int]interface{})
			}
			vm.stmtParams[handle][idx] = val
		}
		return value.NewNull()
	}

	vm.defineNative("sqlite_bind_text", func(args []value.Value) value.Value {
		return bindFunc(args, args[2].String())
	})
	vm.defineNative("sqlite_bind_float", func(args []value.Value) value.Value {
		return bindFunc(args, args[2].AsFloat)
	})
	vm.defineNative("sqlite_bind_int", func(args []value.Value) value.Value {
		return bindFunc(args, args[2].AsInt)
	})

	vm.defineNative("sqlite_step_exec", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		stmtInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resTmplInst, ok := args[1].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct := resTmplInst.Struct

		handle := int(stmtInst.Fields["handle"].AsInt)
		if stmt, ok := vm.stmtHandles[handle]; ok {
			params := vm.stmtParams[handle]
			var maxIdx int
			for k := range params {
				if k > maxIdx {
					maxIdx = k
				}
			}
			argsList := make([]interface{}, maxIdx)
			for k, v := range params {
				if k > 0 && k <= maxIdx {
					argsList[k-1] = v
				}
			}
			result, err := stmt.Exec(argsList...)

			resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
			if err != nil {
				resInst.Fields["ok"] = value.NewBool(false)
				resInst.Fields["error"] = value.NewString(err.Error())
				resInst.Fields["rows_affected"] = value.NewInt(0)
				resInst.Fields["last_insert_id"] = value.NewInt(0)
			} else {
				rowsAffected, _ := result.RowsAffected()
				lastId, _ := result.LastInsertId()
				resInst.Fields["ok"] = value.NewBool(true)
				resInst.Fields["error"] = value.NewString("")
				resInst.Fields["rows_affected"] = value.NewInt(rowsAffected)
				resInst.Fields["last_insert_id"] = value.NewInt(lastId)
			}
			return value.Value{Type: value.VAL_OBJ, Obj: resInst}
		}

		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["ok"] = value.NewBool(false)
		resInst.Fields["error"] = value.NewString("invalid statement handle")
		resInst.Fields["rows_affected"] = value.NewInt(0)
		resInst.Fields["last_insert_id"] = value.NewInt(0)
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})

	vm.defineNative("sqlite_reset", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		stmtInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		handle := int(stmtInst.Fields["handle"].AsInt)
		if _, ok := vm.stmtHandles[handle]; ok {
			vm.stmtParams[handle] = make(map[int]interface{})
		}
		return value.NewNull()
	})

	vm.defineNative("sqlite_finalize", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		stmtInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		handle := int(stmtInst.Fields["handle"].AsInt)
		if stmt, ok := vm.stmtHandles[handle]; ok {
			stmt.Close()
			delete(vm.stmtHandles, handle)
			delete(vm.stmtParams, handle)
		}
		return value.NewNull()
	})

	vm.defineNative("sqlite_query", func(args []value.Value) value.Value {
		if len(args) < 4 {
			return value.NewNull()
		} // db, sql, tmplQueryResult, tmplRow

		dbInst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		sqlStr := args[1].String()

		resTmplInst, ok := args[2].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct := resTmplInst.Struct

		rowTmplInst, ok := args[3].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		rowStruct := rowTmplInst.Struct

		handle := int(dbInst.Fields["handle"].AsInt)
		if db, ok := vm.dbHandles[handle]; ok {
			rows, err := db.Query(sqlStr)
			if err != nil {
				// Return QueryResult with ok=false and error message
				resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
				resInst.Fields["columns"] = value.NewArray(nil)
				resInst.Fields["rows"] = value.NewArray(nil)
				resInst.Fields["row_count"] = value.NewInt(0)
				resInst.Fields["ok"] = value.NewBool(false)
				resInst.Fields["error"] = value.NewString(err.Error())
				return value.Value{Type: value.VAL_OBJ, Obj: resInst}
			}
			defer rows.Close()

			cols, _ := rows.Columns()
			colVals := make([]value.Value, len(cols))
			for i, c := range cols {
				colVals[i] = value.NewString(c)
			}

			var rowInsts []value.Value

			for rows.Next() {
				// Scan to interface{}
				dest := make([]interface{}, len(cols))
				destPtrs := make([]interface{}, len(cols))
				for i := range dest {
					destPtrs[i] = &dest[i]
				}

				rows.Scan(destPtrs...)

				rowVals := make([]value.Value, len(cols))
				for i, v := range dest {
					// Convert Go type to Noxy value
					switch tv := v.(type) {
					case nil:
						rowVals[i] = value.NewNull()
					case int64:
						rowVals[i] = value.NewInt(tv)
					case float64:
						rowVals[i] = value.NewFloat(tv)
					case string:
						rowVals[i] = value.NewString(tv)
					case []byte:
						rowVals[i] = value.NewString(string(tv))
					default:
						rowVals[i] = value.NewString(fmt.Sprintf("%v", tv))
					}
				}

				// Create Row instance
				rowInst := value.NewInstance(rowStruct).Obj.(*value.ObjInstance)
				rowInst.Fields["values"] = value.NewArray(rowVals)
				rowInsts = append(rowInsts, value.Value{Type: value.VAL_OBJ, Obj: rowInst})
			}

			// Create QueryResult instance with ok=true
			resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
			resInst.Fields["columns"] = value.NewArray(colVals)
			resInst.Fields["rows"] = value.NewArray(rowInsts)
			resInst.Fields["row_count"] = value.NewInt(int64(len(rowInsts)))
			resInst.Fields["ok"] = value.NewBool(true)
			resInst.Fields["error"] = value.NewString("")

			return value.Value{Type: value.VAL_OBJ, Obj: resInst}
		}
		// DB handle not found - return error result
		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["columns"] = value.NewArray(nil)
		resInst.Fields["rows"] = value.NewArray(nil)
		resInst.Fields["row_count"] = value.NewInt(0)
		resInst.Fields["ok"] = value.NewBool(false)
		resInst.Fields["error"] = value.NewString("invalid database handle")
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})

	return vm
}

func (vm *VM) defineNative(name string, fn value.NativeFunc) {
	vm.globals[name] = value.NewNative(name, fn)
}

func (vm *VM) Interpret(c *chunk.Chunk) error {
	// Default to VM globals
	return vm.InterpretWithGlobals(c, vm.globals)
}

func (vm *VM) InterpretWithGlobals(c *chunk.Chunk, globals map[string]value.Value) error {
	scriptFn := &value.ObjFunction{
		Name:    "script",
		Arity:   0,
		Chunk:   c,
		Globals: globals,
	}

	vm.stackTop = 0
	vm.push(value.NewFunction("script", 0, c, globals)) // Push script function to stack slot 0

	// Call frame for script
	frame := &CallFrame{
		Function: scriptFn,
		IP:       0,
		Slots:    1, // Locals start at 1
		Globals:  globals,
	}
	vm.frames[0] = frame
	vm.frameCount = 1
	vm.currentFrame = frame

	return vm.run(1)
}

func (vm *VM) run(minFrameCount int) error {
	// Cache current frame values for speed
	frame := vm.currentFrame
	c := frame.Function.Chunk.(*chunk.Chunk)
	ip := frame.IP

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

			// If it's a function, bind it to current globals (Module binding)
			if constant.Type == value.VAL_FUNCTION {
				fn := constant.Obj.(*value.ObjFunction)
				// Clone to bind globals
				boundFn := &value.ObjFunction{
					Name:    fn.Name,
					Arity:   fn.Arity,
					Chunk:   fn.Chunk,
					Globals: frame.Globals,
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

			// Try frame globals (Module scope)
			val, ok := frame.Globals[name]
			if !ok {
				// Try VM globals (Builtins / Shared)
				val, ok = vm.globals[name]
				if !ok {
					return fmt.Errorf("undefined global variable '%s'", name)
				}
			}
			vm.push(val)

		case chunk.OP_SET_GLOBAL:
			index := c.Code[ip]
			ip++
			nameVal := c.Constants[index]
			name := nameVal.Obj.(string)
			// Set in frame globals (Module scope)
			if frame.Globals != nil {
				frame.Globals[name] = vm.peek(0)
			} else {
				vm.globals[name] = vm.peek(0)
			}

		case chunk.OP_GET_LOCAL:
			slot := c.Code[ip]
			ip++
			val := vm.stack[frame.Slots+int(slot)]
			vm.push(val)

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
			if v.Type == value.VAL_BOOL {
				vm.push(value.NewBool(!v.AsBool))
			} else {
				return fmt.Errorf("operand must be boolean")
			}
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
			// Return from function

			// 1. Pop result
			result := vm.pop()
			calleeFrame := vm.currentFrame

			// 2. Decrement frame count
			vm.frameCount--

			// 3. Update current frame pointer (Always do this to keep state consistent)
			if vm.frameCount > 0 {
				vm.currentFrame = vm.frames[vm.frameCount-1]
			} else {
				vm.currentFrame = nil // Or handle main return
			}

			// 4. Return from run() if we drop below call depth
			if vm.frameCount < minFrameCount {
				vm.pop() // Pop script function (effectively)
				// Note: vm.push(result) ?
				// Usually result is consumed by caller.
				// If we return from run(), result is on stack?
				// vm.pop() above removed result.
				// We need to leave result on stack for caller if this is a nested run.
				// Original code: vm.pop() -> result. vm.pop() -> func. return.
				// Caller (OP_CALL) expects result pushed.
				// If we return from run(), who pushes result?
				// The caller of run()!
				// vm.loadModule does: vm.pop() -> pops result.
				// So we must PUSH result back before returning?
				// NO. The stack must be balanced.
				// OP_RETURN pops result.
				// Then it pops CALLEE (script/func).
				// Then it pushes result.

				// If we return from run(), we should probably leave result on stack.
				vm.push(result)
				return nil
			}

			// 5. Restore execution context
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

			// Refresh frame if import caused GC or stack moves (unlikely but safe)
			// And if recursive run changed things?
			frame = vm.currentFrame
			// c/ip are from frame. c/ip in local vars are stale?
			// frame IS vm.currentFrame.
			// After loadModule, frame is valid (frames[0]).
			// c/ip valid.
			// But to be safe:
			// frame = vm.currentFrame -- Done.

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
				} else if str, ok := collectionVal.Obj.(string); ok {
					// String indexing
					if indexVal.Type != value.VAL_INT {
						return fmt.Errorf("string index must be integer")
					}
					idx := int(indexVal.AsInt)
					if idx < 0 || idx >= len(str) {
						return fmt.Errorf("string index out of bounds")
					}
					vm.push(value.NewString(string(str[idx])))
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
		Globals:  fn.Globals,
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
	// Convert dot notation to path path separator
	pathName := strings.ReplaceAll(name, ".", string(filepath.Separator))

	// Search paths candidates (File .nx OR Directory)
	// We prefer file over directory if both exist? usually explicit file wins.
	// But let's check both possibilities.

	var path string
	var isDir bool
	// found := false // Unused

	// Helper to check 4 locations
	checkLocations := func(suffix string) bool {
		candidates := []string{
			filepath.Join(vm.Config.RootPath, "stdlib", suffix),
			filepath.Join(vm.Config.RootPath, suffix),
			filepath.Join("stdlib", suffix),
			suffix,
		}
		for _, p := range candidates {
			info, err := os.Stat(p)
			if err == nil {
				path = p
				isDir = info.IsDir()
				// found = true
				return true
			}
		}
		return false
	}

	// 1. Check for .nx file
	if checkLocations(pathName+".nx") && !isDir {
		// Found file
	} else if checkLocations(pathName) && isDir {
		// Found directory (on disk)
	} else {
		// Not found on disk, check embedded stdlib
		// Stdlib is flat in embed.go usually? Or structure preserved?
		// We moved stdlib/* to internal/stdlib.
		// So internal/stdlib has *.nx files directly.
		// Name would be "time" "io" etc.
		// pathName for "time" is "time".
		// embedded file is "time.nx".

		// Check if it exists in embedded FS
		embedPath := pathName + ".nx"
		content, err := stdlib.FS.ReadFile(embedPath)
		if err == nil {
			// Found in embedded stdlib!
			l := lexer.New(string(content))
			p := parser.New(l)
			prog := p.ParseProgram()
			if len(p.Errors()) > 0 {
				return value.NewNull(), fmt.Errorf("parse error in embedded module %s: %v", name, p.Errors())
			}
			c := compiler.New()
			chunk, err := c.Compile(prog)
			if err != nil {
				return value.NewNull(), err
			}
			moduleGlobals := make(map[string]value.Value)
			modFn := &value.ObjFunction{
				Name:    name,
				Arity:   0,
				Chunk:   chunk,
				Globals: moduleGlobals,
			}
			modVal := value.Value{Type: value.VAL_FUNCTION, Obj: modFn}
			vm.push(modVal)
			vm.callValue(modVal, 0)
			err = vm.run(vm.frameCount) // Run until return
			if err != nil {
				return value.NewNull(), err
			}
			vm.pop() // Pop result
			return value.NewMapWithData(moduleGlobals), nil
		}

		return value.NewNull(), fmt.Errorf("module not found: %s", name)
	}

	// Case 1: Directory Import (Implicit Module)
	if isDir {
		files, err := os.ReadDir(path)
		if err != nil {
			return value.NewNull(), err
		}

		moduleGlobals := make(map[string]value.Value)

		for _, f := range files {
			if f.IsDir() {
				// Recursively load subdirectory as a nested module
				subDirName := name + "." + f.Name()
				subMod, err := vm.loadModule(subDirName)
				if err != nil {
					// Ignore subdirectories that fail to load (e.g., empty or invalid)
					continue
				}
				moduleGlobals[f.Name()] = subMod
			} else if strings.HasSuffix(f.Name(), ".nx") {
				baseName := strings.TrimSuffix(f.Name(), ".nx")
				subModuleName := name + "." + baseName

				// Recursive load
				subMod, err := vm.loadModule(subModuleName)
				if err != nil {
					return value.NewNull(), fmt.Errorf("failed to load submodule %s: %v", subModuleName, err)
				}
				moduleGlobals[baseName] = subMod
			}
		}
		return value.NewMapWithData(moduleGlobals), nil
	}

	// Case 2: File Import
	content, err := os.ReadFile(path)
	if err != nil {
		return value.NewNull(), err
	}

	l := lexer.New(string(content))
	p := parser.New(l)
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 {
		return value.NewNull(), fmt.Errorf("parse error in module %s: %v", name, p.Errors())
	}

	c := compiler.New()
	chunk, err := c.Compile(prog)
	if err != nil {
		return value.NewNull(), err
	}

	// Create isolated Module Globals
	moduleGlobals := make(map[string]value.Value)

	// Prepare Module Function
	modFn := &value.ObjFunction{
		Name:    name,
		Arity:   0,
		Chunk:   chunk,
		Globals: moduleGlobals,
	}
	modVal := value.Value{Type: value.VAL_FUNCTION, Obj: modFn}

	// Execute Module Synchronously
	vm.push(modVal)
	vm.callValue(modVal, 0)

	// Run until this frame returns
	startFrameCount := vm.frameCount
	err = vm.run(startFrameCount)
	if err != nil {
		return value.NewNull(), err
	}

	// Module execution finished.
	// The result of module (usually null) is on stack. Pop it.
	vm.pop()

	// Return the Module Map
	return value.NewMapWithData(moduleGlobals), nil
}

func (vm *VM) peek(distance int) value.Value {
	return vm.stack[vm.stackTop-1-distance]

}
