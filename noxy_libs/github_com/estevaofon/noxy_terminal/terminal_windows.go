//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"

	"golang.org/x/sys/windows"
)

var cancelSynchronousIOProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

type windowsTerminalRead struct {
	thread   windows.Handle
	done     chan struct{}
	handleMu sync.Mutex
}

type windowsTerminalDevice struct {
	file *os.File

	readMu sync.Mutex
	state  sync.Mutex
	active *windowsTerminalRead
	closed bool

	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func openTerminalDevice() (terminalDevice, error) {
	file, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &windowsTerminalDevice{
		file:      file,
		closeDone: make(chan struct{}),
	}, nil
}

func (d *windowsTerminalDevice) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}

	d.readMu.Lock()
	defer d.readMu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	thread, err := duplicateCurrentThread()
	if err != nil {
		return 0, fmt.Errorf("duplicate console reader thread: %w", err)
	}

	operation := &windowsTerminalRead{
		thread: thread,
		done:   make(chan struct{}),
	}

	d.state.Lock()
	if d.closed {
		d.state.Unlock()
		_ = windows.CloseHandle(thread)
		return 0, os.ErrClosed
	}
	d.active = operation
	d.state.Unlock()

	n, readErr := d.file.Read(buffer)

	d.state.Lock()
	if d.active == operation {
		d.active = nil
	}
	d.state.Unlock()
	operation.handleMu.Lock()
	if err := windows.CloseHandle(thread); err != nil {
		readErr = errors.Join(readErr, fmt.Errorf("close console reader thread: %w", err))
	}
	close(operation.done)
	operation.handleMu.Unlock()
	return n, readErr
}

func (d *windowsTerminalDevice) Close() error {
	d.closeOnce.Do(func() {
		d.closeErr = d.close()
		close(d.closeDone)
	})
	<-d.closeDone
	return d.closeErr
}

func (d *windowsTerminalDevice) close() error {
	d.state.Lock()
	d.closed = true
	operation := d.active
	d.state.Unlock()

	if operation != nil {
		if err := cancelTerminalRead(operation); err != nil {
			return err
		}
	}
	return d.file.Close()
}

func (d *windowsTerminalDevice) Fd() uintptr {
	return d.file.Fd()
}

func duplicateCurrentThread() (windows.Handle, error) {
	process := windows.CurrentProcess()
	var thread windows.Handle
	err := windows.DuplicateHandle(
		process,
		windows.CurrentThread(),
		process,
		&thread,
		0,
		false,
		windows.DUPLICATE_SAME_ACCESS,
	)
	return thread, err
}

func cancelTerminalRead(operation *windowsTerminalRead) error {
	for {
		operation.handleMu.Lock()
		select {
		case <-operation.done:
			operation.handleMu.Unlock()
			return nil
		default:
		}

		err := cancelSynchronousIO(operation.thread)
		operation.handleMu.Unlock()
		if err == nil {
			<-operation.done
			return nil
		}
		if !errors.Is(err, windows.ERROR_NOT_FOUND) {
			return fmt.Errorf("cancel console read: %w", err)
		}

		// The reader publishes its real thread handle immediately before
		// entering ReadConsoleW. If cancellation wins that small race, let the
		// reader enter the syscall and try again.
		runtime.Gosched()
	}
}

func cancelSynchronousIO(thread windows.Handle) error {
	result, _, callErr := cancelSynchronousIOProc.Call(uintptr(thread))
	if result != 0 {
		return nil
	}
	return callErr
}
