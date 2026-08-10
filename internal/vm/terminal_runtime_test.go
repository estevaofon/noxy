package vm

import (
	"bufio"
	"errors"
	"io"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"
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
	if !runtime.raw {
		t.Error("runtime.raw = false after failed restore, want true")
	}
	if runtime.saved == nil {
		t.Error("runtime.saved = nil after failed restore, want saved terminal state")
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

type gatedTerminalReader struct {
	firstReadStarted chan struct{}
	firstReadRelease chan struct{}
	releaseOnce      sync.Once
	mu               sync.Mutex
	reads            int
}

func newGatedTerminalReader() *gatedTerminalReader {
	return &gatedTerminalReader{
		firstReadStarted: make(chan struct{}),
		firstReadRelease: make(chan struct{}),
	}
}

func (reader *gatedTerminalReader) Read(buffer []byte) (int, error) {
	reader.mu.Lock()
	readNumber := reader.reads
	reader.reads++
	reader.mu.Unlock()

	if readNumber == 0 {
		close(reader.firstReadStarted)
		<-reader.firstReadRelease
		buffer[0] = 'A'
		return 1, nil
	}
	if readNumber == 1 {
		buffer[0] = 'B'
		return 1, nil
	}
	return 0, io.EOF
}

func (reader *gatedTerminalReader) releaseFirstRead() {
	reader.releaseOnce.Do(func() {
		close(reader.firstReadRelease)
	})
}

func (reader *gatedTerminalReader) readCount() int {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.reads
}

type terminalReadResult struct {
	key string
	err error
}

func runQueuedTerminalRead(runtime *terminalRuntime, started chan<- struct{}, result chan<- terminalReadResult) {
	close(started)
	key, err := runtime.readKey()
	result <- terminalReadResult{key: key, err: err}
}

func waitForQueuedTerminalRead(t *testing.T) {
	t.Helper()
	// Observe the second goroutine parked in readMu so close cannot race with
	// whether the pre-fix implementation has completed its early raw check.
	deadline := time.Now().Add(2 * time.Second)
	stackBuffer := make([]byte, 1<<20)
	for {
		stackLength := goruntime.Stack(stackBuffer, true)
		stacks := string(stackBuffer[:stackLength])
		for _, stack := range strings.Split(stacks, "\n\n") {
			if strings.Contains(stack, "runQueuedTerminalRead") &&
				strings.Contains(stack, "sync.(*Mutex).Lock") &&
				strings.Contains(stack, "terminalRuntime).readKey") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued read did not block on readMu; goroutine stacks:\n%s", stacks)
		}
		goruntime.Gosched()
	}
}

func receiveTerminalRead(t *testing.T, description string, result <-chan terminalReadResult) terminalReadResult {
	t.Helper()
	select {
	case received := <-result:
		return received
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not finish", description)
		return terminalReadResult{}
	}
}

func TestTerminalCloseMakesQueuedReadFailAfterActiveReadFinishes(t *testing.T) {
	driver := &fakeTerminalDriver{terminal: true}
	reader := newGatedTerminalReader()
	runtime := &terminalRuntime{
		driver: driver,
		input:  bufio.NewReader(reader),
		fd:     42,
	}
	if err := runtime.openRaw(); err != nil {
		t.Fatalf("openRaw() error = %v", err)
	}
	t.Cleanup(reader.releaseFirstRead)

	firstResult := make(chan terminalReadResult, 1)
	go func() {
		key, err := runtime.readKey()
		firstResult <- terminalReadResult{key: key, err: err}
	}()
	<-reader.firstReadStarted

	secondStarted := make(chan struct{})
	secondResult := make(chan terminalReadResult, 1)
	go runQueuedTerminalRead(runtime, secondStarted, secondResult)
	<-secondStarted
	waitForQueuedTerminalRead(t)

	closeResult := make(chan error, 1)
	go func() {
		closeResult <- runtime.close()
	}()
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("close() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		reader.releaseFirstRead()
		t.Fatal("close() blocked behind the active read")
	}
	if driver.restored != 1 {
		t.Fatalf("restore calls = %d, want 1", driver.restored)
	}
	reader.releaseFirstRead()

	first := receiveTerminalRead(t, "active read", firstResult)
	if first.err != nil || first.key != "a" {
		t.Errorf("active readKey() = (%q, %v), want (\"a\", nil)", first.key, first.err)
	}
	second := receiveTerminalRead(t, "queued read", secondResult)
	if second.err == nil || second.err.Error() != "terminal is not in raw mode" {
		t.Errorf("queued readKey() = (%q, %v), want inactive raw-mode error", second.key, second.err)
	}
	if reads := reader.readCount(); reads != 1 {
		t.Errorf("input reads = %d, want 1", reads)
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

func TestInterpretReportsRestoreErrorWhenExecutionSucceeds(t *testing.T) {
	restoreErr := errors.New("restore failed")
	machine, driver := newVMWithActiveTestTerminal(t, restoreErr)

	err := interpretVMSource(t, machine, "let value: int = 1")

	if !errors.Is(err, restoreErr) {
		t.Fatalf("Interpret() error = %v, want wrapped %v", err, restoreErr)
	}
	if !strings.Contains(err.Error(), "restore terminal") {
		t.Errorf("Interpret() error = %v, want restore terminal context", err)
	}
	if driver.restored != 1 {
		t.Errorf("restore calls = %d, want 1", driver.restored)
	}
}

func TestInterpretPreservesRuntimeErrorWhenRestoreAlsoFails(t *testing.T) {
	restoreErr := errors.New("restore failed")
	machine, driver := newVMWithActiveTestTerminal(t, restoreErr)

	err := interpretVMSource(t, machine, "1 / 0")

	if err == nil {
		t.Fatal("Interpret() error = nil, want runtime error")
	}
	if !strings.Contains(err.Error(), "division by zero") {
		t.Errorf("Interpret() error = %v, want division by zero", err)
	}
	if errors.Is(err, restoreErr) {
		t.Errorf("Interpret() error = %v, unexpectedly wraps restore error", err)
	}
	if driver.restored != 1 {
		t.Errorf("restore calls = %d, want 1", driver.restored)
	}
}
