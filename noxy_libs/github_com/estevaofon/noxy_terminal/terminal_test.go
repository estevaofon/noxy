package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeTerminalDevice struct {
	reader     io.Reader
	fd         uintptr
	closeErr   error
	closeCalls int
}

func (d *fakeTerminalDevice) Read(p []byte) (int, error) {
	return d.reader.Read(p)
}

func (d *fakeTerminalDevice) Close() error {
	d.closeCalls++
	return d.closeErr
}

func (d *fakeTerminalDevice) Fd() uintptr {
	return d.fd
}

type fakeTerminalDriver struct {
	device       terminalDevice
	openErr      error
	terminal     bool
	makeRawErr   error
	restoreErr   error
	openCalls    int
	makeRawCalls int
	restoreCalls int
}

func (d *fakeTerminalDriver) open() (terminalDevice, error) {
	d.openCalls++
	return d.device, d.openErr
}

func (d *fakeTerminalDriver) isTerminal(int) bool {
	return d.terminal
}

func (d *fakeTerminalDriver) makeRaw(int) (*terminalSnapshot, error) {
	d.makeRawCalls++
	if d.makeRawErr != nil {
		return nil, d.makeRawErr
	}
	return &terminalSnapshot{}, nil
}

func (d *fakeTerminalDriver) restore(int, *terminalSnapshot) error {
	d.restoreCalls++
	return d.restoreErr
}

func TestRuntimeRejectsNonTerminal(t *testing.T) {
	device := &fakeTerminalDevice{reader: strings.NewReader("")}
	driver := &fakeTerminalDriver{device: device}
	runtime := newTerminalRuntime(driver)

	if runtime.isTerminal() {
		t.Fatal("isTerminal() = true, want false")
	}
	if runtime.openRaw() {
		t.Fatal("openRaw() = true, want false")
	}
	if device.closeCalls != 2 {
		t.Fatalf("Close calls = %d, want 2", device.closeCalls)
	}
}

func TestRuntimeOpenReadCloseIsIdempotent(t *testing.T) {
	device := &fakeTerminalDevice{reader: strings.NewReader("A")}
	driver := &fakeTerminalDriver{device: device, terminal: true}
	runtime := newTerminalRuntime(driver)

	if !runtime.openRaw() {
		t.Fatalf("first openRaw() = false: %s", runtime.lastError())
	}
	if !runtime.openRaw() {
		t.Fatalf("second openRaw() = false: %s", runtime.lastError())
	}
	if driver.makeRawCalls != 1 {
		t.Fatalf("makeRaw calls = %d, want 1", driver.makeRawCalls)
	}

	key, ok := runtime.readKey()
	if !ok || key != "a" {
		t.Fatalf("readKey() = %q, %t; want %q, true", key, ok, "a")
	}

	if !runtime.close() {
		t.Fatalf("first close() = false: %s", runtime.lastError())
	}
	if !runtime.close() {
		t.Fatalf("second close() = false: %s", runtime.lastError())
	}
	if driver.restoreCalls != 1 {
		t.Fatalf("restore calls = %d, want 1", driver.restoreCalls)
	}
	if device.closeCalls != 1 {
		t.Fatalf("Close calls = %d, want 1", device.closeCalls)
	}
}

func TestRuntimeReportsRestoreFailure(t *testing.T) {
	device := &fakeTerminalDevice{reader: strings.NewReader("")}
	driver := &fakeTerminalDriver{
		device:     device,
		terminal:   true,
		restoreErr: errors.New("restore failed"),
	}
	runtime := newTerminalRuntime(driver)

	if !runtime.openRaw() {
		t.Fatalf("openRaw() = false: %s", runtime.lastError())
	}
	if runtime.close() {
		t.Fatal("close() = true, want false")
	}
	if got := runtime.lastError(); got != "restore failed" {
		t.Fatalf("lastError() = %q, want %q", got, "restore failed")
	}
	if device.closeCalls != 0 {
		t.Fatalf("Close calls = %d, want 0", device.closeCalls)
	}
}

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		input rune
		want  string
	}{
		{'A', "a"},
		{' ', "space"},
		{'\r', "enter"},
		{'\n', "enter"},
		{'\x03', "ctrl+c"},
		{'é', "é"},
		{'\x01', "unknown:0x01"},
	}

	for _, test := range tests {
		t.Run(test.want, func(t *testing.T) {
			if got := normalizeKey(test.input); got != test.want {
				t.Fatalf("normalizeKey(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestShutdownRestoresAndClosesDevice(t *testing.T) {
	device := &fakeTerminalDevice{reader: strings.NewReader("")}
	driver := &fakeTerminalDriver{device: device, terminal: true}
	runtime := newTerminalRuntime(driver)

	if !runtime.openRaw() {
		t.Fatalf("openRaw() = false: %s", runtime.lastError())
	}
	runtime.shutdown()

	if driver.restoreCalls != 1 {
		t.Fatalf("restore calls = %d, want 1", driver.restoreCalls)
	}
	if device.closeCalls != 1 {
		t.Fatalf("Close calls = %d, want 1", device.closeCalls)
	}
	if runtime.openRaw() {
		t.Fatal("openRaw() after shutdown = true, want false")
	}
}
