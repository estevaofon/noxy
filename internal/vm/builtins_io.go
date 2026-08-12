package vm

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"noxy-vm/internal/value"
)

func (vm *VM) defineIOBuiltins() {
	vm.DefineNative("io_open", func(args []value.Value) value.Value {
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
	vm.DefineNative("io_close", func(args []value.Value) value.Value {
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
	vm.DefineNative("io_close_result", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resultStruct, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		result := value.NewInstance(resultStruct).Obj.(*value.ObjInstance)
		result.Fields["success"] = value.NewBool(false)
		result.Fields["error"] = value.NewString("File not open")

		fd := inst.Fields["fd"].AsInt
		f, exists := vm.openFiles[fd]
		if !exists {
			return value.Value{Type: value.VAL_OBJ, Obj: result}
		}

		err := f.Close()
		delete(vm.openFiles, fd)
		inst.Fields["open"] = value.NewBool(false)
		if err != nil {
			result.Fields["error"] = value.NewString(err.Error())
			return value.Value{Type: value.VAL_OBJ, Obj: result}
		}

		result.Fields["success"] = value.NewBool(true)
		result.Fields["error"] = value.NewString("")
		return value.Value{Type: value.VAL_OBJ, Obj: result}
	})
	vm.DefineNative("io_write", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}

		fd := inst.Fields["fd"].AsInt
		if f, exists := vm.openFiles[fd]; exists {
			if args[1].Type == value.VAL_BYTES {
				// Bytes are stored as string in Obj, but treat as raw bytes
				data := args[1].Obj.(string)
				f.Write([]byte(data))
			} else {
				content := args[1].String()
				f.WriteString(content)
			}
		}
		return value.NewNull()
	})
	vm.DefineNative("io_write_result", func(args []value.Value) value.Value {
		if len(args) < 3 {
			return value.NewNull()
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resultStruct, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		result := value.NewInstance(resultStruct).Obj.(*value.ObjInstance)
		result.Fields["success"] = value.NewBool(false)
		result.Fields["bytes_written"] = value.NewInt(0)
		result.Fields["error"] = value.NewString("File not open")

		fd := inst.Fields["fd"].AsInt
		f, exists := vm.openFiles[fd]
		if !exists {
			return value.Value{Type: value.VAL_OBJ, Obj: result}
		}

		var written int
		var err error
		if args[1].Type == value.VAL_BYTES {
			written, err = f.Write([]byte(args[1].Obj.(string)))
		} else {
			written, err = f.WriteString(args[1].String())
		}
		result.Fields["bytes_written"] = value.NewInt(int64(written))
		if err != nil {
			result.Fields["error"] = value.NewString(err.Error())
			return value.Value{Type: value.VAL_OBJ, Obj: result}
		}

		result.Fields["success"] = value.NewBool(true)
		result.Fields["error"] = value.NewString("")
		return value.Value{Type: value.VAL_OBJ, Obj: result}
	})
	vm.DefineNative("io_read", func(args []value.Value) value.Value {
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

	vm.DefineNative("io_read_bytes", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}

		fd := inst.Fields["fd"].AsInt
		var contentBytes []byte
		var errorStr string
		var isOk bool = false

		if f, exists := vm.openFiles[fd]; exists {
			// Read all
			stat, _ := f.Stat()
			if stat.Size() > 0 {
				buf := make([]byte, stat.Size())
				f.Seek(0, 0)
				n, err := f.Read(buf)
				if err == nil || (err != nil && n > 0) {
					contentBytes = buf[:n]
					isOk = true
				} else {
					errorStr = err.Error()
				}
			} else {
				contentBytes = []byte{}
				isOk = true
			}
		} else {
			errorStr = "File not open"
		}

		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["ok"] = value.NewBool(isOk)
		resInst.Fields["data"] = value.NewBytes(string(contentBytes))
		resInst.Fields["error"] = value.NewString(errorStr)
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})
	vm.DefineNative("io_exists", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		path := args[0].String()
		_, err := os.Stat(path)
		return value.NewBool(err == nil)
	})
	vm.DefineNative("io_remove", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		path := args[0].String()
		err := os.Remove(path)
		return value.NewBool(err == nil)
	})
	vm.DefineNative("io_read_lines", func(args []value.Value) value.Value {
		// args: fileInst, IOLinesResultStructDef
		if len(args) < 2 {
			return value.NewNull()
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		resStruct, ok := args[1].Obj.(*value.ObjStruct) // IOLinesResult
		if !ok {
			return value.NewNull()
		}

		fd := inst.Fields["fd"].AsInt
		var lines []string
		var errorStr string
		var isOk bool = false

		if f, exists := vm.openFiles[fd]; exists {
			// Read all
			stat, _ := f.Stat()
			var contentStr string
			if stat.Size() > 0 {
				f.Seek(0, 0)
				buf := make([]byte, stat.Size())
				n, err := f.Read(buf)
				if err == nil || (err != nil && n > 0) {
					contentStr = string(buf[:n])
					isOk = true
				} else {
					errorStr = err.Error()
				}
			} else {
				isOk = true
			}

			if isOk {
				// Split by newlines, handling \r\n and \n
				// Naive split
				contentStr = strings.ReplaceAll(contentStr, "\r\n", "\n")
				lines = strings.Split(contentStr, "\n")
				// Retain behavior of strings.Split where trailing newline results in an empty string.
			}
		} else {
			errorStr = "File not open"
		}

		resInst := value.NewInstance(resStruct).Obj.(*value.ObjInstance)
		resInst.Fields["ok"] = value.NewBool(isOk)

		linesVal := make([]value.Value, len(lines))
		for i, line := range lines {
			linesVal[i] = value.NewString(line)
		}
		resInst.Fields["data"] = value.NewArray(linesVal)

		resInst.Fields["error"] = value.NewString(errorStr)
		return value.Value{Type: value.VAL_OBJ, Obj: resInst}
	})
	vm.DefineNative("io_stat", func(args []value.Value) value.Value {
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
	vm.DefineNative("io_mkdir", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		path := args[0].String()
		err := os.MkdirAll(path, 0755)
		return value.NewBool(err == nil)
	})
	// Input
	vm.DefineNative("input", func(args []value.Value) value.Value {
		// args[0]: prompt (optional)
		if len(args) > 0 {
			fmt.Print(args[0].String())
		}
		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		// Trim newline (windows \r\n and unix \n)
		text = strings.TrimRight(text, "\r\n")
		return value.NewString(text)
	})
}
