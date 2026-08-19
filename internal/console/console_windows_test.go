//go:build windows

package console

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// TestEnsureLineInputRepairsLeakedRawMode re-runs the test binary with
// CREATE_NO_WINDOW so the child owns a fresh console, poisons that console
// with the exact mode x/term.MakeRaw leaves behind, and verifies
// ensureLineInputMode restores a line-readable mode. The helper below does the
// console work; this parent only asserts on its output.
func TestEnsureLineInputRepairsLeakedRawMode(t *testing.T) {
	if os.Getenv("NOXY_CONSOLE_TEST_HELPER") == "1" {
		t.Skip("helper invocation")
	}

	cmd := exec.Command(os.Args[0], "-test.run", "TestConsoleModeHelper", "-test.v")
	cmd.Env = append(os.Environ(), "NOXY_CONSOLE_TEST_HELPER=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper process failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "CONSOLE_MODE_REPAIRED") {
		t.Fatalf("helper did not confirm repair; output:\n%s", out)
	}
}

// TestEnableANSIStdoutSetsVTProcessing usa o mesmo esquema de subprocesso com
// console proprio: o helper limpa ENABLE_VIRTUAL_TERMINAL_PROCESSING do
// CONOUT$ e verifica que enableVTOutput o liga de volta.
func TestEnableANSIStdoutSetsVTProcessing(t *testing.T) {
	if os.Getenv("NOXY_CONSOLE_TEST_HELPER") == "1" {
		t.Skip("helper invocation")
	}

	cmd := exec.Command(os.Args[0], "-test.run", "TestANSIOutputHelper", "-test.v")
	cmd.Env = append(os.Environ(), "NOXY_CONSOLE_TEST_HELPER=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper process failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "ANSI_OUTPUT_ENABLED") {
		t.Fatalf("helper did not confirm VT processing; output:\n%s", out)
	}
}

func TestANSIOutputHelper(t *testing.T) {
	if os.Getenv("NOXY_CONSOLE_TEST_HELPER") != "1" {
		t.Skip("only runs as helper subprocess")
	}

	conout, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open CONOUT$: %v", err)
	}
	defer conout.Close()
	handle := windows.Handle(conout.Fd())

	var original uint32
	if err := windows.GetConsoleMode(handle, &original); err != nil {
		t.Fatalf("GetConsoleMode: %v", err)
	}
	if err := windows.SetConsoleMode(handle, original&^windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		t.Fatalf("SetConsoleMode(sem VT): %v", err)
	}

	if err := enableVTOutput(handle); err != nil {
		t.Fatalf("enableVTOutput: %v", err)
	}

	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		t.Fatalf("GetConsoleMode after enable: %v", err)
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING == 0 {
		t.Fatalf("VT processing not enabled: mode=%#x", mode)
	}
	t.Log("ANSI_OUTPUT_ENABLED")
}

// Um handle que nao e console (pipe) deve falhar, e EnableANSIStdout entao
// reporta false — e isso que mantem bytes de escape fora de saida redirecionada.
func TestEnableVTOutputRejectsPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	if err := enableVTOutput(windows.Handle(w.Fd())); err == nil {
		t.Fatal("expected error for non-console handle")
	}
}

func TestConsoleModeHelper(t *testing.T) {
	if os.Getenv("NOXY_CONSOLE_TEST_HELPER") != "1" {
		t.Skip("only runs as helper subprocess")
	}

	conin, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open CONIN$: %v", err)
	}
	defer conin.Close()
	handle := windows.Handle(conin.Fd())

	var original uint32
	if err := windows.GetConsoleMode(handle, &original); err != nil {
		t.Fatalf("GetConsoleMode: %v", err)
	}

	leaked := original &^ (windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_LINE_INPUT)
	leaked |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if err := windows.SetConsoleMode(handle, leaked); err != nil {
		t.Fatalf("SetConsoleMode(leaked): %v", err)
	}

	if err := ensureLineInputMode(handle); err != nil {
		t.Fatalf("ensureLineInputMode: %v", err)
	}

	var repaired uint32
	if err := windows.GetConsoleMode(handle, &repaired); err != nil {
		t.Fatalf("GetConsoleMode after repair: %v", err)
	}
	if repaired&cookedInputFlags != cookedInputFlags {
		t.Fatalf("cooked flags missing: mode=%#x", repaired)
	}
	if repaired&windows.ENABLE_VIRTUAL_TERMINAL_INPUT != 0 {
		t.Fatalf("virtual terminal input still set: mode=%#x", repaired)
	}
	t.Log("CONSOLE_MODE_REPAIRED")
}
