package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
	"testing"
)

func compileVMSource(t *testing.T, source string) *chunk.Chunk {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.New().Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}
	return code
}

func interpretVMSource(t *testing.T, machine *VM, source string) error {
	t.Helper()
	return machine.Interpret(compileVMSource(t, source))
}

func captureVMSource(t *testing.T, source string) value.Value {
	t.Helper()
	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, source); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	return captured
}

func requireBuiltin(t *testing.T, machine *VM, name string) *value.ObjNative {
	t.Helper()
	actual, ok := machine.GetGlobal(name)
	if !ok {
		t.Fatalf("builtin %q is not registered", name)
	}
	native, ok := actual.Obj.(*value.ObjNative)
	if actual.Type != value.VAL_NATIVE || !ok || native == nil {
		t.Fatalf("global %q is not a native: %#v", name, actual)
	}
	return native
}

func callBuiltin(t *testing.T, machine *VM, name string, args ...value.Value) value.Value {
	t.Helper()
	result, err := requireBuiltin(t, machine, name).Invoke(machine, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}
