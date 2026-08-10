package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestPluginServerPrimitiveResultsAndLastError(t *testing.T) {
	device := &fakeTerminalDevice{reader: strings.NewReader("A")}
	driver := &fakeTerminalDriver{device: device, terminal: true}
	server := newPluginServer(newTerminalRuntime(driver), io.Discard)

	assertPluginResult(t, server.handle(pluginRequest{Method: "is_terminal"}), true)
	assertPluginResult(t, server.handle(pluginRequest{Method: "open_raw"}), true)
	assertPluginResult(t, server.handle(pluginRequest{Method: "read_key"}), "a")
	assertPluginResult(t, server.handle(pluginRequest{Method: "close"}), true)

	readErr := errors.New("forced read failure")
	failingDevice := &fakeTerminalDevice{reader: errorReader{err: readErr}}
	failingDriver := &fakeTerminalDriver{device: failingDevice, terminal: true}
	failingServer := newPluginServer(newTerminalRuntime(failingDriver), io.Discard)
	assertPluginResult(t, failingServer.handle(pluginRequest{Method: "open_raw"}), true)
	assertPluginResult(t, failingServer.handle(pluginRequest{Method: "read_key"}), nil)
	assertPluginResult(t, failingServer.handle(pluginRequest{Method: "last_error"}), readErr.Error())
}

func TestPluginServerRestoresTerminalOnParentEOF(t *testing.T) {
	device := newBlockingTerminalDevice()
	driver := &fakeTerminalDriver{device: device, terminal: true}
	runtime := newTerminalRuntime(driver)
	server := newPluginServer(runtime, &bytes.Buffer{})
	inputReader, inputWriter := io.Pipe()
	t.Cleanup(func() {
		_ = inputWriter.Close()
		_ = inputReader.Close()
		runtime.shutdown()
	})
	serveDone := make(chan error, 1)

	go func() {
		serveDone <- server.serve(inputReader)
	}()

	if _, err := io.WriteString(inputWriter, "{\"method\":\"open_raw\",\"params\":[]}\n{\"method\":\"read_key\",\"params\":[]}\n"); err != nil {
		t.Fatalf("write requests: %v", err)
	}

	select {
	case <-device.readStarted:
	case <-time.After(time.Second):
		t.Fatal("read_key did not start")
	}

	if err := inputWriter.Close(); err != nil {
		t.Fatalf("close input: %v", err)
	}

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("serve() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve() did not return after parent EOF")
	}

	if driver.restoreCalls != 1 {
		t.Fatalf("restore calls = %d, want 1", driver.restoreCalls)
	}
	if got := device.closeCount(); got != 1 {
		t.Fatalf("Close calls = %d, want 1", got)
	}
}

func TestPluginServerRejectsUnknownMethod(t *testing.T) {
	server := newPluginServer(newTerminalRuntime(&fakeTerminalDriver{}), io.Discard)

	response := server.handle(pluginRequest{Method: "missing"})
	if response.Error != "unknown method: missing" {
		t.Fatalf("error = %q, want %q", response.Error, "unknown method: missing")
	}
}

func TestPluginServerWritesJSONResponsesAndContinuesAfterDecodeError(t *testing.T) {
	device := &fakeTerminalDevice{reader: strings.NewReader("")}
	driver := &fakeTerminalDriver{device: device, terminal: true}
	var output bytes.Buffer
	server := newPluginServer(newTerminalRuntime(driver), &output)

	input := strings.NewReader("not json\n{\"method\":\"is_terminal\",\"params\":[]}\n")
	if err := server.serve(input); err != nil {
		t.Fatalf("serve() error = %v, want nil", err)
	}

	decoder := json.NewDecoder(&output)
	var decodeError pluginResponse
	if err := decoder.Decode(&decodeError); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if decodeError.Error == "" {
		t.Fatal("decode error response has no error")
	}

	var result pluginResponse
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode result response: %v", err)
	}
	assertPluginResult(t, result, true)
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

func assertPluginResult(t *testing.T, response pluginResponse, want interface{}) {
	t.Helper()
	if response.Error != "" {
		t.Fatalf("unexpected response error: %s", response.Error)
	}
	if response.Result != want {
		t.Fatalf("result = %#v, want %#v", response.Result, want)
	}
}
