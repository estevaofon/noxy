//go:build windows

// Package console normalizes shared terminal state that other processes may
// have corrupted. On Windows the console input mode belongs to the console,
// not to the process: when a program enables raw mode (x/term.MakeRaw clears
// ENABLE_LINE_INPUT, ENABLE_ECHO_INPUT and ENABLE_PROCESSED_INPUT and sets
// ENABLE_VIRTUAL_TERMINAL_INPUT) and dies without restoring, every later
// process in that terminal inherits the raw mode. A line-oriented reader then
// blocks forever: keystrokes arrive unassembled, without echo, and Enter
// yields '\r' with no '\n'. Shells such as PSReadLine reset the mode at every
// prompt, which hides the corruption until a program like the noxy REPL reads
// stdin directly.
package console

import (
	"os"

	"golang.org/x/sys/windows"
)

const cookedInputFlags = windows.ENABLE_PROCESSED_INPUT |
	windows.ENABLE_LINE_INPUT |
	windows.ENABLE_ECHO_INPUT

// EnsureLineInput makes the process's stdin console usable for line-oriented
// reads, repairing a raw mode leaked by a crashed program. It is a no-op when
// stdin is not a Windows console (pipe, file, MSYS terminal).
func EnsureLineInput() {
	ensureLineInputMode(windows.Handle(os.Stdin.Fd()))
}

func ensureLineInputMode(handle windows.Handle) error {
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return err
	}
	want := (mode | cookedInputFlags) &^ windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	if want == mode {
		return nil
	}
	return windows.SetConsoleMode(handle, want)
}

// EnableANSIStdout reports whether stdout can render ANSI escape sequences,
// enabling ENABLE_VIRTUAL_TERMINAL_PROCESSING on the console when needed
// (Windows Terminal ja liga por padrao; conhost classico nao). Returns false
// when stdout is not a Windows console (pipe, file, MSYS terminal), keeping
// escape bytes out of redirected output.
func EnableANSIStdout() bool {
	return enableVTOutput(windows.Handle(os.Stdout.Fd())) == nil
}

func enableVTOutput(handle windows.Handle) error {
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return err
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return nil
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
