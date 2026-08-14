package vm

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"noxy-vm/internal/value"
)

func (vm *VM) defineIOBuiltins() {
	vm.DefineContextualNative("io_open", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 3 {
			return value.NewNull(), nil
		}
		path := args[0].String()
		mode := args[1].String()

		structDef, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}

		flag := os.O_RDONLY
		if mode == "w" {
			flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		} else if mode == "a" {
			flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
		} else if mode == "rw" || mode == "r+" {
			flag = os.O_RDWR | os.O_CREATE
		}

		file, openErr := os.OpenFile(path, flag, 0644)
		isOpen := openErr == nil
		var fd int64
		if isOpen {
			fd = int64(machine.shared.Files.add(&FileResource{file: file}))
		}

		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		machine.shared.fileMetaMu.Lock()
		inst.Fields["fd"] = value.NewInt(fd)
		inst.Fields["path"] = value.NewString(path)
		inst.Fields["mode"] = value.NewString(mode)
		inst.Fields["open"] = value.NewBool(isOpen)
		machine.shared.fileMetaMu.Unlock()
		return value.Value{Type: value.VAL_OBJ, Obj: inst}, nil
	})

	vm.DefineContextualNative("io_close", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), nil
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}

		resource, exists := machine.shared.Files.remove(fileHandle(machine.shared, inst))
		if exists {
			_ = resource.close()
			markFileClosed(machine.shared, inst)
		}
		return value.NewNull(), nil
	})

	vm.DefineContextualNative("io_close_result", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultStruct, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}

		result := value.NewInstance(resultStruct).Obj.(*value.ObjInstance)
		result.Fields["success"] = value.NewBool(false)
		result.Fields["error"] = value.NewString("File not open")
		resource, exists := machine.shared.Files.remove(fileHandle(machine.shared, inst))
		if !exists {
			return value.Value{Type: value.VAL_OBJ, Obj: result}, nil
		}

		closeErr := resource.close()
		markFileClosed(machine.shared, inst)
		if closeErr != nil {
			result.Fields["error"] = value.NewString(closeErr.Error())
			return value.Value{Type: value.VAL_OBJ, Obj: result}, nil
		}
		result.Fields["success"] = value.NewBool(true)
		result.Fields["error"] = value.NewString("")
		return value.Value{Type: value.VAL_OBJ, Obj: result}, nil
	})

	vm.DefineContextualNative("io_write", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resource, exists := machine.shared.Files.get(fileHandle(machine.shared, inst))
		if !exists {
			return value.NewNull(), nil
		}
		resource.use(func(file *os.File) value.Value {
			if args[1].Type == value.VAL_BYTES {
				_, _ = file.Write([]byte(args[1].Obj.(string)))
			} else {
				_, _ = file.WriteString(args[1].String())
			}
			return value.NewNull()
		})
		return value.NewNull(), nil
	})

	vm.DefineContextualNative("io_write_result", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 3 {
			return value.NewNull(), nil
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultStruct, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}

		result := value.NewInstance(resultStruct).Obj.(*value.ObjInstance)
		result.Fields["success"] = value.NewBool(false)
		result.Fields["bytes_written"] = value.NewInt(0)
		result.Fields["error"] = value.NewString("File not open")
		resource, exists := machine.shared.Files.get(fileHandle(machine.shared, inst))
		if !exists {
			return value.Value{Type: value.VAL_OBJ, Obj: result}, nil
		}

		operationResult, used := resource.use(func(file *os.File) value.Value {
			var written int
			var writeErr error
			if args[1].Type == value.VAL_BYTES {
				written, writeErr = file.Write([]byte(args[1].Obj.(string)))
			} else {
				written, writeErr = file.WriteString(args[1].String())
			}
			result.Fields["bytes_written"] = value.NewInt(int64(written))
			if writeErr != nil {
				result.Fields["error"] = value.NewString(writeErr.Error())
				return value.Value{Type: value.VAL_OBJ, Obj: result}
			}
			result.Fields["success"] = value.NewBool(true)
			result.Fields["error"] = value.NewString("")
			return value.Value{Type: value.VAL_OBJ, Obj: result}
		})
		if !used {
			return value.Value{Type: value.VAL_OBJ, Obj: result}, nil
		}
		return operationResult, nil
	})

	vm.DefineContextualNative("io_read", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultStruct, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}

		result := newIOReadResult(resultStruct, false, value.NewString(""), "File not open")
		resource, exists := machine.shared.Files.get(fileHandle(machine.shared, inst))
		if !exists {
			return result, nil
		}
		operationResult, used := resource.use(func(file *os.File) value.Value {
			content, ok, errorText := readFileContents(file)
			return newIOReadResult(resultStruct, ok, value.NewString(string(content)), errorText)
		})
		if !used {
			return result, nil
		}
		return operationResult, nil
	})

	vm.DefineContextualNative("io_read_bytes", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultStruct, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}

		result := newIOReadResult(resultStruct, false, value.NewBytes(""), "File not open")
		resource, exists := machine.shared.Files.get(fileHandle(machine.shared, inst))
		if !exists {
			return result, nil
		}
		operationResult, used := resource.use(func(file *os.File) value.Value {
			content, ok, errorText := readFileContents(file)
			return newIOReadResult(resultStruct, ok, value.NewBytes(string(content)), errorText)
		})
		if !used {
			return result, nil
		}
		return operationResult, nil
	})

	vm.DefineNative("io_exists", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		_, err := os.Stat(args[0].String())
		return value.NewBool(err == nil)
	})
	vm.DefineNative("io_remove", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewBool(false)
		}
		return value.NewBool(os.Remove(args[0].String()) == nil)
	})

	vm.DefineContextualNative("io_read_lines", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), nil
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultStruct, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}

		result := newIOLinesResult(resultStruct, false, nil, "File not open")
		resource, exists := machine.shared.Files.get(fileHandle(machine.shared, inst))
		if !exists {
			return result, nil
		}
		operationResult, used := resource.use(func(file *os.File) value.Value {
			content, ok, errorText := readFileContents(file)
			var lines []string
			if ok {
				normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
				lines = strings.Split(normalized, "\n")
			}
			return newIOLinesResult(resultStruct, ok, lines, errorText)
		})
		if !used {
			return result, nil
		}
		return operationResult, nil
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
		exists := err == nil
		var size int64
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
		return value.NewBool(os.MkdirAll(args[0].String(), 0755) == nil)
	})
	vm.DefineNative("input", func(args []value.Value) value.Value {
		if len(args) > 0 {
			fmt.Print(args[0].String())
		}
		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		return value.NewString(strings.TrimRight(text, "\r\n"))
	})
}

func fileHandle(shared *SharedState, instance *value.ObjInstance) int {
	shared.fileMetaMu.RLock()
	handle := int(instance.Fields["fd"].AsInt)
	shared.fileMetaMu.RUnlock()
	return handle
}

func markFileClosed(shared *SharedState, instance *value.ObjInstance) {
	shared.fileMetaMu.Lock()
	instance.Fields["open"] = value.NewBool(false)
	shared.fileMetaMu.Unlock()
}

func readFileContents(file *os.File) ([]byte, bool, string) {
	stat, err := file.Stat()
	if err != nil {
		return nil, false, err.Error()
	}
	if stat.Size() == 0 {
		return []byte{}, true, ""
	}
	buffer := make([]byte, stat.Size())
	_, _ = file.Seek(0, 0)
	n, readErr := file.Read(buffer)
	if readErr == nil || n > 0 {
		return buffer[:n], true, ""
	}
	return nil, false, readErr.Error()
}

func newIOReadResult(definition *value.ObjStruct, ok bool, data value.Value, errorText string) value.Value {
	result := value.NewInstance(definition).Obj.(*value.ObjInstance)
	result.Fields["ok"] = value.NewBool(ok)
	result.Fields["data"] = data
	result.Fields["error"] = value.NewString(errorText)
	return value.Value{Type: value.VAL_OBJ, Obj: result}
}

func newIOLinesResult(definition *value.ObjStruct, ok bool, lines []string, errorText string) value.Value {
	values := make([]value.Value, len(lines))
	for index, line := range lines {
		values[index] = value.NewString(line)
	}
	return newIOReadResult(definition, ok, value.NewArray(values), errorText)
}
