package vm

import (
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
