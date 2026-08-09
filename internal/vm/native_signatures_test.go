package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
)

func runWithTypedTestNative(t *testing.T, input string, sig value.NativeSignature, called *bool) error {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.New().Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}
	machine := New()
	machine.DefineNativeWithSignature("typed_test", sig, func(args []value.Value) value.Value {
		*called = true
		return value.NewNull()
	})
	return machine.Interpret(code)
}

func TestTypedNativeRejectsModeBeforeInvocation(t *testing.T) {
	called := false
	sig := value.NativeSignature{
		Arity:      1,
		Params:     []value.ParamInfo{{IsRef: true, TypeName: "ref int"}},
		ReturnType: "void",
	}
	err := runWithTypedTestNative(t, "let n: int = 1\ntyped_test(n)", sig, &called)
	if err == nil || !strings.Contains(err.Error(), "expected ref int") {
		t.Fatalf("error=%v", err)
	}
	if called {
		t.Fatal("native closure must not run after contract failure")
	}
}

func TestTypedNativeAcceptsExplicitReference(t *testing.T) {
	called := false
	sig := value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{IsRef: true, TypeName: "ref int"}}, ReturnType: "void"}
	err := runWithTypedTestNative(t, "let n: int = 1\ntyped_test(ref n)", sig, &called)
	if err != nil || !called {
		t.Fatalf("called=%v error=%v", called, err)
	}
}

func TestTypedNativeRejectsIncorrectExactArityBeforeInvocation(t *testing.T) {
	called := false
	sig := value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int"}}, ReturnType: "void"}
	err := runWithTypedTestNative(t, "typed_test()", sig, &called)
	if err == nil || !strings.Contains(err.Error(), "native 'typed_test' expects 1 arguments, got 0") {
		t.Fatalf("error=%v", err)
	}
	if called {
		t.Fatal("native closure must not run after arity failure")
	}
}

func TestTypedNativeRejectsFewerThanMinimumVariadicArityBeforeInvocation(t *testing.T) {
	called := false
	sig := value.NativeSignature{
		Arity:      2,
		Variadic:   true,
		Params:     []value.ParamInfo{{TypeName: "int"}, {TypeName: "int"}},
		ReturnType: "void",
	}
	err := runWithTypedTestNative(t, "typed_test(1)", sig, &called)
	if err == nil || !strings.Contains(err.Error(), "native 'typed_test' expects at least 2 arguments, got 1") {
		t.Fatalf("error=%v", err)
	}
	if called {
		t.Fatal("native closure must not run after arity failure")
	}
}

func TestTypedNativeRejectsReferenceForOrdinaryParameterBeforeInvocation(t *testing.T) {
	called := false
	sig := value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{IsRef: false, TypeName: "int"}}, ReturnType: "void"}
	err := runWithTypedTestNative(t, "let n: int = 1\ntyped_test(ref n)", sig, &called)
	if err == nil || !strings.Contains(err.Error(), "expected int, got ref") {
		t.Fatalf("error=%v", err)
	}
	if called {
		t.Fatal("native closure must not run after contract failure")
	}
}

func TestTypedNativeExpandsFinalVariadicParameterMode(t *testing.T) {
	called := false
	sig := value.NativeSignature{
		Arity:      1,
		Variadic:   true,
		Params:     []value.ParamInfo{{IsRef: true, TypeName: "ref int"}},
		ReturnType: "void",
	}
	err := runWithTypedTestNative(t, "let n: int = 1\ntyped_test(ref n, n)", sig, &called)
	if err == nil || !strings.Contains(err.Error(), "argument 2: expected ref int") {
		t.Fatalf("error=%v", err)
	}
	if called {
		t.Fatal("native closure must not run after contract failure")
	}
}

func TestTypedNativeShallowCopiesOrdinaryComposite(t *testing.T) {
	source := "let values: int[] = [1]\ntyped_test(values)"
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.New().Compile(program)
	if err != nil {
		t.Fatal(err)
	}

	machine := New()
	machine.DefineNativeWithSignature("typed_test", value.NativeSignature{
		Arity:      1,
		Params:     []value.ParamInfo{{IsRef: false, TypeName: "int[]"}},
		ReturnType: "void",
	}, func(args []value.Value) value.Value {
		args[0].Obj.(*value.ObjArray).Elements[0] = value.NewInt(9)
		return value.NewNull()
	})
	if err := machine.Interpret(code); err != nil {
		t.Fatal(err)
	}
	global, ok := machine.GetGlobal("values")
	if !ok {
		t.Fatal("missing values global")
	}
	if got := global.Obj.(*value.ObjArray).Elements[0].AsInt; got != 1 {
		t.Fatalf("caller value=%d, want 1", got)
	}
}

func TestTypedMutatingBuiltinsPreserveSourceSyntax(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let values: int[] = [1]
append(values, 2)
let removed: int = pop(values)
test_report(length(values) * 10 + removed)`)
	testExpectedObject(t, 12, got)
}

func TestTypedDeleteMutatesCallerMap(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let mapping: map[string, int] = {"a": 1}
delete(mapping, "a")
test_report(length(keys(mapping)))`)
	testExpectedObject(t, 0, got)
}

func TestTypedJSONLoadsPopulatesCallerTarget(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let target: map[string, int] = {"old": 0}
let ok: bool = json_loads("{\"answer\":42}", target)
test_report(target["answer"])`)
	testExpectedObject(t, 42, got)
}

func TestTypedMutatingBuiltinResolvesCapturedTarget(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func make_mutator() -> func() -> int
    let values: int[] = [1]
    return func() -> int
        append(values, 2)
        let removed: int = pop(values)
        return length(values) * 10 + removed
    end
end
let mutate: func() -> int = make_mutator()
test_report(mutate())`)
	testExpectedObject(t, 12, got)
}

func TestTypedMutatingBuiltinsResolvePropertyTargets(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Collections
    values: int[]
    mapping: map[string, int]
end
let collections: Collections = Collections([1], {"a": 1})
append(collections.values, 2)
delete(collections.mapping, "a")
test_report(length(collections.values) * 10 + length(keys(collections.mapping)))`)
	testExpectedObject(t, 20, got)
}

func TestTypedMutatingBuiltinResolvesArrayIndexTarget(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let nested: int[][] = [[1]]
append(nested[0], 2)
let removed: int = pop(nested[0])
test_report(length(nested[0]) * 10 + removed)`)
	testExpectedObject(t, 12, got)
}

func TestTypedMutatingBuiltinResolvesMapIndexTarget(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let nested: map[string, int[]] = {"values": [1]}
append(nested["values"], 2)
let removed: int = pop(nested["values"])
test_report(length(nested["values"]) * 10 + removed)`)
	testExpectedObject(t, 12, got)
}
