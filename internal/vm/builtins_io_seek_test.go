package vm

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func testIOPositionResultDefinition() value.Value {
	return value.NewStruct("IOPositionResult", []string{"ok", "position", "error"})
}

func testIOBytesResultDefinition() value.Value {
	return value.NewStruct("IOBytesResult", []string{"ok", "data", "error"})
}

func testIOResultDefinition() value.Value {
	return value.NewStruct("IOResult", []string{"ok", "data", "error"})
}

func writeSeekFixture(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func openSeekFixture(t *testing.T, machine *VM, path, mode string) value.Value {
	t.Helper()
	return callBuiltin(t, machine, "io_open", value.NewString(path), value.NewString(mode), testFileDefinition())
}

func assertPositionResult(t *testing.T, got, definition value.Value, position int64) {
	t.Helper()
	result := requireBuiltinInstance(t, got, definition)
	assertBuiltinValue(t, result.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, result.Field("position"), value.NewInt(position))
	assertBuiltinValue(t, result.Field("error"), value.NewString(""))
}

func assertPositionError(t *testing.T, got, definition value.Value, errorText string) {
	t.Helper()
	result := requireBuiltinInstance(t, got, definition)
	assertBuiltinValue(t, result.Field("ok"), value.NewBool(false))
	assertBuiltinValue(t, result.Field("position"), value.NewInt(-1))
	if errorText == "" {
		if result.Field("error").String() == "" {
			t.Fatal("error text is empty")
		}
		return
	}
	assertBuiltinValue(t, result.Field("error"), value.NewString(errorText))
}

func assertBytesResult(t *testing.T, got, definition value.Value, data string) {
	t.Helper()
	result := requireBuiltinInstance(t, got, definition)
	assertBuiltinValue(t, result.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, result.Field("data"), value.NewBytes(data))
	assertBuiltinValue(t, result.Field("error"), value.NewString(""))
}

func assertBytesError(t *testing.T, got, definition value.Value, errorText string) {
	t.Helper()
	result := requireBuiltinInstance(t, got, definition)
	assertBuiltinValue(t, result.Field("ok"), value.NewBool(false))
	assertBuiltinValue(t, result.Field("data"), value.NewBytes(""))
	assertBuiltinValue(t, result.Field("error"), value.NewString(errorText))
}

// seek(SEEK_SET/CUR/END) + read_n: le um trecho do meio do arquivo sem ler o
// arquivo inteiro; read_n devolve menos de n so no fim; EOF explicito quando
// nao ha nada.
func TestIOSeekAndReadNReadFromTheNewPosition(t *testing.T) {
	machine := New()
	path := writeSeekFixture(t, "digits.txt", "0123456789")
	// Depois do TempDir: os cleanups rodam em ordem inversa, e o arquivo tem
	// de ser fechado ANTES de o diretorio ser removido.
	cleanupFileResources(t, machine)
	handle := openSeekFixture(t, machine, path, "r")
	position := testIOPositionResultDefinition()
	bytesResult := testIOBytesResultDefinition()

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(3), value.NewInt(0), position), position, 3)
	assertBytesResult(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(4), bytesResult), bytesResult, "3456")
	assertPositionResult(t, callBuiltin(t, machine, "io_tell", handle, position), position, 7)

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(-2), value.NewInt(1), position), position, 5)
	assertBytesResult(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(2), bytesResult), bytesResult, "56")

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(-3), value.NewInt(2), position), position, 7)
	assertBytesResult(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(10), bytesResult), bytesResult, "789")
	assertBytesError(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(1), bytesResult), bytesResult, "EOF")

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(0), value.NewInt(2), position), position, 10)
	callBuiltin(t, machine, "io_close", handle)
}

func TestIOSeekRejectsInvalidWhenceAndNegativePositionWithoutMoving(t *testing.T) {
	machine := New()
	path := writeSeekFixture(t, "digits.txt", "0123456789")
	// Depois do TempDir: os cleanups rodam em ordem inversa, e o arquivo tem
	// de ser fechado ANTES de o diretorio ser removido.
	cleanupFileResources(t, machine)
	handle := openSeekFixture(t, machine, path, "r")
	position := testIOPositionResultDefinition()

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(4), value.NewInt(0), position), position, 4)
	assertPositionError(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(0), value.NewInt(7), position), position, "invalid whence 7 (use io.SEEK_SET, io.SEEK_CUR or io.SEEK_END)")
	assertPositionError(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(-1), value.NewInt(0), position), position, "")
	assertPositionResult(t, callBuiltin(t, machine, "io_tell", handle, position), position, 4)
	callBuiltin(t, machine, "io_close", handle)
}

func TestIOReadNRejectsNegativeCountAndReadsNothingForZero(t *testing.T) {
	machine := New()
	path := writeSeekFixture(t, "digits.txt", "0123456789")
	// Depois do TempDir: os cleanups rodam em ordem inversa, e o arquivo tem
	// de ser fechado ANTES de o diretorio ser removido.
	cleanupFileResources(t, machine)
	handle := openSeekFixture(t, machine, path, "r")
	bytesResult := testIOBytesResultDefinition()
	position := testIOPositionResultDefinition()

	assertBytesError(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(-1), bytesResult), bytesResult, "read_n: n must be >= 0, got -1")
	assertBytesResult(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(0), bytesResult), bytesResult, "")
	assertPositionResult(t, callBuiltin(t, machine, "io_tell", handle, position), position, 0)
	callBuiltin(t, machine, "io_close", handle)
}

// tell reporta a posicao LOGICA (fisica menos o que o leitor de linha ainda
// tem no buffer); seek descarta esse buffer, e o read_line seguinte le a
// partir da nova posicao.
func TestIOTellIsLogicalAndSeekResetsTheLineReader(t *testing.T) {
	machine := New()
	path := writeSeekFixture(t, "lines.txt", "um\ndois\ntres\n")
	cleanupFileResources(t, machine)
	handle := openSeekFixture(t, machine, path, "r")
	position := testIOPositionResultDefinition()
	ioResult := testIOResultDefinition()

	first := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, first.Field("data"), value.NewString("um"))
	assertPositionResult(t, callBuiltin(t, machine, "io_tell", handle, position), position, 3)

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(0), value.NewInt(0), position), position, 0)
	again := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, again.Field("data"), value.NewString("um"))
	second := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, second.Field("data"), value.NewString("dois"))
	assertPositionResult(t, callBuiltin(t, machine, "io_tell", handle, position), position, 8)

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(8), value.NewInt(0), position), position, 8)
	third := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, third.Field("data"), value.NewString("tres"))
	callBuiltin(t, machine, "io_close", handle)
}

// Em "rw" a escrita cai na posicao LOGICA do cursor e nao trunca: um trecho
// do meio e sobrescrito e o resto do arquivo fica intacto; apos read_line a
// escrita re-sincroniza o offset do SO com a posicao logica.
func TestIOWriteInReadWriteModeWritesAtTheCursorWithoutTruncating(t *testing.T) {
	machine := New()
	cleanupFileResources(t, machine)
	position := testIOPositionResultDefinition()
	writeResult := testIOWriteResultDefinition()
	ioResult := testIOResultDefinition()

	t.Run("after seek", func(t *testing.T) {
		path := writeSeekFixture(t, "digits.txt", "0123456789")
		handle := openSeekFixture(t, machine, path, "rw")
		assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(4), value.NewInt(0), position), position, 4)
		callBuiltin(t, machine, "io_write", handle, value.NewString("AB"))
		assertPositionResult(t, callBuiltin(t, machine, "io_tell", handle, position), position, 6)
		assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(0), value.NewInt(2), position), position, 10)
		written := requireBuiltinInstance(t, callBuiltin(t, machine, "io_write_result", handle, value.NewBytes("Z"), writeResult), writeResult)
		assertBuiltinValue(t, written.Field("bytes_written"), value.NewInt(1))
		callBuiltin(t, machine, "io_close", handle)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "0123AB6789Z" {
			t.Fatalf("content = %q, want %q", content, "0123AB6789Z")
		}
	})

	t.Run("after read_line", func(t *testing.T) {
		path := writeSeekFixture(t, "lines.txt", "ab\ncd\nef\n")
		handle := openSeekFixture(t, machine, path, "rw")
		line := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
		assertBuiltinValue(t, line.Field("data"), value.NewString("ab"))
		callBuiltin(t, machine, "io_write", handle, value.NewString("XY"))
		callBuiltin(t, machine, "io_close", handle)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "ab\nXY\nef\n" {
			t.Fatalf("content = %q, want %q", content, "ab\nXY\nef\n")
		}
	})
}

// BREAKING (0.12.0): as leituras de arquivo inteiro (read/read_bytes/
// read_lines) partem do cursor LOGICO — regra unica com stdin. Handle novo
// continua lendo o arquivo inteiro; depois de read_line/seek leem o resto; um
// segundo read devolve "" com ok=true.
func TestIOWholeFileReadsStartAtTheLogicalCursor(t *testing.T) {
	machine := New()
	path := writeSeekFixture(t, "lines.txt", "um\ndois\ntres\n")
	cleanupFileResources(t, machine)
	handle := openSeekFixture(t, machine, path, "r")
	position := testIOPositionResultDefinition()
	ioResult := testIOResultDefinition()
	bytesResult := testIOBytesResultDefinition()
	linesResult := value.NewStruct("IOLinesResult", []string{"ok", "data", "error"})

	line := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, line.Field("data"), value.NewString("um"))
	rest := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read", handle, ioResult), ioResult)
	assertBuiltinValue(t, rest.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, rest.Field("data"), value.NewString("dois\ntres\n"))
	empty := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read", handle, ioResult), ioResult)
	assertBuiltinValue(t, empty.Field("ok"), value.NewBool(true))
	assertBuiltinValue(t, empty.Field("data"), value.NewString(""))
	eof := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_line", handle, ioResult), ioResult)
	assertBuiltinValue(t, eof.Field("error"), value.NewString("EOF"))

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(0), value.NewInt(0), position), position, 0)
	lines := requireBuiltinInstance(t, callBuiltin(t, machine, "io_read_lines", handle, linesResult), linesResult)
	if got := lines.Field("data").Obj.(*value.ObjArray).Elements; len(got) != 3 {
		t.Fatalf("read_lines after seek(0) = %d lines, want 3", len(got))
	}

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(3), value.NewInt(0), position), position, 3)
	assertBytesResult(t, callBuiltin(t, machine, "io_read_bytes", handle, bytesResult), bytesResult, "dois\ntres\n")
	assertPositionResult(t, callBuiltin(t, machine, "io_tell", handle, position), position, 13)
	callBuiltin(t, machine, "io_close", handle)
}

func TestIOSeekAndTellRefuseStdinButReadNWorks(t *testing.T) {
	withStdin(t, "abcdef\n", func() {
		machine := New()
		handle := callBuiltin(t, machine, "io_stdin", testFileDefinition())
		position := testIOPositionResultDefinition()
		bytesResult := testIOBytesResultDefinition()
		assertPositionError(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(0), value.NewInt(0), position), position, "stdin is not seekable")
		assertPositionError(t, callBuiltin(t, machine, "io_tell", handle, position), position, "stdin is not seekable")
		assertBytesResult(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(3), bytesResult), bytesResult, "abc")
		assertBytesResult(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(10), bytesResult), bytesResult, "def\n")
		assertBytesError(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(1), bytesResult), bytesResult, "EOF")
	})
}

func TestIOSeekTellReadNReportClosedFile(t *testing.T) {
	machine := New()
	path := writeSeekFixture(t, "digits.txt", "0123456789")
	// Depois do TempDir: os cleanups rodam em ordem inversa, e o arquivo tem
	// de ser fechado ANTES de o diretorio ser removido.
	cleanupFileResources(t, machine)
	handle := openSeekFixture(t, machine, path, "r")
	position := testIOPositionResultDefinition()
	bytesResult := testIOBytesResultDefinition()
	callBuiltin(t, machine, "io_close", handle)

	assertPositionError(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(0), value.NewInt(0), position), position, "File not open")
	assertPositionError(t, callBuiltin(t, machine, "io_tell", handle, position), position, "File not open")
	assertIOErrorResult(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(1), bytesResult), bytesResult)
}

// Ponta a ponta pelos wrappers de io.nx: constantes SEEK_*, seek/tell com
// IOPositionResult e read_n — o `get(fd, pos, buf, n)` do K&R 8.4.
func TestIOSeekWrappersEndToEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.txt")
	got := captureVMSource(t, `use io

func get(f: io.File, pos: int, n: int) -> bytes
    if io.seek(f, pos, io.SEEK_SET).ok then
        return io.read_n(f, n).data
    end
    return b""
end

let w: io.File = io.open(`+strconv.Quote(path)+`, "w")
io.write(w, "0123456789")
io.close(w)
let f: io.File = io.open(`+strconv.Quote(path)+`, "r")
let size: int = io.seek(f, 0, io.SEEK_END).position
let meio: string = to_str(get(f, 4, 3))
let back: io.IOPositionResult = io.seek(f, -2, io.SEEK_CUR)
let fim: string = to_str(io.read_n(f, 100).data)
let pos: io.IOPositionResult = io.tell(f)
let bad: io.IOPositionResult = io.seek(f, 0, 9)
io.close(f)
test_report(to_str(size) + "|" + meio + "|" + to_str(back.position) + "|" + fim + "|" + to_str(pos.position) + "|" + to_str(bad.ok) + "|" + bad.error)`)
	want := "10|456|5|56789|10|false|invalid whence 9 (use io.SEEK_SET, io.SEEK_CUR or io.SEEK_END)"
	if got.Type != value.VAL_OBJ || got.Obj.(string) != want {
		t.Fatalf("reported %q, want %q", got.String(), want)
	}
}

// seek alem do fim e permitido: uma leitura ali reporta EOF e uma escrita
// estende o arquivo (o SO preenche o buraco com zeros).
func TestIOSeekPastEndReadsEOFAndWriteExtendsTheFile(t *testing.T) {
	machine := New()
	path := writeSeekFixture(t, "short.txt", "abc")
	cleanupFileResources(t, machine)
	handle := openSeekFixture(t, machine, path, "rw")
	position := testIOPositionResultDefinition()
	bytesResult := testIOBytesResultDefinition()

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(5), value.NewInt(0), position), position, 5)
	assertBytesError(t, callBuiltin(t, machine, "io_read_n", handle, value.NewInt(1), bytesResult), bytesResult, "EOF")
	callBuiltin(t, machine, "io_write", handle, value.NewString("Z"))
	assertPositionResult(t, callBuiltin(t, machine, "io_tell", handle, position), position, 6)
	callBuiltin(t, machine, "io_close", handle)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "abc\x00\x00Z" {
		t.Fatalf("content = %q, want %q", content, "abc\x00\x00Z")
	}
}

// Em "a" o SO anexa sempre no fim, independentemente de seek; tell reporta
// o fim depois da escrita.
func TestIOAppendModeWritesAtTheEndRegardlessOfSeek(t *testing.T) {
	machine := New()
	path := writeSeekFixture(t, "log.txt", "abc")
	cleanupFileResources(t, machine)
	handle := openSeekFixture(t, machine, path, "a")
	position := testIOPositionResultDefinition()

	assertPositionResult(t, callBuiltin(t, machine, "io_seek", handle, value.NewInt(0), value.NewInt(0), position), position, 0)
	callBuiltin(t, machine, "io_write", handle, value.NewString("XY"))
	assertPositionResult(t, callBuiltin(t, machine, "io_tell", handle, position), position, 5)
	callBuiltin(t, machine, "io_close", handle)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "abcXY" {
		t.Fatalf("content = %q, want %q", content, "abcXY")
	}
}
