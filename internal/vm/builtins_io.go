package vm

import (
	"fmt"
	"io"
	"os"
	"strings"

	"noxy-vm/internal/console"
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

		handle := fileHandle(machine.shared, inst)
		// stdin nao fecha e continua registrado: o handle de io.stdin() e unico
		// e vale por todo o processo.
		if resource, ok := machine.shared.Files.get(handle); ok && resource.stdin {
			return value.NewNull(), nil
		}
		resource, exists := machine.shared.Files.remove(handle)
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
		handle := fileHandle(machine.shared, inst)
		if resource, ok := machine.shared.Files.get(handle); ok && resource.stdin {
			result.Fields["error"] = value.NewString("stdin cannot be closed")
			return value.Value{Type: value.VAL_OBJ, Obj: result}, nil
		}
		resource, exists := machine.shared.Files.remove(handle)
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
			if resource.stdin {
				return value.NewNull()
			}
			// Escreve na posicao LOGICA do cursor: alinha o offset do SO com o
			// que read_line/read_n ja consumiram antes de escrever.
			if err := resource.syncCursor(file); err != nil {
				return value.NewNull()
			}
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
			if resource.stdin {
				result.Fields["error"] = value.NewString("stdin is read-only")
				return value.Value{Type: value.VAL_OBJ, Obj: result}
			}
			if err := resource.syncCursor(file); err != nil {
				result.Fields["error"] = value.NewString(err.Error())
				return value.Value{Type: value.VAL_OBJ, Obj: result}
			}
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
			content, ok, errorText := resource.readAll(file)
			if ok {
				if err := requireValidUTF8("io.read", string(content)); err != nil {
					return newIOReadResult(resultStruct, false, value.NewString(""), err.Error())
				}
			}
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
			content, ok, errorText := resource.readAll(file)
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
			content, ok, errorText := resource.readAll(file)
			var lines []string
			if ok {
				if err := requireValidUTF8("io.read_lines", string(content)); err != nil {
					return newIOLinesResult(resultStruct, false, nil, err.Error())
				}
				normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
				lines = splitLines(normalized)
			}
			return newIOLinesResult(resultStruct, ok, lines, errorText)
		})
		if !used {
			return result, nil
		}
		return operationResult, nil
	})

	vm.DefineContextualNative("io_read_line", func(context value.NativeContext, args []value.Value) (value.Value, error) {
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
			line, readErr := resource.lineReader(file).ReadString('\n')
			if readErr != nil && readErr != io.EOF {
				return newIOReadResult(resultStruct, false, value.NewString(""), readErr.Error())
			}
			if line == "" && readErr == io.EOF {
				return newIOReadResult(resultStruct, false, value.NewString(""), "EOF")
			}
			line = strings.TrimRight(line, "\r\n")
			if err := requireValidUTF8("io.read_line", line); err != nil {
				return newIOReadResult(resultStruct, false, value.NewString(""), err.Error())
			}
			return newIOReadResult(resultStruct, true, value.NewString(line), "")
		})
		if !used {
			return result, nil
		}
		return operationResult, nil
	})

	// io_seek(file, offset, whence, IOPositionResult): posiciona o cursor
	// LOGICO (SEEK_SET=0, SEEK_CUR=1, SEEK_END=2, como o lseek de C) e devolve
	// a nova posicao absoluta. stdin nao e posicionavel.
	vm.DefineContextualNative("io_seek", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 4 {
			return value.NewNull(), nil
		}
		inst, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull(), nil
		}
		resultStruct, ok := args[3].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}
		offset := args[1].AsInt
		whence := args[2].AsInt
		result := newIOPositionResult(resultStruct, false, -1, "File not open")
		resource, exists := machine.shared.Files.get(fileHandle(machine.shared, inst))
		if !exists {
			return result, nil
		}
		operationResult, used := resource.use(func(file *os.File) value.Value {
			if resource.stdin {
				return newIOPositionResult(resultStruct, false, -1, "stdin is not seekable")
			}
			if whence < 0 || whence > 2 {
				return newIOPositionResult(resultStruct, false, -1,
					fmt.Sprintf("invalid whence %d (use io.SEEK_SET, io.SEEK_CUR or io.SEEK_END)", whence))
			}
			// SEEK_CUR e relativo a posicao LOGICA: alinha o offset do SO com
			// ela (descartando o buffer de read_line/read_n) antes de mover. Um
			// Seek invalido (posicao negativa) falha sem mover.
			if err := resource.syncCursor(file); err != nil {
				return newIOPositionResult(resultStruct, false, -1, err.Error())
			}
			position, seekErr := file.Seek(offset, int(whence))
			if seekErr != nil {
				return newIOPositionResult(resultStruct, false, -1, seekErr.Error())
			}
			return newIOPositionResult(resultStruct, true, position, "")
		})
		if !used {
			return result, nil
		}
		return operationResult, nil
	})

	// io_tell(file, IOPositionResult): posicao LOGICA atual (offset do SO
	// menos o que o leitor bufferizado ainda nao entregou).
	vm.DefineContextualNative("io_tell", func(context value.NativeContext, args []value.Value) (value.Value, error) {
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
		result := newIOPositionResult(resultStruct, false, -1, "File not open")
		resource, exists := machine.shared.Files.get(fileHandle(machine.shared, inst))
		if !exists {
			return result, nil
		}
		operationResult, used := resource.use(func(file *os.File) value.Value {
			if resource.stdin {
				return newIOPositionResult(resultStruct, false, -1, "stdin is not seekable")
			}
			position, positionErr := resource.logicalPosition(file)
			if positionErr != nil {
				return newIOPositionResult(resultStruct, false, -1, positionErr.Error())
			}
			return newIOPositionResult(resultStruct, true, position, "")
		})
		if !used {
			return result, nil
		}
		return operationResult, nil
	})

	// io_read_n(file, count, IOBytesResult): ate count bytes a partir do
	// cursor logico, pelo MESMO leitor bufferizado de read_line (os dois
	// compoem). Menos de count so no fim do arquivo; sem nada para ler,
	// ok=false e error="EOF" — o contrato de read_line. Funciona em stdin.
	vm.DefineContextualNative("io_read_n", func(context value.NativeContext, args []value.Value) (value.Value, error) {
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
		count := args[1].AsInt
		result := newIOReadResult(resultStruct, false, value.NewBytes(""), "File not open")
		resource, exists := machine.shared.Files.get(fileHandle(machine.shared, inst))
		if !exists {
			return result, nil
		}
		operationResult, used := resource.use(func(file *os.File) value.Value {
			if count < 0 {
				return newIOReadResult(resultStruct, false, value.NewBytes(""), fmt.Sprintf("read_n: n must be >= 0, got %d", count))
			}
			if count == 0 {
				return newIOReadResult(resultStruct, true, value.NewBytes(""), "")
			}
			// LimitReader + ReadAll: cresce o buffer conforme le, em vez de
			// alocar count bytes de uma vez para um count enorme.
			data, readErr := io.ReadAll(io.LimitReader(resource.lineReader(file), count))
			if readErr != nil {
				return newIOReadResult(resultStruct, false, value.NewBytes(""), readErr.Error())
			}
			if len(data) == 0 {
				return newIOReadResult(resultStruct, false, value.NewBytes(""), "EOF")
			}
			return newIOReadResult(resultStruct, true, value.NewBytes(string(data)), "")
		})
		if !used {
			return result, nil
		}
		return operationResult, nil
	})

	vm.DefineNative("io_list_dir", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		resultStruct, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}
		entries, err := os.ReadDir(args[0].String())
		if err != nil {
			return newIOLinesResult(resultStruct, false, nil, err.Error())
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		return newIOLinesResult(resultStruct, true, names, "")
	})

	vm.DefineNative("io_rename", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		return value.NewBool(os.Rename(args[0].String(), args[1].String()) == nil)
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
	vm.DefineContextualNative("input", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		// Repair a raw console mode leaked by a crashed program before a
		// line-oriented read, which would otherwise block forever.
		console.EnsureLineInput()
		if len(args) > 0 {
			fmt.Print(args[0].String())
		}
		// Leitor unico (SharedState): em pipe/arquivo le TODAS as linhas. No
		// EOF devolve o parcial, ou "" — input() nao sinaliza EOF; para isso
		// use io.read_line(io.stdin()).
		//
		// A leitura passa pelo RECURSO de stdin (nao pelo shared.stdin() cru)
		// para pegar o mesmo operationMu que io.read_line/io.read usam: VMs de
		// tasks compartilham o SharedState, entao input() concorrente com
		// io.read_line(io.stdin()) mexeria no mesmo *bufio.Reader em paralelo.
		resource, exists := machine.shared.Files.get(machine.shared.stdinHandle())
		if !exists {
			return value.NewString(""), nil
		}
		text, used := resource.use(func(file *os.File) value.Value {
			line, _ := resource.lineReader(file).ReadString('\n')
			return value.NewString(strings.TrimRight(line, "\r\n"))
		})
		if !used {
			return value.NewString(""), nil
		}
		return text, nil
	})

	vm.DefineContextualNative("io_stdin", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), nil
		}
		structDef, ok := args[0].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), nil
		}
		inst := value.NewInstance(structDef).Obj.(*value.ObjInstance)
		// stdinHandle toma Files.mu: fora do fileMetaMu, para nao aninhar os
		// dois mutexes.
		handle := machine.shared.stdinHandle()
		machine.shared.fileMetaMu.Lock()
		inst.Fields["fd"] = value.NewInt(int64(handle))
		inst.Fields["path"] = value.NewString("<stdin>")
		inst.Fields["mode"] = value.NewString("r")
		inst.Fields["open"] = value.NewBool(true)
		machine.shared.fileMetaMu.Unlock()
		return value.Value{Type: value.VAL_OBJ, Obj: inst}, nil
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

// newIOPositionResult monta IOPositionResult {ok, position, error}; position
// e -1 sempre que ok=false.
func newIOPositionResult(definition *value.ObjStruct, ok bool, position int64, errorText string) value.Value {
	return value.NewInstanceWith(definition, map[string]value.Value{
		"ok":       value.NewBool(ok),
		"position": value.NewInt(position),
		"error":    value.NewString(errorText),
	})
}

func newIOReadResult(definition *value.ObjStruct, ok bool, data value.Value, errorText string) value.Value {
	// RC: NewInstanceWith retem data quando composto (array de read_lines);
	// string/bytes sao no-op.
	return value.NewInstanceWith(definition, map[string]value.Value{
		"ok":    value.NewBool(ok),
		"data":  data,
		"error": value.NewString(errorText),
	})
}

func newIOLinesResult(definition *value.ObjStruct, ok bool, lines []string, errorText string) value.Value {
	values := make([]value.Value, len(lines))
	for index, line := range lines {
		values[index] = value.NewString(line)
	}
	return newIOReadResult(definition, ok, value.NewArray(values), errorText)
}

// splitLines separa em linhas sem produzir o "" fantasma de um conteudo
// terminado em \n (#56 item 12): "a\nb\n" -> [a b], "a\nb" -> [a b],
// "\n" -> [""], "" -> [].
func splitLines(content string) []string {
	if content == "" {
		return []string{}
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}
