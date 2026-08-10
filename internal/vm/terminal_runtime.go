package vm

import (
	"bufio"
	"fmt"
	"sync"
	"unicode"

	"golang.org/x/term"
)

type terminalSnapshot struct {
	state *term.State
}

type terminalDriver interface {
	isTerminal(fd int) bool
	makeRaw(fd int) (*terminalSnapshot, error)
	restore(fd int, snapshot *terminalSnapshot) error
}

type xTermDriver struct{}

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

type terminalRuntime struct {
	stateMu sync.Mutex
	readMu  sync.Mutex
	driver  terminalDriver
	input   *bufio.Reader
	fd      int
	raw     bool
	saved   *terminalSnapshot
}

func (runtime *terminalRuntime) isTerminal() bool {
	return runtime.driver.isTerminal(runtime.fd)
}

func (runtime *terminalRuntime) openRaw() error {
	runtime.stateMu.Lock()
	defer runtime.stateMu.Unlock()

	if runtime.raw {
		return nil
	}
	if !runtime.isTerminal() {
		return fmt.Errorf("standard input is not a terminal")
	}

	snapshot, err := runtime.driver.makeRaw(runtime.fd)
	if err != nil {
		return err
	}
	runtime.saved = snapshot
	runtime.raw = true
	return nil
}

func (runtime *terminalRuntime) close() error {
	runtime.stateMu.Lock()
	defer runtime.stateMu.Unlock()

	if !runtime.raw {
		return nil
	}
	if err := runtime.driver.restore(runtime.fd, runtime.saved); err != nil {
		return err
	}
	runtime.raw = false
	runtime.saved = nil
	return nil
}

func (runtime *terminalRuntime) readKey() (string, error) {
	runtime.stateMu.Lock()
	if !runtime.raw {
		runtime.stateMu.Unlock()
		return "", fmt.Errorf("terminal is not in raw mode")
	}
	runtime.stateMu.Unlock()

	runtime.readMu.Lock()
	defer runtime.readMu.Unlock()

	r, _, err := runtime.input.ReadRune()
	if err != nil {
		return "", err
	}

	switch r {
	case ' ':
		return "space", nil
	case '\r', '\n':
		return "enter", nil
	case '\x03':
		return "ctrl+c", nil
	}
	if unicode.IsControl(r) {
		return fmt.Sprintf("unknown:0x%02x", r), nil
	}
	if r >= 'A' && r <= 'Z' {
		r = unicode.ToLower(r)
	}
	return string(r), nil
}
