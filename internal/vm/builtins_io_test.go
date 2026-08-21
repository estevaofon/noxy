package vm

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func recordDeferredFileResource(machine *VM, captured **FileResource) {
	machine.DefineNative("record_file_resource", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		instance, ok := args[0].Obj.(*value.ObjInstance)
		if !ok {
			return value.NewNull()
		}
		*captured, _ = machine.shared.Files.get(fileHandle(machine.shared, instance))
		return value.NewNull()
	})
}

func requireDeferredFileClosed(t *testing.T, machine *VM, resource *FileResource, path string) {
	t.Helper()
	if got := len(machine.shared.Files.snapshot()); got != 0 {
		t.Fatalf("files=%d, want 0", got)
	}
	if resource == nil {
		t.Fatal("file resource was not recorded")
	}
	resource.stateMu.Lock()
	closed := resource.closed
	resource.stateMu.Unlock()
	if !closed {
		t.Fatal("file resource remains open")
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove deferred file: %v", err)
	}
}

func TestDeferredFileCleanupClosesResourceOnEveryExit(t *testing.T) {
	tests := []struct {
		name      string
		suffix    string
		wantError bool
	}{
		{"normal", "", false},
		{"runtime error", "\nlet zero: int = 0\nprint(1 / zero)", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			machine := New()
			cleanupFileResources(t, machine)
			path := filepath.Join(t.TempDir(), "deferred.txt")
			var resource *FileResource
			recordDeferredFileResource(machine, &resource)

			source := "use io\nlet file: io.File = io.open(" + strconv.Quote(path) + ", \"w\")\n" +
				"record_file_resource(file)\ndefer io.close(file)" + test.suffix
			err := interpretVMSource(t, machine, source)
			if (err != nil) != test.wantError {
				t.Fatalf("error=%v, wantError=%v", err, test.wantError)
			}
			requireDeferredFileClosed(t, machine, resource, path)
		})
	}
}

func TestDeferredFileResourceCleanupContinuesAfterFailure(t *testing.T) {
	machine := New()
	cleanupFileResources(t, machine)
	path := filepath.Join(t.TempDir(), "deferred-failure.txt")
	var resource *FileResource
	recordDeferredFileResource(machine, &resource)
	sentinel := errors.New("sentinel cleanup failure")
	defineCleanupFailureNative(machine, sentinel)

	err := interpretVMSource(t, machine, "use io\nlet file: io.File = io.open("+strconv.Quote(path)+", \"w\")\n"+
		"record_file_resource(file)\ndefer io.close(file)\ndefer cleanup_fail()")
	if !errors.Is(err, sentinel) {
		t.Fatalf("error=%v, want sentinel cleanup failure", err)
	}
	requireDeferredFileClosed(t, machine, resource, path)
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
		value.NewString("alpha"), value.NewString("beta"),
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

func TestIOReadRejectsInvalidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dirty.bin")
	if err := os.WriteFile(path, []byte("hello\xffworld"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "use io\nlet file: io.File = io.open(" + strconv.Quote(path) + ", \"r\")\n" +
		"let r: io.IOResult = io.read(file)\n" +
		"io.close(file)\n" +
		"test_report(to_str(!r.ok) + \"|\" + r.error)"
	captured := captureVMSource(t, source)
	report, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	if !strings.HasPrefix(report, "true|") {
		t.Fatalf("io.read on invalid UTF-8 reported %q, want ok=false", report)
	}
	if !strings.Contains(report, "UTF-8") {
		t.Fatalf("error = %q, want it to mention UTF-8", report)
	}
}

func TestIOReadBytesStillReadsInvalidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dirty.bin")
	if err := os.WriteFile(path, []byte("hello\xffworld"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "use io\nlet file: io.File = io.open(" + strconv.Quote(path) + ", \"r\")\n" +
		"let r: io.IOBytesResult = io.read_bytes(file)\n" +
		"io.close(file)\n" +
		"test_report(to_str(r.ok) + \"|\" + to_str(length(r.data)))"
	captured := captureVMSource(t, source)
	report, _ := captured.Obj.(string)
	if report != "true|11" {
		t.Fatalf("io.read_bytes reported %q, want %q — the raw escape hatch must still work", report, "true|11")
	}
}

func TestIOReadLinesRejectsInvalidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dirty.txt")
	if err := os.WriteFile(path, []byte("linha um\nlinha \xff dois\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "use io\nlet file: io.File = io.open(" + strconv.Quote(path) + ", \"r\")\n" +
		"let r: io.IOLinesResult = io.read_lines(file)\n" +
		"io.close(file)\n" +
		"test_report(to_str(!r.ok) + \"|\" + r.error)"
	captured := captureVMSource(t, source)
	report, _ := captured.Obj.(string)
	if !strings.HasPrefix(report, "true|") || !strings.Contains(report, "UTF-8") {
		t.Fatalf("io.read_lines reported %q, want ok=false with a UTF-8 message", report)
	}
}

func TestIOReadAcceptsValidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean.txt")
	if err := os.WriteFile(path, []byte("acentuação e emoji \U0001F600"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := "use io\nlet file: io.File = io.open(" + strconv.Quote(path) + ", \"r\")\n" +
		"let r: io.IOResult = io.read(file)\n" +
		"io.close(file)\n" +
		"test_report(r.data)"
	captured := captureVMSource(t, source)
	if text, _ := captured.Obj.(string); text != "acentuação e emoji \U0001F600" {
		t.Fatalf("io.read returned %q, want the file content unchanged", text)
	}
}

func TestIOReadLineIsIncrementalWithExplicitEOF(t *testing.T) {
	machine := New()
	cleanupFileResources(t, machine)
	path := filepath.Join(t.TempDir(), "lines.txt")
	if err := os.WriteFile(path, []byte("um\r\ndois\n\ntres"), 0o600); err != nil {
		t.Fatal(err)
	}
	handle := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("r"), testFileDefinition())
	ioResult := value.NewStruct("IOResult", []string{"ok", "data", "error"})
	for _, want := range []string{"um", "dois", "", "tres"} {
		result := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
		assertBuiltinValue(t, result.Fields["ok"], value.NewBool(true))
		assertBuiltinValue(t, result.Fields["data"], value.NewString(want))
	}
	eof := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, eof.Fields["ok"], value.NewBool(false))
	assertBuiltinValue(t, eof.Fields["error"], value.NewString("EOF"))
	callBuiltin(t, machine, "io_close", handle)
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
}

func TestIOListDirAndRename(t *testing.T) {
	machine := New()
	root := t.TempDir()
	for _, name := range []string{"b.txt", "a.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	linesDef := value.NewStruct("IOLinesResult", []string{"ok", "data", "error"})
	listed := requireBuiltinInstance(t, callBuiltin(t, machine, "io_list_dir", value.NewString(root), linesDef), linesDef)
	assertBuiltinValue(t, listed.Fields["ok"], value.NewBool(true))
	assertBuiltinArray(t, listed.Fields["data"], []value.Value{value.NewString("a.txt"), value.NewString("b.txt"), value.NewString("sub")})
	missing := requireBuiltinInstance(t, callBuiltin(t, machine, "io_list_dir", value.NewString(filepath.Join(root, "nope")), linesDef), linesDef)
	assertBuiltinValue(t, missing.Fields["ok"], value.NewBool(false))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_rename", value.NewString(filepath.Join(root, "a.txt")), value.NewString(filepath.Join(root, "c.txt"))), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_exists", value.NewString(filepath.Join(root, "c.txt"))), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_rename", value.NewString(filepath.Join(root, "nope.txt")), value.NewString(filepath.Join(root, "d.txt"))), value.NewBool(false))
}

func TestSplitLinesDropsOnlyTheTrailingEmptyLine(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a\nb\n", []string{"a", "b"}},
		{"a\nb", []string{"a", "b"}},
		{"\n", []string{""}},
		{"", []string{}},
		{"a\n\n", []string{"a", ""}},
	}
	for _, tc := range cases {
		got := splitLines(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("splitLines(%q) = %q, want %q", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("splitLines(%q) = %q, want %q", tc.in, got, tc.want)
			}
		}
	}
}

func TestIOWriteBytesWrappersRoundTrip(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "bin.dat"))
	reported := captureVMSource(t, `
use io
let f: io.File = io.open("`+path+`", "w")
let r: io.IOWriteResult = io.write_bytes_result(f, b"\x00\xff")
io.write_bytes(f, b"\x01")
io.close(f)
let g: io.File = io.open("`+path+`", "r")
let data: io.IOBytesResult = io.read_bytes(g)
io.close(g)
test_report(to_str(r.bytes_written) + "|" + hex_encode(data.data))`)
	if got := reported.Obj.(string); got != "2|00ff01" {
		t.Fatalf("got %q", got)
	}
}
