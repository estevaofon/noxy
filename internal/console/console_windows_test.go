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
