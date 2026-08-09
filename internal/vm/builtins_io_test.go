package vm

import (
	"path/filepath"
	"testing"

	"noxy-vm/internal/value"
)

func testFileDefinition() value.Value {
	return value.NewStruct("File", []string{"fd", "path", "mode", "open"})
}

func assertIOErrorResult(t *testing.T, got, definition value.Value) {
	t.Helper()
	result := requireBuiltinInstance(t, got, definition)
	assertBuiltinValue(t, result.Fields["ok"], value.NewBool(false))
	assertBuiltinValue(t, result.Fields["error"], value.NewString("File not open"))
}

func TestIOBuiltinsUseTemporaryFilesAndInvalidateHandles(t *testing.T) {
	machine := New()
	defer func() {
		for descriptor, file := range machine.openFiles {
			_ = file.Close()
			delete(machine.openFiles, descriptor)
		}
	}()
	temporaryRoot := t.TempDir()
	directory := filepath.Join(temporaryRoot, "nested", "data")
	path := filepath.Join(directory, "sample.txt")
	contents := "alpha\nbeta\n"

	assertBuiltinValue(t, callBuiltin(t, machine, "io_mkdir", value.NewString(directory)), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_exists", value.NewString(directory)), value.NewBool(true))

	fileDefinition := testFileDefinition()
	writeHandleValue := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("w"), fileDefinition)
	writeHandle := requireBuiltinInstance(t, writeHandleValue, fileDefinition)
	assertBuiltinValue(t, writeHandle.Fields["open"], value.NewBool(true))
	writeFD := writeHandle.Fields["fd"].AsInt
	if _, ok := machine.openFiles[writeFD]; !ok {
		t.Fatalf("open file descriptor %d is absent from VM state", writeFD)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "io_write", writeHandleValue, value.NewBytes(contents)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "io_close", writeHandleValue), value.NewNull())
	assertBuiltinValue(t, writeHandle.Fields["open"], value.NewBool(false))
	if _, ok := machine.openFiles[writeFD]; ok {
		t.Fatalf("closed file descriptor %d remains in VM state", writeFD)
	}

	readHandleValue := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("r"), fileDefinition)
	readHandle := requireBuiltinInstance(t, readHandleValue, fileDefinition)
	ioResultDefinition := value.NewStruct("IOResult", []string{"ok", "data", "error"})
	ioBytesDefinition := value.NewStruct("IOBytesResult", []string{"ok", "data", "error"})
	ioLinesDefinition := value.NewStruct("IOLinesResult", []string{"ok", "data", "error"})

	textResult := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read", readHandleValue, ioResultDefinition), ioResultDefinition)
	assertBuiltinValue(t, textResult.Fields["ok"], value.NewBool(true))
	assertBuiltinValue(t, textResult.Fields["data"], value.NewString(contents))
	assertBuiltinValue(t, textResult.Fields["error"], value.NewString(""))

	bytesResult := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_bytes", readHandleValue, ioBytesDefinition), ioBytesDefinition)
	assertBuiltinValue(t, bytesResult.Fields["ok"], value.NewBool(true))
	assertBuiltinValue(t, bytesResult.Fields["data"], value.NewBytes(contents))
	assertBuiltinValue(t, bytesResult.Fields["error"], value.NewString(""))

	linesResult := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_lines", readHandleValue, ioLinesDefinition), ioLinesDefinition)
	assertBuiltinValue(t, linesResult.Fields["ok"], value.NewBool(true))
	assertBuiltinArray(t, linesResult.Fields["data"], []value.Value{
		value.NewString("alpha"), value.NewString("beta"), value.NewString(""),
	})
	assertBuiltinValue(t, linesResult.Fields["error"], value.NewString(""))

	fileInfoDefinition := value.NewStruct("FileInfo", []string{"exists", "size", "is_dir"})
	fileInfo := requireBuiltinInstance(t, callBuiltin(t, machine, "io_stat", value.NewString(path), fileInfoDefinition), fileInfoDefinition)
	assertBuiltinValue(t, fileInfo.Fields["exists"], value.NewBool(true))
	assertBuiltinValue(t, fileInfo.Fields["size"], value.NewInt(int64(len(contents))))
	assertBuiltinValue(t, fileInfo.Fields["is_dir"], value.NewBool(false))

	assertBuiltinValue(t, callBuiltin(t, machine, "io_close", readHandleValue), value.NewNull())
	assertBuiltinValue(t, readHandle.Fields["open"], value.NewBool(false))
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read", readHandleValue, ioResultDefinition), ioResultDefinition)
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read_bytes", readHandleValue, ioBytesDefinition), ioBytesDefinition)
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read_lines", readHandleValue, ioLinesDefinition), ioLinesDefinition)

	unknownHandleValue := value.NewInstance(fileDefinition.Obj.(*value.ObjStruct))
	unknownHandle := unknownHandleValue.Obj.(*value.ObjInstance)
	unknownHandle.Fields["fd"] = value.NewInt(987654321)
	unknownHandle.Fields["open"] = value.NewBool(true)
	assertBuiltinValue(t, callBuiltin(t, machine, "io_write", unknownHandleValue, value.NewString("ignored")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "io_close", unknownHandleValue), value.NewNull())
	assertBuiltinValue(t, unknownHandle.Fields["open"], value.NewBool(true))
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read", unknownHandleValue, ioResultDefinition), ioResultDefinition)

	assertBuiltinValue(t, callBuiltin(t, machine, "io_exists", value.NewString(path)), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_remove", value.NewString(path)), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_exists", value.NewString(path)), value.NewBool(false))
	missingInfo := requireBuiltinInstance(t, callBuiltin(t, machine, "io_stat", value.NewString(path), fileInfoDefinition), fileInfoDefinition)
	assertBuiltinValue(t, missingInfo.Fields["exists"], value.NewBool(false))
	assertBuiltinValue(t, missingInfo.Fields["size"], value.NewInt(0))
	assertBuiltinValue(t, missingInfo.Fields["is_dir"], value.NewBool(false))
}
