package vm

import (
	"errors"
	"testing"

	"noxy-vm/internal/value"
)

func testTerminalResultDefinition() value.Value {
	return value.NewStruct("TerminalResult", []string{"ok", "error"})
}

func testKeyEventDefinition() value.Value {
	return value.NewStruct("KeyEvent", []string{"ok", "key", "error"})
}

func assertTerminalResult(t *testing.T, got, definition value.Value, ok bool, errorText string) {
	t.Helper()
	result := requireBuiltinInstance(t, got, definition)
	assertBuiltinValue(t, result.Fields["ok"], value.NewBool(ok))
	assertBuiltinValue(t, result.Fields["error"], value.NewString(errorText))
}

func assertKeyEvent(t *testing.T, got, definition value.Value, ok bool, key, errorText string) {
	t.Helper()
	event := requireBuiltinInstance(t, got, definition)
	assertBuiltinValue(t, event.Fields["ok"], value.NewBool(ok))
	assertBuiltinValue(t, event.Fields["key"], value.NewString(key))
	assertBuiltinValue(t, event.Fields["error"], value.NewString(errorText))
}

func TestTerminalBuiltins(t *testing.T) {
	t.Run("reports terminal availability and successful raw session", func(t *testing.T) {
		machine := New()
		machine.shared.Terminal = newTestTerminalRuntime(&fakeTerminalDriver{terminal: true}, "A")
		resultDefinition := testTerminalResultDefinition()
		eventDefinition := testKeyEventDefinition()

		assertBuiltinValue(t, callBuiltin(t, machine, "terminal_is_terminal"), value.NewBool(true))
		assertTerminalResult(t, callBuiltin(t, machine, "terminal_open_raw", resultDefinition), resultDefinition, true, "")
		assertKeyEvent(t, callBuiltin(t, machine, "terminal_read_key", eventDefinition), eventDefinition, true, "a", "")
		assertBuiltinValue(t, callBuiltin(t, machine, "terminal_close"), value.NewBool(true))
	})

	t.Run("returns inactive read fields", func(t *testing.T) {
		machine := New()
		machine.shared.Terminal = newTestTerminalRuntime(&fakeTerminalDriver{terminal: true}, "A")
		eventDefinition := testKeyEventDefinition()

		assertKeyEvent(t, callBuiltin(t, machine, "terminal_read_key", eventDefinition), eventDefinition, false, "", "terminal is not in raw mode")
	})

	t.Run("returns read error fields", func(t *testing.T) {
		machine := New()
		machine.shared.Terminal = newTestTerminalRuntime(&fakeTerminalDriver{terminal: true}, "")
		resultDefinition := testTerminalResultDefinition()
		eventDefinition := testKeyEventDefinition()

		assertTerminalResult(t, callBuiltin(t, machine, "terminal_open_raw", resultDefinition), resultDefinition, true, "")
		assertKeyEvent(t, callBuiltin(t, machine, "terminal_read_key", eventDefinition), eventDefinition, false, "", "EOF")
	})

	t.Run("returns operational failure fields", func(t *testing.T) {
		machine := New()
		machine.shared.Terminal = newTestTerminalRuntime(&fakeTerminalDriver{}, "")
		resultDefinition := testTerminalResultDefinition()

		assertTerminalResult(t, callBuiltin(t, machine, "terminal_open_raw", resultDefinition), resultDefinition, false, "standard input is not a terminal")
	})

	t.Run("returns false on close failure", func(t *testing.T) {
		machine := New()
		machine.shared.Terminal = newTestTerminalRuntime(&fakeTerminalDriver{terminal: true, closeErr: errors.New("restore failed")}, "")
		resultDefinition := testTerminalResultDefinition()

		assertTerminalResult(t, callBuiltin(t, machine, "terminal_open_raw", resultDefinition), resultDefinition, true, "")
		assertBuiltinValue(t, callBuiltin(t, machine, "terminal_close"), value.NewBool(false))
	})

	t.Run("returns null for malformed internal calls", func(t *testing.T) {
		machine := New()
		assertBuiltinValue(t, callBuiltin(t, machine, "terminal_open_raw"), value.NewNull())
		assertBuiltinValue(t, callBuiltin(t, machine, "terminal_open_raw", value.NewString("wrong")), value.NewNull())
		assertBuiltinValue(t, callBuiltin(t, machine, "terminal_read_key"), value.NewNull())
		assertBuiltinValue(t, callBuiltin(t, machine, "terminal_read_key", value.NewString("wrong")), value.NewNull())
	})

	t.Run("returns null for typed nil struct definitions", func(t *testing.T) {
		machine := New()
		typedNilDefinition := value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjStruct)(nil)}

		assertBuiltinValue(t, callBuiltin(t, machine, "terminal_open_raw", typedNilDefinition), value.NewNull())
		assertBuiltinValue(t, callBuiltin(t, machine, "terminal_read_key", typedNilDefinition), value.NewNull())
	})
}
