//go:build unix

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const (
	sigpipeHelper       = "NOXY_TERMINAL_SIGPIPE_HELPER"
	sigpipeHelperMarker = "NOXY_TERMINAL_SIGPIPE_MARKER"
)

func TestPluginServerClosedStdoutReturnsEPIPEAfterCleanup(t *testing.T) {
	if os.Getenv(sigpipeHelper) == "1" {
		runSIGPIPEHelper(t)
		return
	}

	marker := filepath.Join(t.TempDir(), "cleanup-complete")
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	if err := readEnd.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	defer writeEnd.Close()

	command := exec.Command(os.Args[0], "-test.run=^TestPluginServerClosedStdoutReturnsEPIPEAfterCleanup$")
	command.Env = append(os.Environ(),
		sigpipeHelper+"=1",
		sigpipeHelperMarker+"="+marker,
	)
	command.Stdout = writeEnd
	var stderr bytes.Buffer
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		t.Fatalf("SIGPIPE helper exited before cleanup: %v\n%s", err, stderr.Bytes())
	}
	contents, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read cleanup marker: %v", err)
	}
	if string(contents) != "restored-and-closed" {
		t.Fatalf("cleanup marker = %q, want %q", contents, "restored-and-closed")
	}
}

func runSIGPIPEHelper(t *testing.T) {
	device := &fakeTerminalDevice{reader: strings.NewReader("")}
	driver := &fakeTerminalDriver{device: device, terminal: true}
	server := newPluginServer(newTerminalRuntime(driver), os.Stdout)

	err := server.serve(strings.NewReader("{\"method\":\"open_raw\",\"params\":[]}\n"))
	if !errors.Is(err, syscall.EPIPE) {
		t.Fatalf("serve() error = %v, want EPIPE", err)
	}
	if driver.restoreCalls != 1 {
		t.Fatalf("restore calls = %d, want 1", driver.restoreCalls)
	}
	if device.closeCalls != 1 {
		t.Fatalf("Close calls = %d, want 1", device.closeCalls)
	}
	if err := os.WriteFile(os.Getenv(sigpipeHelperMarker), []byte("restored-and-closed"), 0o600); err != nil {
		t.Fatalf("write cleanup marker: %v", err)
	}
}
