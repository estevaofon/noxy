package main

import (
	"bufio"
	"fmt"
	"io"
	"sync"

	"golang.org/x/term"
)

type terminalDevice interface {
	io.Reader
	io.Closer
	Fd() uintptr
}

type terminalDriver interface {
	open() (terminalDevice, error)
	isTerminal(fd int) bool
	makeRaw(fd int) (*terminalSnapshot, error)
	restore(fd int, snapshot *terminalSnapshot) error
}

type terminalSnapshot struct {
	state *term.State
}

type xTermDriver struct{}

type terminalRuntime struct {
	stateMu  sync.Mutex
	readMu   sync.Mutex
	driver   terminalDriver
	device   terminalDevice
	input    *bufio.Reader
	saved    *terminalSnapshot
	raw      bool
	stopping bool
	lastErr  string
}

func (xTermDriver) open() (terminalDevice, error) {
	return openTerminalDevice()
}

func (xTermDriver) isTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

func (xTermDriver) makeRaw(fd int) (*terminalSnapshot, error) {
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return &terminalSnapshot{state: state}, nil
}

func (xTermDriver) restore(fd int, snapshot *terminalSnapshot) error {
	return term.Restore(fd, snapshot.state)
}

func newTerminalRuntime(driver terminalDriver) *terminalRuntime {
	return &terminalRuntime{driver: driver}
}

func (r *terminalRuntime) isTerminal() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()

	device, err := r.driver.open()
	if err != nil {
		r.lastErr = err.Error()
		return false
	}

	isTerminal := r.driver.isTerminal(int(device.Fd()))
	if err := device.Close(); err != nil {
		r.lastErr = err.Error()
		return false
	}

	r.lastErr = ""
	return isTerminal
}

func (r *terminalRuntime) openRaw() bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()

	if r.stopping {
		r.lastErr = "terminal runtime is shutting down"
		return false
	}
	if r.raw {
		r.lastErr = ""
		return true
	}

	device, err := r.driver.open()
	if err != nil {
		r.lastErr = err.Error()
		return false
	}

	if !r.driver.isTerminal(int(device.Fd())) {
		if err := device.Close(); err != nil {
			r.lastErr = err.Error()
			return false
		}
		r.lastErr = "terminal device is not a terminal"
		return false
	}

	snapshot, err := r.driver.makeRaw(int(device.Fd()))
	if err != nil {
		if closeErr := device.Close(); closeErr != nil {
			r.lastErr = closeErr.Error()
			return false
		}
		r.lastErr = err.Error()
		return false
	}

	r.device = device
	r.input = bufio.NewReader(device)
	r.saved = snapshot
	r.raw = true
	r.lastErr = ""
	return true
}

func (r *terminalRuntime) readKey() (string, bool) {
	r.readMu.Lock()
	defer r.readMu.Unlock()

	r.stateMu.Lock()
	input := r.input
	r.stateMu.Unlock()

	if input == nil {
		r.recordError(fmt.Errorf("terminal is not open"))
		return "", false
	}

	key, _, err := input.ReadRune()
	if err != nil {
		r.recordError(err)
		return "", false
	}

	r.clearError()
	return normalizeKey(key), true
}

func (r *terminalRuntime) lastError() string {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	return r.lastErr
}

func (r *terminalRuntime) close() bool {
	r.readMu.Lock()
	defer r.readMu.Unlock()

	r.stateMu.Lock()
	defer r.stateMu.Unlock()

	if !r.raw {
		r.lastErr = ""
		return true
	}

	if err := r.driver.restore(int(r.device.Fd()), r.saved); err != nil {
		r.lastErr = err.Error()
		return false
	}

	if err := r.device.Close(); err != nil {
		r.clearResources()
		r.lastErr = err.Error()
		return false
	}

	r.clearResources()
	r.lastErr = ""
	return true
}

func (r *terminalRuntime) shutdown() {
	r.stateMu.Lock()
	r.stopping = true
	r.stateMu.Unlock()

	r.readMu.Lock()
	defer r.readMu.Unlock()

	r.stateMu.Lock()
	defer r.stateMu.Unlock()

	if r.device == nil {
		r.lastErr = ""
		return
	}

	var operationErr error
	if r.raw {
		operationErr = r.driver.restore(int(r.device.Fd()), r.saved)
	}
	if err := r.device.Close(); err != nil && operationErr == nil {
		operationErr = err
	}
	r.clearResources()
	if operationErr != nil {
		r.lastErr = operationErr.Error()
		return
	}
	r.lastErr = ""
}

func (r *terminalRuntime) recordError(err error) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.lastErr = err.Error()
}

func (r *terminalRuntime) clearError() {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.lastErr = ""
}

func (r *terminalRuntime) clearResources() {
	r.device = nil
	r.input = nil
	r.saved = nil
	r.raw = false
}

func normalizeKey(key rune) string {
	switch key {
	case ' ':
		return "space"
	case '\r', '\n':
		return "enter"
	case '\x03':
		return "ctrl+c"
	}
	if key >= 'A' && key <= 'Z' {
		return string(key + ('a' - 'A'))
	}
	if key < 0x20 || key == 0x7f {
		return fmt.Sprintf("unknown:0x%02x", key)
	}
	return string(key)
}
