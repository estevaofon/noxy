package vm

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func testIOWriteResultDefinition() value.Value {
	return value.NewStruct("IOWriteResult", []string{"success", "bytes_written", "error"})
}

func testIOCloseResultDefinition() value.Value {
	return value.NewStruct("IOCloseResult", []string{"success", "error"})
}

func cleanupFileResources(t *testing.T, machine *VM) {
	t.Helper()
	t.Cleanup(func() {
		for handle := range machine.shared.Files.snapshot() {
			if resource, ok := machine.shared.Files.remove(handle); ok {
				_ = resource.close()
			}
		}
	})
}

func TestFileHandleIsUsableAcrossSharedVMs(t *testing.T) {
	parent := New()
	child := NewWithShared(parent.shared, parent.Config)
	path := filepath.Join(t.TempDir(), "shared.txt")
	fileType := testFileDefinition()
	handle := callBuiltin(t, parent, "io_open", value.NewString(path), value.NewString("w"), fileType)
	defer callBuiltin(t, parent, "io_close", handle)
	fd := int(requireBuiltinInstance(t, handle, fileType).Fields["fd"].AsInt)
	if _, ok := parent.shared.Files.get(fd); !ok {
		t.Fatalf("file handle %d was not published to shared resources", fd)
	}
	callBuiltin(t, child, "io_write", handle, value.NewString("shared"))
	callBuiltin(t, child, "io_close", handle)
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "shared" {
		t.Fatalf("contents=%q err=%v", contents, err)
	}
}

func TestIOHandleMetadataIsSafeAcrossSharedVMs(t *testing.T) {
	parent := New()
	child := NewWithShared(parent.shared, parent.Config)
	path := filepath.Join(t.TempDir(), "metadata-race.txt")
	fileType := testFileDefinition()
	handle := callBuiltin(t, parent, "io_open", value.NewString(path), value.NewString("w"), fileType)
	defer callBuiltin(t, parent, "io_close", handle)

	writeNative := requireBuiltin(t, child, "io_write")
	closeNative := requireBuiltin(t, parent, "io_close")
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		<-start
		_, err := writeNative.Invoke(child, []value.Value{handle, value.NewString("")})
		done <- err
	}()
	go func() {
		<-start
		_, err := closeNative.Invoke(parent, []value.Value{handle})
		done <- err
	}()
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileCloseInterruptsBlockedReadWithoutRegistryDeadlock(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	machine := New()
	resource := &FileResource{file: reader}
	handle := machine.shared.Files.add(resource)
	fileType := testFileDefinition()
	handleValue := value.NewInstance(fileType.Obj.(*value.ObjStruct))
	instance := handleValue.Obj.(*value.ObjInstance)
	instance.Fields["fd"] = value.NewInt(int64(handle))
	instance.Fields["open"] = value.NewBool(true)
	closeNative := requireBuiltin(t, machine, "io_close")
	readStarted := make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		resource.operationMu.Lock()
		defer resource.operationMu.Unlock()
		close(readStarted)
		buffer := make([]byte, 1)
		_, readErr := resource.file.Read(buffer)
		readDone <- readErr
	}()
	<-readStarted

	closeDone := make(chan error, 1)
	go func() {
		_, closeErr := closeNative.Invoke(machine, []value.Value{handleValue})
		closeDone <- closeErr
	}()
	select {
	case closeErr := <-closeDone:
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	case <-time.After(statefulBuiltinTimeout):
		t.Fatal("close waited for the active file operation")
	}
	if _, ok := machine.shared.Files.get(handle); ok {
		t.Fatal("closed file handle remains in shared resources")
	}
	assertBuiltinValue(t, instance.Fields["open"], value.NewBool(false))

	select {
	case readErr := <-readDone:
		if readErr == nil {
			t.Fatal("blocked read succeeded after close")
		}
	case <-time.After(statefulBuiltinTimeout):
		t.Fatal("close did not interrupt blocked read")
	}
}

func TestIOWriteResultReportsSuccessAndFailure(t *testing.T) {
	machine := New()
	cleanupFileResources(t, machine)

	path := filepath.Join(t.TempDir(), "observable-write.txt")
	fileDefinition := testFileDefinition()
	handleValue := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("w"), fileDefinition)
	handle := requireBuiltinInstance(t, handleValue, fileDefinition)
	resultDefinition := testIOWriteResultDefinition()
	contents := "Noxy 🐍"

	success := requireBuiltinInstance(t, callBuiltin(t, machine, "io_write_result", handleValue, value.NewString(contents), resultDefinition), resultDefinition)
	assertBuiltinValue(t, success.Fields["success"], value.NewBool(true))
	assertBuiltinValue(t, success.Fields["bytes_written"], value.NewInt(int64(len([]byte(contents)))))
	assertBuiltinValue(t, success.Fields["error"], value.NewString(""))

	fd := int(handle.Fields["fd"].AsInt)
	resource, ok := machine.shared.Files.get(fd)
	if !ok {
		t.Fatalf("open file descriptor %d is absent from shared resources", fd)
	}
	if err := resource.file.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}

	failure := requireBuiltinInstance(t, callBuiltin(t, machine, "io_write_result", handleValue, value.NewString("ignored"), resultDefinition), resultDefinition)
	assertBuiltinValue(t, failure.Fields["success"], value.NewBool(false))
	assertBuiltinValue(t, failure.Fields["bytes_written"], value.NewInt(0))
	if failure.Fields["error"].String() == "" {
		t.Fatal("failed write returned an empty error")
	}
}

func TestIOCloseResultReportsSuccessAndFailure(t *testing.T) {
	machine := New()
	cleanupFileResources(t, machine)

	path := filepath.Join(t.TempDir(), "observable-close.txt")
	fileDefinition := testFileDefinition()
	handleValue := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("w"), fileDefinition)
	handle := requireBuiltinInstance(t, handleValue, fileDefinition)
	resultDefinition := testIOCloseResultDefinition()

	success := requireBuiltinInstance(t, callBuiltin(t, machine, "io_close_result", handleValue, resultDefinition), resultDefinition)
	assertBuiltinValue(t, success.Fields["success"], value.NewBool(true))
	assertBuiltinValue(t, success.Fields["error"], value.NewString(""))
	assertBuiltinValue(t, handle.Fields["open"], value.NewBool(false))

	failure := requireBuiltinInstance(t, callBuiltin(t, machine, "io_close_result", handleValue, resultDefinition), resultDefinition)
	assertBuiltinValue(t, failure.Fields["success"], value.NewBool(false))
	if failure.Fields["error"].String() == "" {
		t.Fatal("failed close returned an empty error")
	}

	failedHandleValue := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("a"), fileDefinition)
	failedHandle := requireBuiltinInstance(t, failedHandleValue, fileDefinition)
	failedFD := int(failedHandle.Fields["fd"].AsInt)
	failedResource, ok := machine.shared.Files.get(failedFD)
	if !ok {
		t.Fatalf("open file descriptor %d is absent from shared resources", failedFD)
	}
	if err := failedResource.file.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}
	underlyingFailure := requireBuiltinInstance(t, callBuiltin(t, machine, "io_close_result", failedHandleValue, resultDefinition), resultDefinition)
	assertBuiltinValue(t, underlyingFailure.Fields["success"], value.NewBool(false))
	if underlyingFailure.Fields["error"].String() == "" {
		t.Fatal("underlying close failure returned an empty error")
	}
	assertBuiltinValue(t, failedHandle.Fields["open"], value.NewBool(false))
	if _, ok := machine.shared.Files.get(failedFD); ok {
		t.Fatalf("failed close descriptor %d remains in shared resources", failedFD)
	}
}

func TestIOBuiltinsUseTemporaryFilesAndInvalidateHandles(t *testing.T) {
	machine := New()
	cleanupFileResources(t, machine)
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
	writeFD := int(writeHandle.Fields["fd"].AsInt)
	if _, ok := machine.shared.Files.get(writeFD); !ok {
		t.Fatalf("open file descriptor %d is absent from shared resources", writeFD)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "io_write", writeHandleValue, value.NewBytes(contents)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "io_close", writeHandleValue), value.NewNull())
	assertBuiltinValue(t, writeHandle.Fields["open"], value.NewBool(false))
	if _, ok := machine.shared.Files.get(writeFD); ok {
		t.Fatalf("closed file descriptor %d remains in shared resources", writeFD)
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
