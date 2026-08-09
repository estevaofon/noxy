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

func TestTypedJSONLoadsPopulatesMapIndexTarget(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let targets: map[string, map[string, int]] = {"slot": {"old": 0}}
let ok: bool = json_loads("{\"answer\":42}", targets["slot"])
test_report(targets["slot"]["answer"])`)
	testExpectedObject(t, 42, got)
}

func TestJSONLoadsUsesModuleFrameGlobals(t *testing.T) {
	machine := New()
	moduleTarget := value.NewMapWithData(map[string]value.Value{"old": value.NewInt(0)})
	moduleGlobals := map[string]value.Value{"target": moduleTarget}
	machine.currentFrame = &CallFrame{Globals: moduleGlobals}
	target := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_GLOBAL, Name: "target"}}

	jsonLoadsValue, ok := machine.GetGlobal("json_loads")
	if !ok {
		t.Fatal("missing json_loads native")
	}
	jsonLoads := jsonLoadsValue.Obj.(*value.ObjNative)
	if result := jsonLoads.Fn([]value.Value{value.NewString(`{"answer":42}`), target}); result.Type != value.VAL_BOOL || !result.AsBool {
		t.Fatal("module-frame global target was rejected")
	}
	got := moduleGlobals["target"].Obj.(*value.ObjMap).Data["answer"]
	if got.Type != value.VAL_INT || got.AsInt != 42 {
		t.Fatalf("module target=%v, want answer=42", moduleGlobals["target"])
	}
	if _, leaked := machine.GetGlobal("target"); leaked {
		t.Fatal("module target must not be written to shared globals")
	}
}

func TestPopulateTargetRejectsInvalidReferences(t *testing.T) {
	machine := New()
	instance := value.NewInstance(&value.ObjStruct{Name: "Holder", Fields: []string{"known"}})
	instance.Obj.(*value.ObjInstance).Fields["known"] = value.NewInt(1)
	tests := []struct {
		name string
		ref  *value.ObjRef
	}{
		{
			name: "missing global",
			ref:  &value.ObjRef{RefType: value.REF_GLOBAL, Name: "missing"},
		},
		{
			name: "missing property",
			ref:  &value.ObjRef{RefType: value.REF_PROPERTY, Container: instance, Name: "missing"},
		},
		{
			name: "property container",
			ref:  &value.ObjRef{RefType: value.REF_PROPERTY, Container: value.NewInt(1), Name: "missing"},
		},
		{
			name: "array bounds",
			ref: &value.ObjRef{
				RefType: value.REF_INDEX,
				Container: value.NewArray([]value.Value{
					value.NewInt(1),
				}),
				Index: value.NewInt(2),
			},
		},
		{
			name: "array index type",
			ref: &value.ObjRef{
				RefType: value.REF_INDEX,
				Container: value.NewArray([]value.Value{
					value.NewInt(1),
				}),
				Index: value.NewBool(true),
			},
		},
		{
			name: "map key",
			ref: &value.ObjRef{
				RefType:   value.REF_INDEX,
				Container: value.NewMap(),
				Index:     value.NewBool(true),
			},
		},
		{
			name: "missing map key",
			ref: &value.ObjRef{
				RefType:   value.REF_INDEX,
				Container: value.NewMap(),
				Index:     value.NewString("missing"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := value.Value{Type: value.VAL_REF, Obj: tt.ref}
			if populateTarget(machine, target, float64(42)) {
				t.Fatal("invalid reference target reported successful population")
			}
		})
	}
}

func TestPopulateTargetPropagatesNestedObjectFailure(t *testing.T) {
	inner := value.NewInstance(&value.ObjStruct{Name: "Inner", Fields: []string{"name"}})
	inner.Obj.(*value.ObjInstance).Fields["name"] = value.NewString("keep")
	outer := value.NewInstance(&value.ObjStruct{Name: "Outer", Fields: []string{"inner"}})
	outer.Obj.(*value.ObjInstance).Fields["inner"] = inner

	if populateTarget(New(), outer, map[string]interface{}{"inner": []interface{}{}}) {
		t.Fatal("nested object population failure reported success")
	}
	got := inner.Obj.(*value.ObjInstance).Fields["name"]
	if got.Type != value.VAL_OBJ || got.Obj != "keep" {
		t.Fatalf("nested field=%v, want unchanged string", got)
	}
}

func TestPopulateTargetRejectsIncompatibleScalarReplacementWithoutMutation(t *testing.T) {
	stored := value.NewString("keep")
	target := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}}

	if populateTarget(New(), target, float64(42)) {
		t.Fatal("incompatible scalar replacement reported success")
	}
	if stored.Type != value.VAL_OBJ || stored.Obj != "keep" {
		t.Fatalf("stored=%v, want unchanged string", stored)
	}
}

func TestPopulateTargetSupportsCompatibleAndNullScalarReplacement(t *testing.T) {
	t.Run("compatible string", func(t *testing.T) {
		stored := value.NewString("before")
		target := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}}
		if !populateTarget(New(), target, "after") {
			t.Fatal("compatible scalar replacement failed")
		}
		if stored.Type != value.VAL_OBJ || stored.Obj != "after" {
			t.Fatalf("stored=%v, want after", stored)
		}
	})

	t.Run("null remains dynamic", func(t *testing.T) {
		stored := value.NewNull()
		target := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}}
		if !populateTarget(New(), target, float64(42)) {
			t.Fatal("null target rejected dynamic scalar replacement")
		}
		if stored.Type != value.VAL_INT || stored.AsInt != 42 {
			t.Fatalf("stored=%v, want int 42", stored)
		}
	})
}

func TestTypedJSONLoadsAllowsNonNullAnyTargetToChangeType(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let target: any = "before"
json_loads("42", target)
test_report(target)`)
	testExpectedObject(t, 42, got)
}

func TestTypedJSONLoadsAllowsNonNullAnyStructFieldToChangeType(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Holder
    data: any
end
let target: Holder = Holder("before")
json_loads("{\"data\":42}", target)
test_report(target.data)`)
	testExpectedObject(t, 42, got)
}

func TestPopulateTargetRetainsCompatibleCompositeFields(t *testing.T) {
	outer := value.NewInstance(&value.ObjStruct{Name: "Outer", Fields: []string{"items", "metadata"}})
	fields := outer.Obj.(*value.ObjInstance).Fields
	fields["items"] = value.NewArray(nil)
	fields["metadata"] = value.NewMap()

	data := map[string]interface{}{
		"items":    []interface{}{float64(42)},
		"metadata": map[string]interface{}{"answer": float64(42)},
	}
	if !populateTarget(New(), outer, data) {
		t.Fatal("compatible composite fields were rejected")
	}
	items := fields["items"].Obj.(*value.ObjArray)
	if len(items.Elements) != 1 || items.Elements[0].Type != value.VAL_INT || items.Elements[0].AsInt != 42 {
		t.Fatalf("items=%v, want [42]", fields["items"])
	}
	metadata := fields["metadata"].Obj.(*value.ObjMap)
	if got := metadata.Data["answer"]; got.Type != value.VAL_INT || got.AsInt != 42 {
		t.Fatalf("metadata=%v, want answer=42", fields["metadata"])
	}
}

func TestPopulateTargetRejectsMalformedReferenceValue(t *testing.T) {
	target := value.Value{Type: value.VAL_REF, Obj: "not an ObjRef"}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("populateTarget panicked for malformed reference: %v", recovered)
		}
	}()
	if populateTarget(New(), target, float64(42)) {
		t.Fatal("malformed reference reported successful population")
	}
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

func TestTypedAppendPreservesReferenceValuedElements(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Vertex
    value: int
end
let first: Vertex = Vertex(1)
let second: Vertex = Vertex(2)
let existing: ref Vertex = ref second
let values: (ref Vertex)[] = []
append(values, ref first)
append(values, existing)
values[0].value = 41
values[1].value = 42
test_report(first.value + second.value)`)
	testExpectedObject(t, 83, got)
}

func TestTypedAppendStoresOrdinaryCompositeItemWithoutNativeCopy(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let item: int[] = [1]
let values: int[][] = []
append(values, item)
item[0] = 42
test_report(values[0][0])`)
	testExpectedObject(t, 42, got)
}

func TestUnrelatedWildcardImportKeepsBuiltinRuntimeContract(t *testing.T) {
	got := runTypedFunctionProgram(t, `
use sys select *
let values: int[] = [1]
append(values, 2)
test_report(length(values))`)
	testExpectedObject(t, 2, got)
}

func TestWildcardImportInstallsCollidingBuiltinNameAtRuntime(t *testing.T) {
	got := runTypedFunctionProgram(t, `
use http_client select *
test_report(delete)`)
	closure, ok := got.Obj.(*value.ObjClosure)
	if got.Type != value.VAL_FUNCTION || !ok || closure.Function.Name != "delete" {
		t.Fatalf("delete binding=%v, want imported function", got)
	}
}

func TestBuiltinCallBeforeLaterWildcardImportUsesRuntimeBuiltin(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let mapping: map[string, int] = {"a": 1}
delete(mapping, "a")
use http_client select *
test_report(length(keys(mapping)))`)
	testExpectedObject(t, 0, got)
}

func TestFunctionUsingLaterWildcardCollisionFailsWhenCalledBeforeImport(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
func remove(url: string) -> void
    delete(url)
end
remove("http://example.invalid")
use http_client select *`)
	if err == nil || !strings.Contains(err.Error(), "expects 2 arguments, got 1") {
		t.Fatalf("error=%v", err)
	}
}
