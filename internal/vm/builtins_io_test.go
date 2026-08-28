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
	assertBuiltinValue(t, result.Field("ok"), value.NewBool(false))
	assertBuiltinValue(t, result.Field("error"), value.NewString("File not open"))
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
	fd := int(requireBuiltinInstance(t, handle, fileType).Field("fd").Int())
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
	instance.MustSet("fd", value.NewInt(int64(handle)))
	instance.MustSet("open", value.NewBool(true))
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
	assertBuiltinValue(t, instance.Field("open"), value.NewBool(false))

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
	assertBuiltinValue(t, success.Field("success"), value.NewBool(true))
	assertBuiltinValue(t, success.Field("bytes_written"), value.NewInt(int64(len([]byte(contents)))))
	assertBuiltinValue(t, success.Field("error"), value.NewString(""))

	fd := int(handle.Field("fd").Int())
	resource, ok := machine.shared.Files.get(fd)
	if !ok {
		t.Fatalf("open file descriptor %d is absent from shared resources", fd)
	}
	if err := resource.file.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}

	failure := requireBuiltinInstance(t, callBuiltin(t, machine, "io_write_result", handleValue, value.NewString("ignored"), resultDefinition), resultDefinition)
	assertBuiltinValue(t, failure.Field("success"), value.NewBool(false))
	assertBuiltinValue(t, failure.Field("bytes_written"), value.NewInt(0))
	if failure.Field("error").String() == "" {
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
	assertBuiltinValue(t, success.Field("success"), value.NewBool(true))
	assertBuiltinValue(t, success.Field("error"), value.NewString(""))
	assertBuiltinValue(t, handle.Field("open"), value.NewBool(false))

	failure := requireBuiltinInstance(t, callBuiltin(t, machine, "io_close_result", handleValue, resultDefinition), resultDefinition)
	assertBuiltinValue(t, failure.Field("success"), value.NewBool(false))
	if failure.Field("error").String() == "" {
		t.Fatal("failed close returned an empty error")
	}

	failedHandleValue := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("a"), fileDefinition)
	failedHandle := requireBuiltinInstance(t, failedHandleValue, fileDefinition)
	failedFD := int(failedHandle.Field("fd").Int())
	failedResource, ok := machine.shared.Files.get(failedFD)
	if !ok {
		t.Fatalf("open file descriptor %d is absent from shared resources", failedFD)
	}
	if err := failedResource.file.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}
	underlyingFailure := requireBuiltinInstance(t, callBuiltin(t, machine, "io_close_result", failedHandleValue, resultDefinition), resultDefinition)
	assertBuiltinValue(t, underlyingFailure.Field("success"), value.NewBool(false))
	if underlyingFailure.Field("error").String() == "" {
		t.Fatal("underlying close failure returned an empty error")
	}
	assertBuiltinValue(t, failedHandle.Field("open"), value.NewBool(false))
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
	assertBuiltinValue(t, writeHandle.Field("open"), value.NewBool(true))
	writeFD := int(writeHandle.Field("fd").Int())
	if _, ok := machine.shared.Files.get(writeFD); !ok {
		t.Fatalf("open file descriptor %d is absent from shared resources", writeFD)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "io_write", writeHandleValue, value.NewBytes(contents)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "io_close", writeHandleValue), value.NewNull())
	assertBuiltinValue(t, writeHandle.Field("open"), value.NewBool(false))
	if _, ok := machine.shared.Files.get(writeFD); ok {
		t.Fatalf("closed file descriptor %d remains in shared resources", writeFD)
	}

	readHandleValue := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("r"), fileDefinition)
	readHandle := requireBuiltinInstance(t, readHandleValue, fileDefinition)
	ioResultDefinition := value.NewStruct("IOResult", []string{"ok", "data", "error"})
	ioBytesDefinition := value.NewStruct("IOBytesResult", []string{"ok", "data", "error"})
	ioLinesDefinition := value.NewStruct("IOLinesResult", []string{"ok", "data", "error"})

	textResult := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read", readHandleValue, ioResultDefinition), ioResultDefinition)
	assertBuiltinValue(t, textResult.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, textResult.Field("data"), value.NewString(contents))
	assertBuiltinValue(t, textResult.Field("error"), value.NewString(""))

	// 0.12.0: as leituras "inteiras" partem do cursor, que io_read deixou no
	// fim — rebobina antes de ler de novo pelo mesmo handle.
	ioPositionDefinition := value.NewStruct("IOPositionResult", []string{"ok", "position", "error"})
	rewound := requireBuiltinInstance(t, callBuiltin(t, machine, "io_seek", readHandleValue, value.NewInt(0), value.NewInt(0), ioPositionDefinition), ioPositionDefinition)
	assertBuiltinValue(t, rewound.Field("position"), value.NewInt(0))
	bytesResult := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_bytes", readHandleValue, ioBytesDefinition), ioBytesDefinition)
	assertBuiltinValue(t, bytesResult.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, bytesResult.Field("data"), value.NewBytes(contents))
	assertBuiltinValue(t, bytesResult.Field("error"), value.NewString(""))

	callBuiltin(t, machine, "io_seek", readHandleValue, value.NewInt(0), value.NewInt(0), ioPositionDefinition)
	linesResult := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_lines", readHandleValue, ioLinesDefinition), ioLinesDefinition)
	assertBuiltinValue(t, linesResult.Field("ok"), value.NewBool(true))
	assertBuiltinArray(t, linesResult.Field("data"), []value.Value{
		value.NewString("alpha"), value.NewString("beta"),
	})
	assertBuiltinValue(t, linesResult.Field("error"), value.NewString(""))

	fileInfoDefinition := value.NewStruct("FileInfo", []string{"exists", "size", "is_dir"})
	fileInfo := requireBuiltinInstance(t, callBuiltin(t, machine, "io_stat", value.NewString(path), fileInfoDefinition), fileInfoDefinition)
	assertBuiltinValue(t, fileInfo.Field("exists"), value.NewBool(true))
	assertBuiltinValue(t, fileInfo.Field("size"), value.NewInt(int64(len(contents))))
	assertBuiltinValue(t, fileInfo.Field("is_dir"), value.NewBool(false))

	assertBuiltinValue(t, callBuiltin(t, machine, "io_close", readHandleValue), value.NewNull())
	assertBuiltinValue(t, readHandle.Field("open"), value.NewBool(false))
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read", readHandleValue, ioResultDefinition), ioResultDefinition)
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read_bytes", readHandleValue, ioBytesDefinition), ioBytesDefinition)
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read_lines", readHandleValue, ioLinesDefinition), ioLinesDefinition)

	unknownHandleValue := value.NewInstance(fileDefinition.Obj.(*value.ObjStruct))
	unknownHandle := unknownHandleValue.Obj.(*value.ObjInstance)
	unknownHandle.MustSet("fd", value.NewInt(987654321))
	unknownHandle.MustSet("open", value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_write", unknownHandleValue, value.NewString("ignored")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "io_close", unknownHandleValue), value.NewNull())
	assertBuiltinValue(t, unknownHandle.Field("open"), value.NewBool(true))
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read", unknownHandleValue, ioResultDefinition), ioResultDefinition)

	assertBuiltinValue(t, callBuiltin(t, machine, "io_exists", value.NewString(path)), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_remove", value.NewString(path)), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "io_exists", value.NewString(path)), value.NewBool(false))
	missingInfo := requireBuiltinInstance(t, callBuiltin(t, machine, "io_stat", value.NewString(path), fileInfoDefinition), fileInfoDefinition)
	assertBuiltinValue(t, missingInfo.Field("exists"), value.NewBool(false))
	assertBuiltinValue(t, missingInfo.Field("size"), value.NewInt(0))
	assertBuiltinValue(t, missingInfo.Field("is_dir"), value.NewBool(false))
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
		assertBuiltinValue(t, result.Field("ok"), value.NewBool(true))
		assertBuiltinValue(t, result.Field("data"), value.NewString(want))
	}
	eof := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, eof.Field("ok"), value.NewBool(false))
	assertBuiltinValue(t, eof.Field("error"), value.NewString("EOF"))
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
	assertBuiltinValue(t, listed.Field("ok"), value.NewBool(true))
	assertBuiltinArray(t, listed.Field("data"), []value.Value{value.NewString("a.txt"), value.NewString("b.txt"), value.NewString("sub")})
	missing := requireBuiltinInstance(t, callBuiltin(t, machine, "io_list_dir", value.NewString(filepath.Join(root, "nope")), linesDef), linesDef)
	assertBuiltinValue(t, missing.Field("ok"), value.NewBool(false))
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
let r: any = io.write_bytes_result(f, b"\x00\xff")
io.write_bytes(f, b"\x01")
io.close(f)
let g: io.File = io.open("`+path+`", "r")
let data: io.IOBytesResult = io.read_bytes(g)
io.close(g)
test_report(to_str(r.value) + "|" + hex_encode(data.data))`)
	if got := reported.Obj.(string); got != "2|00ff01" {
		t.Fatalf("got %q", got)
	}
}

func withStdin(t *testing.T, content string, run func()) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = previous
		_ = reader.Close()
	}()
	go func() {
		_, _ = writer.WriteString(content)
		_ = writer.Close()
	}()
	run()
}

func TestInputReadsEveryLineFromRedirectedStdin(t *testing.T) {
	withStdin(t, "um\ndois\ntres\n", func() {
		reported := captureVMSource(t, `
let a: string = input()
let b: string = input()
let c: string = input()
let d: string = input()
test_report(a + "|" + b + "|" + c + "|" + d)`)
		if got := reported.Obj.(string); got != "um|dois|tres|" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestIOStdinReadLineSignalsEOFAndSharesBufferWithInput(t *testing.T) {
	withStdin(t, "primeira\nsegunda\nterceira", func() {
		reported := captureVMSource(t, `
use io
let first: string = input()
let stdin_file: io.File = io.stdin()
let out: string = first
let r: io.IOResult = io.read_line(stdin_file)
while r.ok do
    out = out + "|" + r.data
    r = io.read_line(stdin_file)
end
test_report(out + "|" + r.error + "|" + to_str(stdin_file.open) + "|" + stdin_file.path)`)
		if got := reported.Obj.(string); got != "primeira|segunda|terceira|EOF|true|<stdin>" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestIOStdinReadReturnsRemainingAndIsReadOnly(t *testing.T) {
	withStdin(t, "x\nresto1\nresto2\n", func() {
		reported := captureVMSource(t, `
use io
let skip: string = input()
let stdin_file: io.File = io.stdin()
let all: io.IOResult = io.read(stdin_file)
let w: any = io.write_result(stdin_file, "nao")
let c: any = io.close_result(stdin_file)
test_report(all.data + "|" + to_str(w.ok) + "|" + w.failure.message + "|" + to_str(c.ok) + "|" + c.failure.message)`)
		if got := reported.Obj.(string); got != "resto1\nresto2\n|false|stdin is read-only|false|stdin cannot be closed" {
			t.Fatalf("got %q", got)
		}
	})
}

// Ler "o arquivo inteiro" (io_read/io_read_bytes/io_read_lines) parte do
// cursor LOGICO (0.12.0: regra unica com stdin) — depois de um read_line le o
// RESTO — e invalida o leitor de linha: o offset do SO termina em EOF e o
// proximo read_line abre leitor novo dali — e ve EOF.
func TestWholeFileReadDiscardsTheLineReader(t *testing.T) {
	machine := New()
	cleanupFileResources(t, machine)
	path := filepath.Join(t.TempDir(), "mixed.txt")
	contents := "um\ndois\ntres\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	handle := callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString("r"), testFileDefinition())
	ioResult := value.NewStruct("IOResult", []string{"ok", "data", "error"})

	first := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, first.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, first.Field("data"), value.NewString("um"))

	whole := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read", handle, ioResult), ioResult)
	assertBuiltinValue(t, whole.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, whole.Field("data"), value.NewString(strings.TrimPrefix(contents, "um\n")))

	eof := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, eof.Field("ok"), value.NewBool(false))
	assertBuiltinValue(t, eof.Field("error"), value.NewString("EOF"))
	callBuiltin(t, machine, "io_close", handle)
}

func TestIOCloseLeavesStdinRegisteredAndUsable(t *testing.T) {
	withStdin(t, "linha\n", func() {
		machine := New()
		fileDefinition := testFileDefinition()
		handleValue := callBuiltin(t, machine, "io_stdin", fileDefinition)
		handle := requireBuiltinInstance(t, handleValue, fileDefinition)
		fd := int(handle.Field("fd").Int())

		callBuiltin(t, machine, "io_close", handleValue)
		if _, ok := machine.shared.Files.get(fd); !ok {
			t.Fatal("io.close removed the stdin resource from the registry")
		}
		assertBuiltinValue(t, handle.Field("open"), value.NewBool(true))

		ioResult := value.NewStruct("IOResult", []string{"ok", "data", "error"})
		line := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handleValue, ioResult), ioResult)
		assertBuiltinValue(t, line.Field("ok"), value.NewBool(true))
		assertBuiltinValue(t, line.Field("data"), value.NewString("linha"))
	})
}

// input() e io.read_line(io.stdin()) leem o MESMO *bufio.Reader; VMs de tasks
// compartilham o SharedState, entao as duas leituras tem de passar pelo mesmo
// operationMu do recurso de stdin. Sem isso -race acusa mutacao concorrente do
// leitor, e uma linha pode se perder ou sair duplicada.
func TestConcurrentInputAndStdinReadLineShareOneBuffer(t *testing.T) {
	const total = 200
	var content strings.Builder
	for index := range total {
		content.WriteString("linha-" + strconv.Itoa(index) + "\n")
	}
	withStdin(t, content.String(), func() {
		machine := New()
		worker := NewWithShared(machine.shared, machine.Config)
		ioResult := value.NewStruct("IOResult", []string{"ok", "data", "error"})
		stdinFile := callBuiltin(t, machine, "io_stdin", testFileDefinition())
		inputNative := requireBuiltin(t, machine, "input")
		readLineNative := requireBuiltin(t, worker, "io_read_line")

		collected := make(chan []string, 2)
		go func() {
			var lines []string
			for {
				got, err := inputNative.Invoke(machine, nil)
				if err != nil {
					t.Errorf("input: %v", err)
					break
				}
				// Nenhuma linha da entrada e vazia: "" so acontece no EOF.
				line, _ := got.Obj.(string)
				if line == "" {
					break
				}
				lines = append(lines, line)
			}
			collected <- lines
		}()
		go func() {
			var lines []string
			for {
				got, err := readLineNative.Invoke(worker, []value.Value{stdinFile, ioResult})
				if err != nil {
					t.Errorf("io_read_line: %v", err)
					break
				}
				result, ok := got.Obj.(*value.ObjInstance)
				if !ok || !result.Field("ok").Bool() {
					break
				}
				line, _ := result.Field("data").Obj.(string)
				lines = append(lines, line)
			}
			collected <- lines
		}()

		seen := map[string]int{}
		read := 0
		for range 2 {
			for _, line := range <-collected {
				seen[line]++
				read++
			}
		}
		if read != total {
			t.Fatalf("as duas goroutines leram %d linhas, want %d", read, total)
		}
		for index := range total {
			if got := seen["linha-"+strconv.Itoa(index)]; got != 1 {
				t.Fatalf("linha-%d foi lida %d vezes, want 1", index, got)
			}
		}
	})
}
