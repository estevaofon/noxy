package vm

import (
	"errors"
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

func TestPreparedTaskCallReturnsExplicitResult(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func worker(value: int) -> int
    return value * 2
end`); err != nil {
		t.Fatal(err)
	}
	callable, _ := machine.GetGlobal("worker")
	call, err := machine.prepareTaskCall(callable, []value.Value{value.NewInt(21)})
	if err != nil {
		t.Fatal(err)
	}
	child := NewWithShared(machine.shared, machine.Config)
	got, err := child.executePreparedTaskCall(call)
	if err != nil || got.Type != value.VAL_INT || got.Int() != 42 {
		t.Fatalf("result=%v err=%v", got, err)
	}
}

func TestRuntimeErrorCapturesNoxyStackAtFailurePoint(t *testing.T) {
	machine := New()
	err := interpretVMSource(t, machine, `
func inner() -> int
    return 1 / 0
end
func outer() -> int
    return inner()
end
outer()`)
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("error type = %T, want *RuntimeError", err)
	}
	if !strings.Contains(runtimeErr.Stack, "in inner") ||
		!strings.Contains(runtimeErr.Stack, "in outer") {
		t.Fatalf("stack = %q", runtimeErr.Stack)
	}
	if runtimeErr.Message != "division by zero" {
		t.Fatalf("message = %q, want division by zero", runtimeErr.Message)
	}
}

func TestRuntimeErrorRemainsTypedThroughImportFailure(t *testing.T) {
	machine := NewWithConfig(VMConfig{RootPath: t.TempDir()})
	err := interpretVMSource(t, machine, "use missing_task_test_module")
	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) {
		t.Fatalf("error type chain = %T, want *RuntimeError: %v", err, err)
	}
	if !strings.Contains(runtimeErr.Stack, "in script") {
		t.Fatalf("stack = %q", runtimeErr.Stack)
	}
}

// Contrato CoW: parâmetro por valor de task é capturado sem copiar — mesmo
// ponteiro, cópia adiada para a primeira mutação (unicize) no bytecode.
// Depois da chave (spec docs/superpowers/specs/
// 2026-08-17-cow-rc-uniqueness-design.md §3), o que registra a captura é o
// contador de donos e não o bit sticky: `outer` é montado fora do bytecode e
// nasce sem dono, então a captura o leva de 0 para 1 (em código real o dono do
// chamador já estaria contado, o total passaria de 1 e a primeira mutação
// dentro da task clonaria).
func TestPreparedTaskCallRetainsValueParameter(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func worker(values: int[][]) -> int
    return 0
end`); err != nil {
		t.Fatal(err)
	}
	callable, _ := machine.GetGlobal("worker")
	nested := value.NewArray([]value.Value{value.NewInt(7)})
	outer := value.NewArray([]value.Value{nested})
	ownersBefore := value.OwnersCount(outer)

	call, err := machine.prepareTaskCall(callable, []value.Value{outer})
	if err != nil {
		t.Fatal(err)
	}
	gotOuter := call.Arguments[0].Obj.(*value.ObjArray)
	wantOuter := outer.Obj.(*value.ObjArray)
	if gotOuter != wantOuter {
		t.Fatal("CoW: o arg deve manter o ponteiro (cópia adiada)")
	}
	if got := value.OwnersCount(call.Arguments[0]); got != ownersBefore+1 {
		t.Fatalf("CoW: a captura do arg composto por valor deve contar um dono durável: esperado %d, veio %d", ownersBefore+1, got)
	}
	if gotOuter.Elements[0].Obj != nested.Obj {
		t.Fatal("marcação não pode clonar o aninhado")
	}
}

func TestPreparedTaskCallRetainsReferenceIdentity(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func worker(target: ref int) -> int
    return *target
end`); err != nil {
		t.Fatal(err)
	}
	callable, _ := machine.GetGlobal("worker")
	stored := value.NewInt(9)
	ref := &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}
	argument := value.Value{Type: value.VAL_REF, Obj: ref}

	call, err := machine.prepareTaskCall(callable, []value.Value{argument})
	if err != nil {
		t.Fatal(err)
	}
	if call.Arguments[0].Obj != ref {
		t.Fatal("ref parameter did not retain its ObjRef pointer")
	}
}

func TestPreparedTaskCallRejectsInvalidCallsSynchronously(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func by_value(item: int) -> int
    return item
end
func by_ref(item: ref int) -> int
    return *item
end`); err != nil {
		t.Fatal(err)
	}
	byValue, _ := machine.GetGlobal("by_value")
	byRef, _ := machine.GetGlobal("by_ref")
	stored := value.NewInt(1)
	reference := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}}

	tests := []struct {
		name     string
		callable value.Value
		args     []value.Value
		want     string
	}{
		{name: "native", callable: value.NewNative("native", func([]value.Value) value.Value { return value.NewNull() }), want: "script function"},
		{name: "malformed", callable: value.Value{Type: value.VAL_FUNCTION, Obj: "not a function"}, want: "malformed"},
		{name: "wrong arity", callable: byValue, want: "expected 1 arguments but got 0"},
		{name: "wrong mode", callable: byValue, args: []value.Value{reference}, want: "expected int, got ref"},
		{name: "wrong type", callable: byValue, args: []value.Value{value.NewString("wrong")}, want: "expected int, got object"},
		{name: "ref mode", callable: byRef, args: []value.Value{value.NewInt(1)}, want: "expected ref int, got int"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := machine.prepareTaskCall(test.callable, test.args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestPreparedTaskCallRejectsMalformedCallableMetadata(t *testing.T) {
	machine := New()
	validChunk := chunk.New()
	var nilChunk *chunk.Chunk

	tests := []struct {
		name     string
		callable value.Value
	}{
		{
			name: "typed nil chunk",
			callable: value.Value{Type: value.VAL_FUNCTION, Obj: &value.ObjFunction{
				Name: "worker", Chunk: nilChunk, Environment: machine.shared.Root,
			}},
		},
		{
			name: "parameter count mismatch",
			callable: value.Value{Type: value.VAL_FUNCTION, Obj: &value.ObjFunction{
				Name: "worker", Arity: 1, Chunk: validChunk, Environment: machine.shared.Root,
			}},
		},
		{
			name: "raw function requiring upvalues",
			callable: value.Value{Type: value.VAL_FUNCTION, Obj: &value.ObjFunction{
				Name: "worker", UpvalueCount: 1, Chunk: validChunk, Environment: machine.shared.Root,
			}},
		},
		{
			name: "closure upvalue count mismatch",
			callable: value.Value{Type: value.VAL_FUNCTION, Obj: &value.ObjClosure{
				Function:    &value.ObjFunction{Name: "worker", UpvalueCount: 1, Chunk: validChunk, Environment: machine.shared.Root},
				Environment: machine.shared.Root,
			}},
		},
		{
			name: "nil closure upvalue",
			callable: value.Value{Type: value.VAL_FUNCTION, Obj: &value.ObjClosure{
				Function:    &value.ObjFunction{Name: "worker", UpvalueCount: 1, Chunk: validChunk, Environment: machine.shared.Root},
				Upvalues:    []*value.ObjUpvalue{nil},
				Environment: machine.shared.Root,
			}},
		},
		{
			name: "nil closure upvalue location",
			callable: value.Value{Type: value.VAL_FUNCTION, Obj: &value.ObjClosure{
				Function:    &value.ObjFunction{Name: "worker", UpvalueCount: 1, Chunk: validChunk, Environment: machine.shared.Root},
				Upvalues:    []*value.ObjUpvalue{{}},
				Environment: machine.shared.Root,
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := machine.prepareTaskCall(test.callable, nil)
			if err == nil || !strings.Contains(err.Error(), "malformed") {
				t.Fatalf("error = %v, want malformed callable rejection", err)
			}
		})
	}
}

func TestPreparedTaskCallChildUsesCallerConfigurationAndClosureEnvironment(t *testing.T) {
	machine := NewWithConfig(VMConfig{RootPath: "caller-root"})
	observedRoot := ""
	machine.DefineContextualNative("task_test_config", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		child, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		observedRoot = child.Config.RootPath
		return value.NewInt(2), nil
	})
	environment := value.NewGlobalEnvironment(machine.shared.Root)
	if err := machine.InterpretWithEnvironment(compileVMSource(t, `
let offset: int = 40
func worker() -> int
    return offset + task_test_config()
end`), environment); err != nil {
		t.Fatal(err)
	}
	callable, ok := environment.GetLocal("worker")
	if !ok {
		t.Fatal("worker was not bound in the custom environment")
	}
	call, err := machine.prepareTaskCall(callable, nil)
	if err != nil {
		t.Fatal(err)
	}
	child := NewWithShared(machine.shared, machine.Config)
	got, err := child.executePreparedTaskCall(call)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != value.VAL_INT || got.Int() != 42 {
		t.Fatalf("result = %v, want 42", got)
	}
	if observedRoot != "caller-root" {
		t.Fatalf("child root = %q, want caller-root", observedRoot)
	}
}
