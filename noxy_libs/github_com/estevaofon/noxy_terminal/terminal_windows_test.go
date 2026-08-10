//go:build windows

package main

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const windowsConsoleReadHelper = "NOXY_TERMINAL_WINDOWS_CONSOLE_READ_HELPER"

func TestWindowsTerminalDeviceCloseUnblocksRead(t *testing.T) {
	if os.Getenv(windowsConsoleReadHelper) == "1" {
		runWindowsConsoleReadHelper(t)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsTerminalDeviceCloseUnblocksRead$")
	command.Env = append(os.Environ(), windowsConsoleReadHelper+"=1")
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_CONSOLE,
		HideWindow:    true,
	}

	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("console read helper did not terminate after device close: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("console read helper failed: %v\n%s", err, output)
	}
}

func runWindowsConsoleReadHelper(t *testing.T) {
	device, err := openTerminalDevice()
	if err != nil {
		t.Fatalf("open CONIN$: %v", err)
	}
	windowsDevice, ok := device.(*windowsTerminalDevice)
	if !ok {
		t.Fatalf("terminal device type = %T, want *windowsTerminalDevice", device)
	}

	readDone := make(chan error, 1)
	go func() {
		var buffer [4]byte
		_, err := device.Read(buffer[:])
		readDone <- err
	}()

	waitForWindowsTerminalRead(t, windowsDevice)
	select {
	case err := <-readDone:
		t.Fatalf("CONIN$ read returned before Close: %v", err)
	default:
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- device.Close()
	}()

	select {
	case err := <-readDone:
		if err == nil {
			t.Fatal("cancelled CONIN$ read returned without an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked CONIN$ read was not released by Close")
	}

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("close CONIN$: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the CONIN$ read ended")
	}
}

func waitForWindowsTerminalRead(t *testing.T, device *windowsTerminalDevice) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		device.state.Lock()
		active := device.active != nil
		device.state.Unlock()
		if active {
			return
		}

		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("CONIN$ read did not become active")
		}
	}
}
