package vm

import (
	"bufio"
	"errors"
	"noxy-vm/internal/chunk"
	"strings"
	"testing"
)

type fakeTerminalDriver struct {
	terminal bool
	makeErr  error
	closeErr error
	made     int
	restored int
}

func (f *fakeTerminalDriver) isTerminal(int) bool { return f.terminal }
func (f *fakeTerminalDriver) makeRaw(int) (*terminalSnapshot, error) {
	f.made++
	if f.makeErr != nil {
		return nil, f.makeErr
	}
	return &terminalSnapshot{}, nil
}
func (f *fakeTerminalDriver) restore(int, *terminalSnapshot) error {
	f.restored++
	return f.closeErr
}

func newTestTerminalRuntime(driver terminalDriver, input string) *terminalRuntime {
	return &terminalRuntime{
		driver: driver,
		input:  bufio.NewReader(strings.NewReader(input)),
		fd:     42,
	}
}

func TestTerminalOpenRejectsNonTerminal(t *testing.T) {
	driver := &fakeTerminalDriver{}
	runtime := newTestTerminalRuntime(driver, "")

	if err := runtime.openRaw(); err == nil {
		t.Fatal("openRaw() error = nil, want an error for a non-terminal input")
	}
	if driver.made != 0 {
		t.Errorf("makeRaw calls = %d, want 0", driver.made)
	}
}

func TestTerminalOpenAndCloseAreIdempotent(t *testing.T) {
	driver := &fakeTerminalDriver{terminal: true}
	runtime := newTestTerminalRuntime(driver, "")

	if err := runtime.openRaw(); err != nil {
		t.Fatalf("first openRaw() error = %v", err)
	}
	if err := runtime.openRaw(); err != nil {
		t.Fatalf("second openRaw() error = %v", err)
	}
	if driver.made != 1 {
		t.Errorf("makeRaw calls = %d, want 1", driver.made)
	}

	if err := runtime.close(); err != nil {
		t.Fatalf("first close() error = %v", err)
	}
	if err := runtime.close(); err != nil {
		t.Fatalf("second close() error = %v", err)
	}
	if driver.restored != 1 {
		t.Errorf("restore calls = %d, want 1", driver.restored)
	}
}

func TestTerminalCloseReportsRestoreFailure(t *testing.T) {
	driver := &fakeTerminalDriver{terminal: true, closeErr: errors.New("restore failed")}
	runtime := newTestTerminalRuntime(driver, "")

	if err := runtime.openRaw(); err != nil {
		t.Fatalf("openRaw() error = %v", err)
	}
	if err := runtime.close(); !errors.Is(err, driver.closeErr) {
		t.Fatalf("close() error = %v, want %v", err, driver.closeErr)
	}
}

func TestTerminalRuntimeNormalizesKeys(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "uppercase", input: "A", want: "a"},
		{name: "space", input: " ", want: "space"},
		{name: "carriage return", input: "\r", want: "enter"},
		{name: "line feed", input: "\n", want: "enter"},
		{name: "control c", input: "\x03", want: "ctrl+c"},
		{name: "unicode", input: "é", want: "é"},
		{name: "unknown control", input: "\x01", want: "unknown:0x01"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := newTestTerminalRuntime(&fakeTerminalDriver{terminal: true}, test.input)
			if err := runtime.openRaw(); err != nil {
				t.Fatalf("openRaw() error = %v", err)
			}

			got, err := runtime.readKey()
			if err != nil {
				t.Fatalf("readKey() error = %v", err)
			}
			if got != test.want {
				t.Errorf("readKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTerminalReadRequiresRawMode(t *testing.T) {
	runtime := newTestTerminalRuntime(&fakeTerminalDriver{terminal: true}, "A")

	if _, err := runtime.readKey(); err == nil {
		t.Fatal("readKey() error = nil, want an error outside raw mode")
	}
}

func TestSpawnedVMsShareTerminalRuntime(t *testing.T) {
	parent := NewWithConfig(VMConfig{RootPath: "."})
	child := NewWithShared(parent.shared, parent.Config)

	if parent.shared.Terminal == nil {
		t.Fatal("parent shared terminal = nil")
	}
	if child.shared.Terminal != parent.shared.Terminal {
		t.Fatal("spawned VM does not retain the parent terminal runtime")
	}
}

func newVMWithActiveTestTerminal(t *testing.T, closeErr error) (*VM, *fakeTerminalDriver) {
	t.Helper()
	driver := &fakeTerminalDriver{terminal: true, closeErr: closeErr}
	machine := New()
	machine.shared.Terminal = newTestTerminalRuntime(driver, "")
	if err := machine.shared.Terminal.openRaw(); err != nil {
		t.Fatalf("openRaw() error = %v", err)
	}
	return machine, driver
}

func TestInterpretRestoresTerminalAfterSuccess(t *testing.T) {
	machine, driver := newVMWithActiveTestTerminal(t, nil)

	err := interpretVMSource(t, machine, "let value: int = 1")

	if err != nil {
		t.Fatalf("Interpret() error = %v", err)
	}
	if driver.restored != 1 {
		t.Errorf("restore calls = %d, want 1", driver.restored)
	}
}

func TestInterpretRestoresTerminalAfterRuntimeError(t *testing.T) {
	machine, driver := newVMWithActiveTestTerminal(t, nil)

	err := interpretVMSource(t, machine, "1 / 0")

	if err == nil {
		t.Fatal("Interpret() error = nil, want runtime error")
	}
	if !strings.Contains(err.Error(), "division by zero") {
		t.Errorf("Interpret() error = %v, want division by zero", err)
	}
	if driver.restored != 1 {
		t.Errorf("restore calls = %d, want 1", driver.restored)
	}
}

func TestInterpretRestoresTerminalAfterPanic(t *testing.T) {
	machine, driver := newVMWithActiveTestTerminal(t, nil)

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("Interpret() did not panic for a truncated OP_CONSTANT")
		}
		if driver.restored != 1 {
			t.Errorf("restore calls = %d, want 1", driver.restored)
		}
	}()

	_ = machine.Interpret(&chunk.Chunk{Code: []byte{byte(chunk.OP_CONSTANT)}})
}
