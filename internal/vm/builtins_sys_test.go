package vm

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"noxy-vm/internal/value"
)

func TestSafeSystemBuiltinsPreserveProcessStateContracts(t *testing.T) {
	machine := New()
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_os"), value.NewString(runtime.GOOS))

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_getcwd"), value.NewString(workingDirectory))

	environmentKey := "NOXY_VM_STATEFUL_BUILTIN_TEST"
	t.Setenv(environmentKey, "before")
	environmentDefinition := value.NewStruct("EnvResult", []string{"value", "ok"})
	before := requireBuiltinInstance(t, callBuiltin(t, machine, "sys_getenv", value.NewString(environmentKey), environmentDefinition), environmentDefinition)
	assertBuiltinValue(t, before.Fields["value"], value.NewString("before"))
	assertBuiltinValue(t, before.Fields["ok"], value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_setenv", value.NewString(environmentKey), value.NewString("after")), value.NewBool(true))
	after := requireBuiltinInstance(t, callBuiltin(t, machine, "sys_getenv", value.NewString(environmentKey), environmentDefinition), environmentDefinition)
	assertBuiltinValue(t, after.Fields["value"], value.NewString("after"))
	assertBuiltinValue(t, after.Fields["ok"], value.NewBool(true))

	arguments := requireBuiltinArray(t, callBuiltin(t, machine, "sys_argv"))
	if len(arguments.Elements) != len(os.Args) {
		t.Fatalf("sys_argv length = %d, want %d", len(arguments.Elements), len(os.Args))
	}
	for index, argument := range os.Args {
		assertBuiltinValue(t, arguments.Elements[index], value.NewString(argument))
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_sleep", value.NewInt(0)), value.NewNull())
}

func TestSystemExecutionAndPluginErrorsDoNotInvokeExternalPrograms(t *testing.T) {
	machine := New()

	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec"), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec", value.NewString("")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec", value.NewString(""), value.NewNull()), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec_output"), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec_output", value.NewString("")), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exec_output", value.NewString(""), value.NewNull()), value.NewNull())

	nonexistentPlugin := filepath.Join(t.TempDir(), "missing-plugin-binary")
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_load_plugin"), value.NewBool(false))
	assertBuiltinValue(t, callBuiltin(t, machine, "sys_load_plugin", value.NewString("missing"), value.NewString(nonexistentPlugin)), value.NewBool(false))
}

func TestSystemExitFromChildVMRestoresSharedTerminalBeforeExiting(t *testing.T) {
	parent, driver := newVMWithActiveTestTerminal(t, nil)
	child := NewWithShared(parent.shared, parent.Config)

	requestedCode := -1
	parent.shared.exitProcess = func(code int) {
		if driver.restored != 1 {
			t.Errorf("restore calls when exit function ran = %d, want 1", driver.restored)
		}
		if parent.shared.Terminal.raw {
			t.Error("terminal remained in raw mode when exit function ran")
		}
		requestedCode = code
	}

	assertBuiltinValue(t, callBuiltin(t, child, "sys_exit", value.NewInt(23)), value.NewNull())

	if requestedCode != 23 {
		t.Errorf("exit code = %d, want 23", requestedCode)
	}
	if driver.restored != 1 {
		t.Errorf("restore calls = %d, want 1", driver.restored)
	}
}

func TestSystemExitContinuesWhenTerminalRestoreFails(t *testing.T) {
	restoreErr := errors.New("restore failed")
	machine, driver := newVMWithActiveTestTerminal(t, restoreErr)

	requestedCode := -1
	machine.shared.exitProcess = func(code int) {
		requestedCode = code
	}

	assertBuiltinValue(t, callBuiltin(t, machine, "sys_exit"), value.NewNull())

	if requestedCode != 0 {
		t.Errorf("exit code = %d, want default 0", requestedCode)
	}
	if driver.restored != 1 {
		t.Errorf("restore calls = %d, want 1", driver.restored)
	}
	if !machine.shared.Terminal.raw {
		t.Error("terminal raw state cleared after failed restoration")
	}
}
