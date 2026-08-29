package vm

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
)

func TestIOModuleExposesObservableResultsAndPreservesLegacyCalls(t *testing.T) {
	path := strings.ReplaceAll(filepath.Join(t.TempDir(), "io-results.txt"), `\`, `\\`)
	got := runTypedFunctionProgram(t, fmt.Sprintf(`
use io
let observable_write: any = io.write_result
let observable_close: any = io.close_result
let legacy: io.File = io.open("%s", "w")
io.write(legacy, "legacy")
io.close(legacy)

let observable: io.File = io.open("%s", "a")
let written: any = io.write_result(observable, "é")
let closed: any = io.close_result(observable)
test_report(written.ok && written.value == 2 && written.failure.message == "" && closed.ok && closed.failure.message == "")`, path, path))
	testExpectedObject(t, true, got)
}

func TestJSONModuleExposesStrictObservableEncoding(t *testing.T) {
	source := "\nuse json\n" +
		"let values: any[] = [1, true, null]\n" +
		"let document: map[string, any] = {\"name\": \"Noxy\", \"values\": values}\n" +
		"let encoded: any = json.dumps_result(document)\n" +
		"let invalid: map[string, any] = {\"raw\": b\"abc\"}\n" +
		"let rejected: any = json.dumps_result(invalid)\n" +
		"test_report(encoded.ok && encoded.value != \"\" && encoded.failure.message == \"\" && !rejected.ok && rejected.value == null && rejected.failure.message != \"\")"
	testExpectedObject(t, true, runTypedFunctionProgram(t, source))
}

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

func TestReferenceTargetMetadataPreservesForwardedReferenceIdentity(t *testing.T) {
	source := `
let value: int = 1
let forwarded: ref int = ref value
typed_test(forwarded)`
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
	var received *value.ObjRef
	machine.DefineNativeWithSignature("typed_test", value.NativeSignature{
		Arity:      1,
		Params:     []value.ParamInfo{{IsRef: true, TypeName: "ref int"}},
		ReturnType: "void",
	}, func(args []value.Value) value.Value {
		received, _ = args[0].Obj.(*value.ObjRef)
		return value.NewNull()
	})
	if err := machine.Interpret(code); err != nil {
		t.Fatal(err)
	}
	forwarded, ok := machine.GetGlobal("forwarded")
	if !ok {
		t.Fatal("missing forwarded reference")
	}
	stored, ok := forwarded.Obj.(*value.ObjRef)
	if forwarded.Type != value.VAL_REF || !ok || stored == nil {
		t.Fatalf("forwarded=%v, want reference", forwarded)
	}
	if received != stored {
		t.Fatal("target metadata marker copied or rebound the forwarded reference")
	}
	targetType := stored.TargetType.Load()
	if targetType == nil || targetType.Kind != value.TYPE_INT {
		t.Fatalf("target metadata=%v, want int", targetType)
	}
}

func TestReferenceTargetMetadataSupportsLongConstantIndex(t *testing.T) {
	var source strings.Builder
	source.WriteString("func stress() -> void\n    let value: int = 1\n")
	for i := 0; i < 260; i++ {
		source.WriteString("    ref value\n")
	}
	source.WriteString("end\nstress()\ntest_report(42)")
	testExpectedObject(t, 42, runTypedFunctionProgram(t, source.String()))
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
	if got := global.Obj.(*value.ObjArray).Elements[0].Int(); got != 1 {
		t.Fatalf("caller value=%d, want 1", got)
	}
}

func TestTypedMutatingBuiltinsPreserveSourceSyntax(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let values: int[] = [1]
append(ref values, 2)
let removed: int = pop(ref values)
test_report(length(values) * 10 + removed)`)
	testExpectedObject(t, 12, got)
}

func TestTypedDeleteMutatesCallerMap(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let mapping: map[string, int] = {"a": 1}
delete(ref mapping, "a")
test_report(length(keys(mapping)))`)
	testExpectedObject(t, 0, got)
}

func TestTypedJSONLoadsPopulatesCallerTarget(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let target: map[string, int] = {"old": 0}
let ok: bool = json_loads("{\"answer\":42}", ref target)
test_report(target["answer"])`)
	testExpectedObject(t, 42, got)
}

func TestTypedJSONLoadsPopulatesMapIndexTarget(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let targets: map[string, map[string, int]] = {"slot": {"old": 0}}
let ok: bool = json_loads("{\"answer\":42}", ref targets["slot"])
test_report(targets["slot"]["answer"])`)
	testExpectedObject(t, 42, got)
}

func TestJSONLoadsUsesModuleFrameGlobals(t *testing.T) {
	machine := New()
	moduleTarget := value.NewMapWithData(map[string]value.Value{"old": value.NewInt(0)})
	moduleEnvironment := value.NewGlobalEnvironmentFrom(map[string]value.Value{"target": moduleTarget}, machine.shared.Root)
	machine.currentFrame = &CallFrame{Environment: moduleEnvironment}
	target := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_GLOBAL, Name: "target", GlobalOwner: moduleEnvironment}}

	jsonLoadsValue, ok := machine.GetGlobal("json_loads")
	if !ok {
		t.Fatal("missing json_loads native")
	}
	jsonLoads := jsonLoadsValue.Obj.(*value.ObjNative)
	result, err := jsonLoads.Invoke(machine, []value.Value{value.NewString(`{"answer":42}`), target})
	if err != nil || result.Type != value.VAL_BOOL || !result.Bool() {
		t.Fatal("module-frame global target was rejected")
	}
	moduleTarget, _ = moduleEnvironment.GetLocal("target")
	got := requireTestMapValue(t, moduleTarget.Obj.(*value.ObjMap), "answer")
	if got.Type != value.VAL_INT || got.Int() != 42 {
		t.Fatalf("module target=%v, want answer=42", moduleTarget)
	}
	if _, leaked := machine.GetGlobal("target"); leaked {
		t.Fatal("module target must not be written to shared globals")
	}
}

func TestPopulateTargetRejectsInvalidReferences(t *testing.T) {
	machine := New()
	missingEnvironment := value.NewGlobalEnvironment(nil)
	instance := value.NewInstance(&value.ObjStruct{Name: "Holder", Fields: []string{"known"}})
	instance.Obj.(*value.ObjInstance).MustSet("known", value.NewInt(1))
	tests := []struct {
		name string
		ref  *value.ObjRef
	}{
		{
			name: "missing global",
			ref:  &value.ObjRef{RefType: value.REF_GLOBAL, Name: "missing", GlobalOwner: missingEnvironment},
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
	inner.Obj.(*value.ObjInstance).MustSet("name", value.NewString("keep"))
	outer := value.NewInstance(&value.ObjStruct{Name: "Outer", Fields: []string{"inner"}})
	outer.Obj.(*value.ObjInstance).MustSet("inner", inner)

	if populateTarget(New(), outer, map[string]interface{}{"inner": []interface{}{}}) {
		t.Fatal("nested object population failure reported success")
	}
	got := inner.Obj.(*value.ObjInstance).Field("name")
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

	t.Run("marked dynamic null", func(t *testing.T) {
		stored := value.NewNull()
		ref := &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}
		ref.JSONDynamic.Store(true)
		target := value.Value{Type: value.VAL_REF, Obj: ref}
		if !populateTarget(New(), target, float64(42)) {
			t.Fatal("null target rejected dynamic scalar replacement")
		}
		if stored.Type != value.VAL_INT || stored.Int() != 42 {
			t.Fatalf("stored=%v, want int 42", stored)
		}
	})

	t.Run("untyped null is conservative", func(t *testing.T) {
		stored := value.NewNull()
		target := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}}
		if populateTarget(New(), target, float64(42)) {
			t.Fatal("untyped null target accepted an unprovable replacement")
		}
		if stored.Type != value.VAL_NULL {
			t.Fatalf("stored=%v, want unchanged null", stored)
		}
	})
}

func TestTypedJSONLoadsAllowsNonNullAnyTargetToChangeType(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let target: any = "before"
json_loads("42", ref target)
test_report(target)`)
	testExpectedObject(t, 42, got)
}

func TestTypedJSONLoadsAllowsNonNullAnyStructFieldToChangeType(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Holder
    data: any
end
let target: Holder = Holder("before")
json_loads("{\"data\":42}", ref target)
test_report(target.data)`)
	testExpectedObject(t, 42, got)
}

func TestTypedJSONLoadsRejectsInvalidArrayElementAtomically(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let target: int[] = [1, 2]
let ok: bool = json_loads("[10,\"bad\"]", ref target)
if ok then
    test_report(999)
else
    test_report(target[0] * 10 + target[1])
end`)
	testExpectedObject(t, 12, got)
}

func TestTypedJSONLoadsParsesIntegersExactlyAndRejectsOverflow(t *testing.T) {
	t.Run("maximum int", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let target: int[] = [1]
let ok: bool = json_loads("[9223372036854775807]", ref target)
if ok then
    test_report(target[0])
else
    test_report(0)
end`)
		if got.Type != value.VAL_INT || got.Int() != int64(9223372036854775807) {
			t.Fatalf("got=%v, want MaxInt64", got)
		}
	})

	t.Run("overflow leaves target unchanged", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let target: int[] = [7]
let ok: bool = json_loads("[9223372036854775808]", ref target)
if ok then
    test_report(999)
else
    test_report(target[0])
end`)
		testExpectedObject(t, 7, got)
	})
}

func TestTypedJSONLoadsRejectsInvalidMapValueAtomically(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let target: map[string, int] = {"a": 1, "b": 2}
let ok: bool = json_loads("{\"a\":10,\"b\":\"bad\"}", ref target)
if ok then
    test_report(999)
else
    test_report(target["a"] * 10 + target["b"])
end`)
	testExpectedObject(t, 12, got)
}

func TestTypedJSONLoadsPreservesReferenceElementTypeForNullSlot(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let target: (ref int)?[] = [null]
let ok: bool = json_loads("[\"bad\"]", ref target)
if ok then
    test_report(999)
elif target[0] == null then
    test_report(42)
else
    test_report(0)
end`)
	testExpectedObject(t, 42, got)
}

// Alvo `ref any` nulo (campo) chega ao json_loads como null encaminhado: o
// marcador OP_MARK_REF_JSON_DYNAMIC deixa o null passar (como o marcador de
// tipo-alvo ja fazia) e o json_loads devolve false — nao ha slot por tras
// para preencher. Antes do encaminhamento, o slot era preenchido com o
// payload cru; sem o passthrough, era "dynamic target marker requires a
// reference" em runtime.
func TestTypedJSONLoadsIntoNullRefAnyFieldReturnsFalse(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Holder
    child: ref any?
end
let h: Holder = Holder(null)
let ok: bool = json_loads("{\"a\": 1}", h.child)
if !ok && h.child == null then
    test_report(42)
else
    test_report(0)
end`)
	testExpectedObject(t, 42, got)
}

func TestTypedJSONLoadsAcceptsCompatibleReferenceElementPayloads(t *testing.T) {
	// Issue #50 Parte 3 (opcao a): slot `ref T` nulo recebe uma CELULA heap
	// nova com o referente + ref para ela — o analogo de `let novo = T;
	// slot = ref novo`. A sonda `type(viz)` distingue ref ("ref") de valor
	// cru (erro do marcador); `viz` e ligado a parte para reusar no *viz
	// abaixo, mas `type(target[0])` tambem devolveria "ref" direto (R2: o
	// indice nunca e lido implicitamente).
	t.Run("null ref slot gets a fresh referent cell", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func le(r: ref int?) -> int
    if r != null then
        return *r
    end
    return -1
end
let target: (ref int)?[] = [null]
let ok: bool = json_loads("[42]", ref target)
let viz: ref int? = target[0]
if ok && viz != null && type(viz) == "ref" && *viz == 42 && le(target[0]) == 42 then
    test_report(42)
else
    test_report(999)
end`)
		testExpectedObject(t, 42, got)
	})

	t.Run("json null clears reference slot", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let backing: int = 7
let target: (ref int)?[] = [ref backing]
let ok: bool = json_loads("[null]", ref target)
if ok then
    if target[0] == null then
        test_report(backing)
    else
        test_report(998)
    end
else
    test_report(999)
end`)
		testExpectedObject(t, 7, got)
	})

	t.Run("json null creates reference slot", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let target: (ref int)[] = []
let ok: bool = json_loads("[null]", ref target)
if ok then
    if target[0] == null then
        test_report(length(target))
    else
        test_report(998)
    end
else
    test_report(999)
end`)
		testExpectedObject(t, 1, got)
	})
}

func TestTypedJSONLoadsRejectsPartialStructAtomically(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Pair
    a: int
    b: int
end
let target: Pair = Pair(1, 2)
let ok: bool = json_loads("{\"a\":10,\"b\":\"bad\"}", ref target)
if ok then
    test_report(999)
else
    test_report(target.a * 10 + target.b)
end`)
	testExpectedObject(t, 12, got)
}

func TestTypedJSONLoadsRequiresAllFieldsWhenBuildingNewStruct(t *testing.T) {
	t.Run("missing field", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Pair
    a: int
    b: int
end
let target: (ref Pair)[] = []
let ok: bool = json_loads("[{\"a\":1}]", ref target)
if ok then
    test_report(999)
else
    test_report(length(target))
end`)
		testExpectedObject(t, 0, got)
	})

	t.Run("complete new struct", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Pair
    a: int
    b: int
end
let target: (ref Pair)[] = []
let ok: bool = json_loads("[{\"a\":3,\"b\":4}]", ref target)
if ok then
    test_report(target[0].a * 10 + target[0].b)
else
    test_report(999)
end`)
		testExpectedObject(t, 34, got)
	})

	t.Run("partial existing struct preserves omitted field", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Pair
    a: int
    b: int
end
let target: Pair = Pair(1, 2)
let ok: bool = json_loads("{\"a\":3}", ref target)
if ok then
    test_report(target.a * 10 + target.b)
else
    test_report(999)
end`)
		testExpectedObject(t, 32, got)
	})
}

// Spec §2.4 fase 2: o null do documento so entra em slot anulavel (T?).
func TestTypedJSONLoadsAcceptsNullForStructSchema(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "target",
			source: `
struct Vertex
    value: int
end
let target: Vertex? = Vertex(1)
let ok: bool = json_loads("null", ref target)
if ok then
    if target == null then test_report(1) else test_report(998) end
else
    test_report(999)
end`,
		},
		{
			name: "array element",
			source: `
struct Vertex
    value: int
end
let target: Vertex?[] = [Vertex(1)]
let ok: bool = json_loads("[null]", ref target)
if ok then
    if target[0] == null then test_report(length(target)) else test_report(998) end
else
    test_report(999)
end`,
		},
		{
			name: "map value",
			source: `
struct Vertex
    value: int
end
let target: map[string, Vertex?] = {"item": Vertex(1)}
let ok: bool = json_loads("{\"item\":null}", ref target)
if ok then
    if target["item"] == null then test_report(1) else test_report(998) end
else
    test_report(999)
end`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testExpectedObject(t, 1, runTypedFunctionProgram(t, tt.source))
		})
	}
}

func TestTypedJSONLoadsReplacesNonNullAnyComposite(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let target: any = [1]
let ok: bool = json_loads("42", ref target)
if ok then
    test_report(target)
else
    test_report(999)
end`)
	testExpectedObject(t, 42, got)
}

func TestTypedJSONLoadsPreservesCompositeIdentityForAliases(t *testing.T) {
	t.Run("struct", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Pair
    a: int
    b: int
end
let target: Pair = Pair(1, 2)
let alias: ref Pair = ref target
let ok: bool = json_loads("{\"a\":3,\"b\":4}", ref target)
if ok then
    test_report(alias.a * 10 + alias.b)
else
    test_report(999)
end`)
		testExpectedObject(t, 34, got)
	})

	t.Run("array", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let target: int[] = [1, 2]
let alias: ref int[] = ref target
let ok: bool = json_loads("[3,4]", ref target)
if ok then
    test_report(alias[0] * 10 + alias[1])
else
    test_report(999)
end`)
		testExpectedObject(t, 34, got)
	})
}

func TestPopulateTargetRetainsCompatibleCompositeFields(t *testing.T) {
	outer := value.NewInstance(&value.ObjStruct{Name: "Outer", Fields: []string{"items", "metadata"}})
	fields := outer.Obj.(*value.ObjInstance)
	fields.MustSet("items", value.NewArray(nil))
	fields.MustSet("metadata", value.NewMap())

	data := map[string]interface{}{
		"items":    []interface{}{float64(42)},
		"metadata": map[string]interface{}{"answer": float64(42)},
	}
	if !populateTarget(New(), outer, data) {
		t.Fatal("compatible composite fields were rejected")
	}
	items := fields.Field("items").Obj.(*value.ObjArray)
	if len(items.Elements) != 1 || items.Elements[0].Type != value.VAL_INT || items.Elements[0].Int() != 42 {
		t.Fatalf("items=%v, want [42]", fields.Field("items"))
	}
	metadata := fields.Field("metadata").Obj.(*value.ObjMap)
	if got := requireTestMapValue(t, metadata, "answer"); got.Type != value.VAL_INT || got.Int() != 42 {
		t.Fatalf("metadata=%v, want answer=42", fields.Field("metadata"))
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
        append(ref values, 2)
        let removed: int = pop(ref values)
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
append(ref collections.values, 2)
delete(ref collections.mapping, "a")
test_report(length(collections.values) * 10 + length(keys(collections.mapping)))`)
	testExpectedObject(t, 20, got)
}

func TestTypedMutatingBuiltinResolvesArrayIndexTarget(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let nested: int[][] = [[1]]
append(ref nested[0], 2)
let removed: int = pop(ref nested[0])
test_report(length(nested[0]) * 10 + removed)`)
	testExpectedObject(t, 12, got)
}

func TestTypedMutatingBuiltinResolvesMapIndexTarget(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let nested: map[string, int[]] = {"values": [1]}
append(ref nested["values"], 2)
let removed: int = pop(ref nested["values"])
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
append(ref values, ref first)
append(ref values, existing)
values[0].value = 41
values[1].value = 42
test_report(first.value + second.value)`)
	testExpectedObject(t, 83, got)
}

// Contrato CoW (spec 2026-08-16): o item guardado por append é um valor
// independente — mutar o original depois não altera o que foi guardado
// (antes desta mudança o esperado era 42, com o item compartilhado).
func TestTypedAppendStoresIndependentCompositeItem(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let item: int[] = [1]
let values: int[][] = []
append(ref values, item)
item[0] = 42
test_report(values[0][0])`)
	testExpectedObject(t, 1, got)
}

func TestDynamicAppendSpecializesItemModeFromTargetType(t *testing.T) {
	t.Run("reject reference for plain element", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let values: int[] = [1]
let item: int = 2
invoke(ref values, ref item)
test_report(length(values))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("reject incompatible plain value", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let values: int[] = [1]
invoke(ref values, "bad")
test_report(length(values))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("reject plain value for reference element", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Vertex
    value: int
end
let invoke: any = append
let values: (ref Vertex)[] = []
let item: Vertex = Vertex(2)
invoke(ref values, item)
test_report(length(values))`)
		testExpectedObject(t, 0, got)
	})

	t.Run("reject reference to incompatible value", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Vertex
    value: int
end
let invoke: any = append
let values: (ref Vertex)[] = []
let item: int = 2
invoke(ref values, ref item)
test_report(length(values))`)
		testExpectedObject(t, 0, got)
	})

	t.Run("reject nested reference to incompatible value", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let targets: ((ref int)[])[] = []
let text: string = "bad"
let wrong: (ref string)[] = [ref text]
invoke(ref targets, wrong)
test_report(length(targets))`)
		testExpectedObject(t, 0, got)
	})

	t.Run("accept plain value", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let values: int[] = [1]
invoke(ref values, 2)
test_report(values[1])`)
		testExpectedObject(t, 2, got)
	})

	t.Run("accept and retain reference value", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Vertex
    value: int
end
let invoke: any = append
let values: (ref Vertex)[] = []
let item: Vertex = Vertex(2)
invoke(ref values, ref item)
values[0].value = 42
test_report(item.value)`)
		testExpectedObject(t, 42, got)
	})

	t.Run("accept null reference element", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let values: (ref int)?[] = []
invoke(ref values, null)
test_report(length(values))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("forward existing target reference", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let values: int[] = [1]
let forwarded: ref int[] = ref values
invoke(forwarded, 2)
test_report(length(values))`)
		testExpectedObject(t, 2, got)
	})
}

func TestTypedAppendAcceptsExactFunctionElement(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func identity(value: int) -> int
    return value
end
let values: (func(int) -> int)[] = [identity]
append(ref values, identity)
test_report(length(values))`)
	testExpectedObject(t, 2, got)
}

func TestDynamicAppendValidatesCallableElementTypes(t *testing.T) {
	t.Run("reject nested non-callable array", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func identity(value: int) -> int
    return value
end
let invoke: any = append
let seed: (func(int) -> int)[] = [identity]
let targets: ((func(int) -> int)[])[] = [seed]
pop(ref targets)
let wrong: int[] = [1]
invoke(ref targets, wrong)
test_report(length(targets))`)
		testExpectedObject(t, 0, got)
	})

	t.Run("reject incompatible exact signature", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func text(value: string) -> int
    return 1
end
func identity(value: int) -> int
    return value
end
let invoke: any = append
let targets: (func(int) -> int)[] = [identity]
pop(ref targets)
invoke(ref targets, text)
test_report(length(targets))`)
		testExpectedObject(t, 0, got)
	})

	t.Run("reject incompatible return and reference mode", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func identity(value: int) -> int
    return value
end
func wrong_return(value: int) -> string
    return "bad"
end
func wrong_mode(value: ref int) -> int
    return *value
end
let invoke: any = append
let targets: (func(int) -> int)[] = [identity]
pop(ref targets)
invoke(ref targets, wrong_return)
invoke(ref targets, wrong_mode)
test_report(length(targets))`)
		testExpectedObject(t, 0, got)
	})

	t.Run("accept callable in bare function array", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func identity(value: int) -> int
    return value
end
let invoke: any = append
let targets: func[] = [identity]
pop(ref targets)
invoke(ref targets, identity)
test_report(length(targets))`)
		testExpectedObject(t, 1, got)
	})
}

func TestTypedAndDynamicAppendValidateChannelElementTypes(t *testing.T) {
	t.Run("exact typed channel", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let channel: chan int = make_chan(1)
let targets: (chan int)[] = [channel]
pop(ref targets)
append(ref targets, channel)
test_report(length(targets))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("dynamic incompatible typed channel", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let wrong: chan string = make_chan(1)
let seed: chan int = make_chan(1)
let targets: (chan int)[] = [seed]
pop(ref targets)
invoke(ref targets, wrong)
test_report(length(targets))`)
		testExpectedObject(t, 0, got)
	})

	t.Run("assignment annotates previously untyped channel", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let raw: any = make_chan(1)
let typed: chan int = make_chan(1)
typed = raw
let seed: chan int = make_chan(1)
let targets: (chan int)[] = [seed]
pop(ref targets)
invoke(ref targets, typed)
test_report(length(targets))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("conflicting assignment fails", func(t *testing.T) {
		err := runTypedFunctionProgramError(t, `
let text: chan string = make_chan(1)
let dynamic: any = text
let integer: chan int = make_chan(1)
integer = dynamic`)
		if err == nil || !strings.Contains(err.Error(), "expected chan int, got chan string") {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("array declaration propagates channel metadata", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let raw: any = make_chan(1)
let channels: (chan int)[] = [raw]
let seed: (chan int)[] = [make_chan(1)]
let targets: ((chan int)[])[] = [seed]
pop(ref targets)
invoke(ref targets, channels)
test_report(length(targets))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("exact append annotates channel expression", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let seed: chan int = make_chan(1)
let targets: (chan int)[] = [seed]
pop(ref targets)
append(ref targets, make_chan(1))
test_report(length(targets))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("exact parameter propagates channel metadata", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func store(channel: chan int, targets: ref (chan int)[]) -> void
    let invoke: any = append
    invoke(targets, channel)
end
let seed: chan int = make_chan(1)
let targets: (chan int)[] = [seed]
pop(ref targets)
store(make_chan(1), ref targets)
test_report(length(targets))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("return propagates channel metadata", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func create() -> chan int
    return make_chan(1)
end
let invoke: any = append
let seed: chan int = make_chan(1)
let targets: (chan int)[] = [seed]
pop(ref targets)
invoke(ref targets, create())
test_report(length(targets))`)
		testExpectedObject(t, 1, got)
	})
}

func TestRuntimeCallableValidationProjectsOnlyCompleteNativeSignatures(t *testing.T) {
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	expected := &value.RuntimeTypeInfo{
		Kind:       value.TYPE_CALLABLE,
		Params:     []*value.RuntimeTypeInfo{integer},
		ParamIsRef: []bool{false},
		Return:     integer,
	}
	typed := value.NewNativeWithSignature("identity", value.NativeSignature{
		Arity:      1,
		Params:     []value.ParamInfo{{TypeName: "int"}},
		ReturnType: "int",
	}, func(args []value.Value) value.Value { return args[0] })
	if !runtimeCallableMatchesType(typed, expected) {
		t.Fatal("complete typed native did not satisfy exact callable schema")
	}
	untyped := value.NewNative("identity", func(args []value.Value) value.Value { return args[0] })
	if runtimeCallableMatchesType(untyped, expected) {
		t.Fatal("untyped native satisfied exact callable schema")
	}
	wrongMode := value.NewNativeWithSignature("identity", value.NativeSignature{
		Arity:      1,
		Params:     []value.ParamInfo{{IsRef: true, TypeName: "ref int"}},
		ReturnType: "int",
	}, func(args []value.Value) value.Value { return args[0] })
	if runtimeCallableMatchesType(wrongMode, expected) {
		t.Fatal("native with incompatible reference mode satisfied exact callable schema")
	}
}

func TestRuntimeCallableValidationMatchesExactCallableArraySpelling(t *testing.T) {
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	callback := &value.RuntimeTypeInfo{
		Kind:       value.TYPE_CALLABLE,
		Params:     []*value.RuntimeTypeInfo{integer},
		ParamIsRef: []bool{false},
		Return:     integer,
	}
	callbacks := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: callback}
	expected := &value.RuntimeTypeInfo{
		Kind:       value.TYPE_CALLABLE,
		Params:     []*value.RuntimeTypeInfo{callbacks},
		ParamIsRef: []bool{false},
		Return:     callbacks,
	}
	typed := value.NewNativeWithSignature("callbacks", value.NativeSignature{
		Arity:      1,
		Params:     []value.ParamInfo{{TypeName: "(func(int) -> int)[]"}},
		ReturnType: "(func(int) -> int)[]",
	}, func(args []value.Value) value.Value { return args[0] })
	if !runtimeCallableMatchesType(typed, expected) {
		t.Fatal("typed native callable-array signature did not match canonical type spelling")
	}
}

func TestDynamicAppendRequiresInvariantExactCallableSchema(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "any parameter does not accept concrete declaration",
			source: `
func seed(value: any) -> int return 1 end
func wrong(value: int) -> int return value end
let invoke: any = append
let target: (func(any) -> int)[] = [seed]
pop(ref target)
invoke(ref target, wrong)
test_report(length(target))`,
		},
		{
			name: "concrete parameter does not accept any declaration",
			source: `
func seed(value: int) -> int return value end
func wrong(value: any) -> int return 1 end
let invoke: any = append
let target: (func(int) -> int)[] = [seed]
pop(ref target)
invoke(ref target, wrong)
test_report(length(target))`,
		},
		{
			name: "any return does not accept concrete declaration",
			source: `
func seed() -> any return 1 end
func wrong() -> int return 1 end
let invoke: any = append
let target: (func() -> any)[] = [seed]
pop(ref target)
invoke(ref target, wrong)
test_report(length(target))`,
		},
		{
			name: "concrete return does not accept any declaration",
			source: `
func seed() -> int return 1 end
func wrong() -> any return 1 end
let invoke: any = append
let target: (func() -> int)[] = [seed]
pop(ref target)
invoke(ref target, wrong)
test_report(length(target))`,
		},
		{
			name: "fixed array parameter size is exact",
			source: `
func seed(values: int[4]) -> int return length(values) end
func wrong(values: int[5]) -> int return length(values) end
let invoke: any = append
let target: (func(int[4]) -> int)[] = [seed]
pop(ref target)
invoke(ref target, wrong)
test_report(length(target))`,
		},
		{
			name: "fixed array return size is exact",
			source: `
func seed() -> int[4]
    let values: int[4]
    return values
end
func wrong() -> int[5]
    let values: int[5]
    return values
end
let invoke: any = append
let target: (func() -> int[4])[] = [seed]
pop(ref target)
invoke(ref target, wrong)
test_report(length(target))`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testExpectedObject(t, 0, runTypedFunctionProgram(t, tt.source))
		})
	}
}

func TestRuntimeCallableValidationIsInvariantAndFailClosed(t *testing.T) {
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	dynamic := &value.RuntimeTypeInfo{Kind: value.TYPE_ANY}
	fixedFour := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: integer, Size: 4}
	tests := []struct {
		name      string
		expected  *value.RuntimeTypeInfo
		signature value.NativeSignature
		fn        value.NativeFunc
		want      bool
	}{
		{
			name:      "matching any parameter and return",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{dynamic}, ParamIsRef: []bool{false}, Return: dynamic},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "any"}}, ReturnType: "any"},
			fn:        func(args []value.Value) value.Value { return args[0] },
			want:      true,
		},
		{
			name:      "any parameter rejects concrete native declaration",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{dynamic}, ParamIsRef: []bool{false}, Return: integer},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int"}}, ReturnType: "int"},
			fn:        func(args []value.Value) value.Value { return args[0] },
		},
		{
			name:      "concrete parameter rejects any native declaration",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{integer}, ParamIsRef: []bool{false}, Return: integer},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "any"}}, ReturnType: "int"},
			fn:        func(args []value.Value) value.Value { return args[0] },
		},
		{
			name:      "any return rejects concrete native declaration",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{integer}, ParamIsRef: []bool{false}, Return: dynamic},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int"}}, ReturnType: "int"},
			fn:        func(args []value.Value) value.Value { return args[0] },
		},
		{
			name:      "concrete return rejects any native declaration",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{integer}, ParamIsRef: []bool{false}, Return: integer},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int"}}, ReturnType: "any"},
			fn:        func(args []value.Value) value.Value { return args[0] },
		},
		{
			name:      "matching fixed array size",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{fixedFour}, ParamIsRef: []bool{false}, Return: fixedFour},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int[4]"}}, ReturnType: "int[4]"},
			fn:        func(args []value.Value) value.Value { return args[0] },
			want:      true,
		},
		{
			name:      "wrong fixed array size",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{fixedFour}, ParamIsRef: []bool{false}, Return: fixedFour},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int[5]"}}, ReturnType: "int[4]"},
			fn:        func(args []value.Value) value.Value { return args[0] },
		},
		{
			name:      "wrong fixed array return size",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{fixedFour}, ParamIsRef: []bool{false}, Return: fixedFour},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int[4]"}}, ReturnType: "int[5]"},
			fn:        func(args []value.Value) value.Value { return args[0] },
		},
		{
			name:      "nil native function",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{integer}, ParamIsRef: []bool{false}, Return: integer},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int"}}, ReturnType: "int"},
		},
		{
			name:      "partial parameter schema",
			expected:  &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{nil}, ParamIsRef: []bool{false}, Return: integer},
			signature: value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "unknown"}}, ReturnType: "int"},
			fn:        func(args []value.Value) value.Value { return args[0] },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			native := value.NewNativeWithSignature("typed", tt.signature, tt.fn)
			if got := runtimeCallableMatchesType(native, tt.expected); got != tt.want {
				t.Fatalf("runtimeCallableMatchesType=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestStructConstructorExactAssignmentAndCallRemainSupported(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Box
    value: int
end
let make_box: func(int) -> Box = Box
let made: Box = make_box(42)
test_report(made.value)`)
	testExpectedObject(t, 42, got)
}

func TestStructConstructorsSatisfyCallableCollectionSchemas(t *testing.T) {
	t.Run("exact array", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Box
    value: int
end
let makers: (func(int) -> Box)[] = [Box]
let made: Box = makers[0](42)
test_report(made.value)`)
		testExpectedObject(t, 42, got)
	})

	t.Run("exact map", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Box
    value: int
end
let makers: map[string, func(int) -> Box] = {"box": Box}
let made: Box = makers["box"](42)
test_report(made.value)`)
		testExpectedObject(t, 42, got)
	})

	t.Run("bare array", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Box
    value: int
end
let makers: func[] = [Box]
let made: any = makers[0](42)
test_report(made.value)`)
		testExpectedObject(t, 42, got)
	})

	t.Run("bare map", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Box
    value: int
end
let makers: map[string, func] = {"box": Box}
let made: any = makers["box"](42)
test_report(made.value)`)
		testExpectedObject(t, 42, got)
	})
}

func TestStructConstructorsSatisfyExactAndDynamicAppendSchemas(t *testing.T) {
	t.Run("exact append", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Box
    value: int
end
func make_box(value: int) -> Box
    return Box(value)
end
let makers: (func(int) -> Box)[] = [make_box]
pop(ref makers)
append(ref makers, Box)
test_report(length(makers))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("dynamic matching and nominal return mismatch", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Box
    value: int
end
struct Other
    value: int
end
func make_box(value: int) -> Box
    return Box(value)
end
let makers: (func(int) -> Box)[] = [make_box]
pop(ref makers)
let invoke: any = append
invoke(ref makers, Box)
invoke(ref makers, Other)
test_report(length(makers))`)
		testExpectedObject(t, 1, got)
	})
}

func TestStructConstructorCallableSchemaPreservesReferenceFields(t *testing.T) {
	t.Run("nested struct reference", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Node
    value: int
end
struct Holder
    node: ref Node
end
let makers: (func(ref Node) -> Holder)[] = [Holder]
let node: Node = Node(42)
let made: Holder = makers[0](ref node)
test_report(made.node.value)`)
		testExpectedObject(t, 42, got)
	})

	t.Run("recursive struct reference", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Node
    value: int
    next: ref Node?
end
let makers: (func(int, ref Node?) -> Node)[] = [Node]
let made: Node = makers[0](42, null)
test_report(made.value)`)
		testExpectedObject(t, 42, got)
	})
}

func TestRuntimeCallableValidationRejectsMalformedStructConstructors(t *testing.T) {
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	boxReturn := &value.RuntimeTypeInfo{Kind: value.TYPE_STRUCT, Name: "Box", Fields: map[string]*value.RuntimeTypeInfo{"value": integer}}
	expected := &value.RuntimeTypeInfo{
		Kind:       value.TYPE_CALLABLE,
		Params:     []*value.RuntimeTypeInfo{integer},
		ParamIsRef: []bool{false},
		Return:     boxReturn,
	}
	bare := &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, CallableBare: true}
	constructor := func(schema *value.RuntimeTypeInfo) value.Value {
		return value.Value{Type: value.VAL_OBJ, Obj: &value.ObjStruct{Name: "Box", Fields: []string{"value"}, ConstructorType: schema}}
	}
	malformed := []struct {
		name   string
		actual value.Value
	}{
		{name: "wrong object", actual: value.Value{Type: value.VAL_OBJ, Obj: "not a struct"}},
		{name: "typed nil", actual: value.Value{Type: value.VAL_OBJ, Obj: (*value.ObjStruct)(nil)}},
		{name: "missing schema", actual: value.NewStruct("Box", []string{"value"})},
		{name: "wrong schema kind", actual: constructor(integer)},
		{name: "bare constructor schema", actual: constructor(&value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, CallableBare: true})},
		{name: "nil return", actual: constructor(&value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{integer}, ParamIsRef: []bool{false}})},
		{name: "wrong nominal return", actual: constructor(&value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{integer}, ParamIsRef: []bool{false}, Return: &value.RuntimeTypeInfo{Kind: value.TYPE_STRUCT, Name: "Other"}})},
		{name: "wrong arity", actual: constructor(&value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Return: boxReturn})},
		{name: "wrong mode length", actual: constructor(&value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, Params: []*value.RuntimeTypeInfo{integer}, Return: boxReturn})},
	}
	for _, tt := range malformed {
		t.Run(tt.name, func(t *testing.T) {
			if runtimeCallableMatchesType(tt.actual, bare) || runtimeCallableMatchesType(tt.actual, expected) {
				t.Fatalf("malformed constructor %v satisfied callable schema", tt.actual)
			}
		})
	}
}

func TestLocalStructConstructorsPublishScopedCallableSchemas(t *testing.T) {
	t.Run("matching and nominal mismatch", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func run() -> int
    struct Local
        value: int
    end
    struct Other
        value: int
    end
    let makers: (func(int) -> Local)[] = [Local]
    pop(ref makers)
    let invoke: any = append
    invoke(ref makers, Local)
    invoke(ref makers, Other)
    return length(makers)
end
test_report(run())`)
		testExpectedObject(t, 1, got)
	})

	t.Run("bare callable", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func run() -> int
    struct Local
        value: int
    end
    let makers: func[] = [Local]
    return makers[0](42).value
end
test_report(run())`)
		testExpectedObject(t, 42, got)
	})

	t.Run("nested function captures registry snapshot", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func run() -> int
    struct Local
        value: int
    end
    struct Other
        value: int
    end
    func make_local(value: int) -> Local
        return Local(value)
    end
    let makers: (func(int) -> Local)[] = [make_local]
    pop(ref makers)
    let invoke: any = append
    invoke(ref makers, Local)
    invoke(ref makers, Other)
    return length(makers)
end
test_report(run())`)
		testExpectedObject(t, 1, got)
	})

	t.Run("shadow restores outer definition", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func run() -> int
    struct Item
        value: int
    end
    let makers: (func(int) -> Item)[] = [Item]
    pop(ref makers)
    if true then
        struct Item
            value: string
        end
        let inner: (func(string) -> Item)[] = [Item]
        let made: Item = inner[0]("ok")
    end
    let invoke: any = append
    invoke(ref makers, Item)
    return length(makers)
end
test_report(run())`)
		testExpectedObject(t, 1, got)
	})

	t.Run("sibling functions isolate registries", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func first() -> int
    struct Local
        value: int
    end
    let makers: (func(int) -> Local)[] = [Local]
    return makers[0](4).value
end
func second() -> int
    struct Local
        value: string
    end
    let makers: (func(string) -> Local)[] = [Local]
    return length(makers[0]("ok").value)
end
test_report(first() * 10 + second())`)
		testExpectedObject(t, 42, got)
	})
}

func TestLocalStructSchemaPreservesReferenceAndJSONFields(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func run() -> int
    struct Node
        value: int
    end
    struct Local
        node: ref Node
        count: int
    end
    let makers: (func(ref Node, int) -> Local)[] = [Local]
    let node: Node = Node(42)
    let target: Local = makers[0](ref node, 1)
    let ok: bool = json_loads("{\"count\":2}", ref target)
    if ok then return target.node.value + target.count else return 0 end
end
test_report(run())`)
	testExpectedObject(t, 44, got)
}

func TestDynamicStructConstructorValidatesModesAndTypesBeforeConstruction(t *testing.T) {
	t.Run("plain passed to ref field", func(t *testing.T) {
		err := runTypedFunctionProgramError(t, `
struct Node
    value: int
end
struct Holder
    node: ref Node
end
let dynamic: func = Holder
let node: Node = Node(1)
dynamic(node)`)
		if err == nil || !strings.Contains(err.Error(), "expected ref Node") {
			t.Fatalf("error=%v, want ref mode rejection", err)
		}
	})

	t.Run("ref passed to ordinary field", func(t *testing.T) {
		err := runTypedFunctionProgramError(t, `
struct Node
    value: int
end
struct Holder
    node: Node
end
let dynamic: func = Holder
let node: Node = Node(1)
dynamic(ref node)`)
		if err == nil || !strings.Contains(err.Error(), "expected Node, got ref") {
			t.Fatalf("error=%v, want ordinary mode rejection", err)
		}
	})

	t.Run("wrong ordinary type", func(t *testing.T) {
		err := runTypedFunctionProgramError(t, `
struct Box
    value: int
end
let dynamic: func = Box
dynamic("wrong")`)
		if err == nil || !strings.Contains(err.Error(), "expected int") {
			t.Fatalf("error=%v, want runtime type rejection", err)
		}
	})

	t.Run("valid explicit ref", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
struct Node
    value: int
end
struct Holder
    node: ref Node
end
let dynamic: func = Holder
let node: Node = Node(42)
let made: any = dynamic(ref node)
test_report(made.node.value)`)
		testExpectedObject(t, 42, got)
	})

	t.Run("valid null ref", func(t *testing.T) {
		// Spec §2.4: so um campo `ref Node?` aceita null, tambem na
		// construcao dinamica.
		got := runTypedFunctionProgram(t, `
struct Node
    value: int
end
struct Holder
    node: ref Node?
end
let dynamic: func = Holder
let made: any = dynamic(null)
if made.node == null then test_report(42) else test_report(0) end`)
		testExpectedObject(t, 42, got)
	})
}

func TestStructConstructorValidationFailureLeavesStackUntouched(t *testing.T) {
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	constructorType := &value.RuntimeTypeInfo{
		Kind:       value.TYPE_CALLABLE,
		Params:     []*value.RuntimeTypeInfo{integer},
		ParamIsRef: []bool{false},
		Return:     &value.RuntimeTypeInfo{Kind: value.TYPE_STRUCT, Name: "Box", Fields: map[string]*value.RuntimeTypeInfo{"value": integer}},
	}
	definition := &value.ObjStruct{Name: "Box", Fields: []string{"value"}, ConstructorType: constructorType}
	constructor := value.Value{Type: value.VAL_OBJ, Obj: definition}
	argument := value.NewString("wrong")
	machine := New()
	machine.push(constructor)
	machine.push(argument)
	before := machine.stackTop
	ok, err := machine.callValue(constructor, 1, nil, 0)
	if ok || err == nil || !strings.Contains(err.Error(), "expected int") {
		t.Fatalf("ok=%v error=%v, want type rejection", ok, err)
	}
	if machine.stackTop != before || machine.stack[0].Obj != definition || machine.stack[1].Obj != argument.Obj {
		t.Fatal("constructor validation failure mutated stack or constructed an instance")
	}
}

func TestDynamicStructConstructorRejectsMalformedReference(t *testing.T) {
	err := runMalformedReferenceProgram(t, `
struct Node
    value: int
end
struct Holder
    node: ref Node
end
let dynamic: func = Holder
dynamic(malformed_ref())`, value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR}})
	if err == nil {
		t.Fatal("dynamic constructor accepted malformed reference")
	}
}

func TestDynamicAppendUsesDeclaredCollectionSchemas(t *testing.T) {
	t.Run("fixed array positive", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let invoke: any = append
let item: int[4] = [1, 2, 3, 4]
let target: int[4][] = [item]
pop(ref target)
invoke(ref target, item)
test_report(length(target))`)
		testExpectedObject(t, 1, got)
	})

	for _, tt := range []struct {
		name     string
		itemType string
		literal  string
	}{
		{name: "dynamic array with matching length", itemType: "int[]", literal: "[1, 2, 3, 4]"},
		{name: "different fixed array size", itemType: "int[5]", literal: "[1, 2, 3, 4, 5]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := fmt.Sprintf(`
let invoke: any = append
let seed: int[4] = [1, 2, 3, 4]
let target: int[4][] = [seed]
pop(ref target)
let item: %s = %s
invoke(ref target, item)
test_report(length(target))`, tt.itemType, tt.literal)
			testExpectedObject(t, 0, runTypedFunctionProgram(t, source))
		})
	}

	t.Run("empty nested callable array mismatch", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func integer(value: int) -> int return value end
func text(value: string) -> int return 1 end
let expected_item: (func(int) -> int)[] = [integer]
pop(ref expected_item)
let target: ((func(int) -> int)[])[] = [expected_item]
pop(ref target)
let wrong: (func(string) -> int)[] = [text]
pop(ref wrong)
let invoke: any = append
invoke(ref target, wrong)
test_report(length(target))`)
		testExpectedObject(t, 0, got)
	})

	t.Run("empty nested callable array positive", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func integer(value: int) -> int return value end
let item: (func(int) -> int)[] = [integer]
pop(ref item)
let target: ((func(int) -> int)[])[] = [item]
pop(ref target)
let invoke: any = append
invoke(ref target, item)
test_report(length(target))`)
		testExpectedObject(t, 1, got)
	})

	t.Run("empty map mismatch", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let seed: map[string, int] = {"value": 1}
let target: map[string, int][] = [seed]
pop(ref target)
let wrong: map[int, string] = {}
let invoke: any = append
invoke(ref target, wrong)
test_report(length(target))`)
		testExpectedObject(t, 0, got)
	})

	t.Run("empty map positive", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let seed: map[string, int] = {"value": 1}
let target: map[string, int][] = [seed]
pop(ref target)
let item: map[string, int] = {}
let invoke: any = append
invoke(ref target, item)
test_report(length(target))`)
		testExpectedObject(t, 1, got)
	})
}

func TestCollectionSchemaSurvivesShallowCopyAndMutation(t *testing.T) {
	t.Run("array", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func forward(item: int[4], target: ref int[4][]) -> void
    pop(ref item)
    let invoke: any = append
    invoke(target, item)
end
let item: int[4] = [1, 2, 3, 4]
let target: int[4][] = [item]
pop(ref target)
forward(item, ref target)
test_report(length(target) * 10 + length(item))`)
		testExpectedObject(t, 14, got)
	})

	t.Run("map", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func forward(item: map[string, int], target: ref map[string, int][]) -> void
    delete(ref item, "value")
    let invoke: any = append
    invoke(target, item)
end
let item: map[string, int] = {"value": 1}
let target: map[string, int][] = [item]
pop(ref target)
forward(item, ref target)
test_report(length(target) * 10 + length(keys(item)))`)
		testExpectedObject(t, 11, got)
	})
}

func TestConflictingCollectionAndChannelMetadataFailsWithoutOverwrite(t *testing.T) {
	tests := []struct{ source, want string }{
		{`let dynamic: int[] = [1, 2, 3, 4]
let fixed: int[4] = [1, 2, 3, 4]
let erased: any = dynamic
fixed = erased`, "expected int[4], got int[]"},
		{`let text: map[string, int] = {"value": 1}
let integer: map[int, string] = {1: "value"}
let erased: any = integer
text = erased`, "expected map[string, int], got map[int, string]"},
		{`let text: chan string = make_chan(1)
let integer: chan int = make_chan(1)
let erased: any = text
integer = erased`, "expected chan int, got chan string"},
	}
	for _, tt := range tests {
		err := runTypedFunctionProgramError(t, tt.source)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("want %q, got error=%v", tt.want, err)
		}
	}
}

func TestCollectionRuntimeMetadataPreservesShallowCopyAndIdentity(t *testing.T) {
	machine := New()
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	arraySchema := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: integer, Size: 4}
	mapSchema := &value.RuntimeTypeInfo{Kind: value.TYPE_MAP, Key: &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}, Value: integer}
	shared := value.NewChannel(1)

	array := value.NewArray([]value.Value{shared})
	arrayObject := array.Obj.(*value.ObjArray)
	arrayObject.RuntimeType.Store(arraySchema)
	arrayCopy := machine.copyValue(array)
	arrayCopyObject := arrayCopy.Obj.(*value.ObjArray)
	if arrayCopyObject == arrayObject || arrayCopyObject.RuntimeType.Load() != arraySchema || arrayCopyObject.Elements[0].Obj != shared.Obj {
		t.Fatal("array shallow copy lost collection schema or nested identity")
	}

	mapping := value.NewMap()
	mapObject := mapping.Obj.(*value.ObjMap)
	setTestMap(mapObject, "item", shared)
	mapObject.RuntimeType.Store(mapSchema)
	mapCopy := machine.copyValue(mapping)
	mapCopyObject := mapCopy.Obj.(*value.ObjMap)
	if mapCopyObject == mapObject || mapCopyObject.RuntimeType.Load() != mapSchema || requireTestMapValue(t, mapCopyObject, "item").Obj != shared.Obj {
		t.Fatal("map shallow copy lost collection schema or nested identity")
	}
}

func TestRuntimeValueMarkerRejectsConflictsWithoutOverwriteOrRefMutation(t *testing.T) {
	machine := New()
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	text := &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}
	dynamicArray := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: integer}
	fixedArray := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: integer, Size: 4}
	array := value.NewArray([]value.Value{value.NewInt(1)})
	arrayObject := array.Obj.(*value.ObjArray)
	arrayObject.RuntimeType.Store(dynamicArray)
	if machine.markRuntimeValueType(array, fixedArray) || arrayObject.RuntimeType.Load() != dynamicArray {
		t.Fatal("array conflict overwrote existing runtime schema")
	}

	stringMap := &value.RuntimeTypeInfo{Kind: value.TYPE_MAP, Key: text, Value: integer}
	intMap := &value.RuntimeTypeInfo{Kind: value.TYPE_MAP, Key: integer, Value: text}
	mapping := value.NewMap()
	mapObject := mapping.Obj.(*value.ObjMap)
	mapObject.RuntimeType.Store(stringMap)
	if machine.markRuntimeValueType(mapping, intMap) || mapObject.RuntimeType.Load() != stringMap {
		t.Fatal("map conflict overwrote existing runtime schema")
	}

	stringChannel := &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}
	channel := value.NewChannel(1)
	channelObject := channel.Obj.(*value.ObjChannel)
	channelObject.ElementType = stringChannel
	if machine.markRuntimeValueType(channel, &value.RuntimeTypeInfo{Kind: value.TYPE_CHANNEL, Element: integer}) || channelObject.ElementType != stringChannel {
		t.Fatal("channel conflict overwrote existing runtime schema")
	}

	ref := &value.ObjRef{RefType: value.REF_PTR, Ptr: &array}
	refValue := value.Value{Type: value.VAL_REF, Obj: ref}
	if !machine.markRuntimeValueType(refValue, &value.RuntimeTypeInfo{Kind: value.TYPE_REF, Element: dynamicArray}) {
		t.Fatal("compatible reference value marker was rejected")
	}
	if ref.TargetType.Load() != nil || refValue.Obj != ref {
		t.Fatal("runtime value marker mutated or rebound ObjRef")
	}
}

func TestTypedJSONLoadsBuildsCollectionRuntimeSchemas(t *testing.T) {
	t.Run("empty nested array", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let decoded: int[][] = [[1]]
let ok: bool = json_loads("[[]]", ref decoded)
let seed: int[] = [1]
let target: int[][] = [seed]
pop(ref target)
let invoke: any = append
invoke(ref target, decoded[0])
if ok then test_report(length(target)) else test_report(999) end`)
		testExpectedObject(t, 1, got)
	})

	t.Run("empty nested map", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let decoded: map[string, int][] = [{"seed": 1}]
let ok: bool = json_loads("[{}]", ref decoded)
let seed: map[string, int] = {"seed": 1}
let target: map[string, int][] = [seed]
pop(ref target)
let invoke: any = append
invoke(ref target, decoded[0])
if ok then test_report(length(target)) else test_report(999) end`)
		testExpectedObject(t, 1, got)
	})
}

func TestRuntimeValueMarkerPropagatesThroughReferenceWithoutMutatingIt(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "typed declaration",
			source: `
let typed: ref int[] = unknown_ref()
let seed: int[] = [1]
let target: int[][] = [seed]
pop(ref target)
let invoke: any = append
invoke(ref target, *typed)
test_report(length(target))`,
		},
		{
			name: "reference rebind",
			source: `
let initial: int[] = [1]
let typed: ref int[] = ref initial
typed = unknown_ref()
let seed: int[] = [1]
let target: int[][] = [seed]
pop(ref target)
let invoke: any = append
invoke(ref target, *typed)
test_report(length(target))`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lexer.New(tt.source)
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
			backing := value.NewArray(nil)
			originalRef := &value.ObjRef{RefType: value.REF_PTR, Ptr: &backing}
			captured := value.NewNull()
			machine.DefineNative("unknown_ref", func(args []value.Value) value.Value {
				return value.Value{Type: value.VAL_REF, Obj: originalRef}
			})
			machine.DefineNative("test_report", func(args []value.Value) value.Value {
				captured = args[0]
				return value.NewNull()
			})
			if err := machine.Interpret(code); err != nil {
				t.Fatal(err)
			}
			testExpectedObject(t, 1, captured)
			backingSchema := backing.Obj.(*value.ObjArray).RuntimeType.Load()
			if backingSchema == nil || backingSchema.Kind != value.TYPE_ARRAY || backingSchema.Element == nil || backingSchema.Element.Kind != value.TYPE_INT {
				t.Fatalf("backing schema=%#v", backingSchema)
			}
			if originalRef.Ptr != &backing || originalRef.TargetType.Load() != nil {
				t.Fatal("runtime value marker mutated or rebound the ObjRef")
			}
		})
	}

	t.Run("map referent", func(t *testing.T) {
		source := `
let typed: ref map[string, int] = unknown_ref()
let seed: map[string, int] = {"seed": 1}
let target: map[string, int][] = [seed]
pop(ref target)
let invoke: any = append
invoke(ref target, *typed)
test_report(length(target))`
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
		backing := value.NewMap()
		originalRef := &value.ObjRef{RefType: value.REF_PTR, Ptr: &backing}
		captured := value.NewNull()
		machine.DefineNative("unknown_ref", func(args []value.Value) value.Value {
			return value.Value{Type: value.VAL_REF, Obj: originalRef}
		})
		machine.DefineNative("test_report", func(args []value.Value) value.Value {
			captured = args[0]
			return value.NewNull()
		})
		if err := machine.Interpret(code); err != nil {
			t.Fatal(err)
		}
		testExpectedObject(t, 1, captured)
		backingSchema := backing.Obj.(*value.ObjMap).RuntimeType.Load()
		if backingSchema == nil || backingSchema.Kind != value.TYPE_MAP || backingSchema.Key.Kind != value.TYPE_STRING || backingSchema.Value.Kind != value.TYPE_INT {
			t.Fatalf("backing schema=%#v", backingSchema)
		}
		if originalRef.Ptr != &backing || originalRef.TargetType.Load() != nil {
			t.Fatal("runtime value marker mutated or rebound the map ObjRef")
		}
	})

	t.Run("channel referent", func(t *testing.T) {
		source := `
let typed: ref chan int = unknown_ref()
let seed: chan int = make_chan(1)
let target: (chan int)[] = [seed]
pop(ref target)
let invoke: any = append
invoke(ref target, *typed)
test_report(length(target))`
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
		backing := value.NewChannel(1)
		originalRef := &value.ObjRef{RefType: value.REF_PTR, Ptr: &backing}
		captured := value.NewNull()
		machine.DefineNative("unknown_ref", func(args []value.Value) value.Value {
			return value.Value{Type: value.VAL_REF, Obj: originalRef}
		})
		machine.DefineNative("test_report", func(args []value.Value) value.Value {
			captured = args[0]
			return value.NewNull()
		})
		if err := machine.Interpret(code); err != nil {
			t.Fatal(err)
		}
		testExpectedObject(t, 1, captured)
		channel := backing.Obj.(*value.ObjChannel)
		if channel.ElementType == nil || channel.ElementType.Kind != value.TYPE_INT {
			t.Fatalf("channel element schema=%#v", channel.ElementType)
		}
		if originalRef.Ptr != &backing || originalRef.TargetType.Load() != nil {
			t.Fatal("runtime value marker mutated or rebound the channel ObjRef")
		}
	})
}

func TestConcurrentCollectionRuntimeMetadataPublication(t *testing.T) {
	machine := New()
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	arraySchema := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: integer}
	mapSchema := &value.RuntimeTypeInfo{Kind: value.TYPE_MAP, Key: &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}, Value: integer}

	tests := []struct {
		name        string
		actual      value.Value
		schema      *value.RuntimeTypeInfo
		conflicting *value.RuntimeTypeInfo
	}{
		{
			name:        "array",
			actual:      value.NewArray(nil),
			schema:      arraySchema,
			conflicting: &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}},
		},
		{
			name:        "map",
			actual:      value.NewMap(),
			schema:      mapSchema,
			conflicting: &value.RuntimeTypeInfo{Kind: value.TYPE_MAP, Key: integer, Value: integer},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := make(chan struct{})
			var workers sync.WaitGroup
			for i := 0; i < 16; i++ {
				workers.Add(2)
				go func() {
					defer workers.Done()
					<-start
					for j := 0; j < 100; j++ {
						machine.markRuntimeValueType(tt.actual, tt.schema)
					}
				}()
				go func() {
					defer workers.Done()
					<-start
					for j := 0; j < 100; j++ {
						machine.runtimeValueMatchesType(tt.actual, tt.schema)
					}
				}()
			}
			close(start)
			workers.Wait()
			if !machine.markRuntimeValueType(tt.actual, tt.schema) {
				t.Fatal("compatible concurrent marker was not published")
			}

			failures := make(chan string, 2400)
			workers = sync.WaitGroup{}
			for i := 0; i < 8; i++ {
				workers.Add(3)
				go func() {
					defer workers.Done()
					for j := 0; j < 100; j++ {
						if !machine.markRuntimeValueType(tt.actual, tt.schema) {
							failures <- "compatible marker was rejected"
						}
					}
				}()
				go func() {
					defer workers.Done()
					for j := 0; j < 100; j++ {
						if machine.markRuntimeValueType(tt.actual, tt.conflicting) {
							failures <- "conflicting marker was accepted"
						}
					}
				}()
				go func() {
					defer workers.Done()
					for j := 0; j < 100; j++ {
						if !machine.runtimeValueMatchesType(tt.actual, tt.schema) {
							failures <- "dynamic validation lost the published schema"
						}
					}
				}()
			}
			workers.Wait()
			close(failures)
			for failure := range failures {
				t.Error(failure)
			}
		})
	}
}

func TestConcurrentReferenceTargetMetadataPublication(t *testing.T) {
	machine := New()
	integer := &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
	arraySchema := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: integer}
	conflicting := &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}}
	stored := value.NewArray(nil)
	ref := &value.ObjRef{RefType: value.REF_PTR, Ptr: &stored}

	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < 100; j++ {
				if !markReferenceTargetType(ref, arraySchema) {
					t.Errorf("compatible reference schema was rejected")
					return
				}
			}
		}()
		go func() {
			defer workers.Done()
			<-start
			for j := 0; j < 100; j++ {
				machine.appendItemCompatible(ref, value.NewInt(1))
				ref.JSONDynamic.Store(true)
				_ = ref.JSONDynamic.Load()
			}
		}()
	}
	close(start)
	workers.Wait()
	if ref.TargetType.Load() != arraySchema {
		t.Fatal("compatible concurrent marker did not preserve the published schema")
	}
	if markReferenceTargetType(ref, conflicting) || ref.TargetType.Load() != arraySchema {
		t.Fatal("conflicting reference marker succeeded or overwrote the published schema")
	}
}

// array_utils.slice (modulo removido) era um curry dinamico sobre o builtin:
// o nativo entrava como `any` e o resultado voltava por uma closure tipada
// `any`, entao o `let` anotado so passava se slice preservasse o schema de
// runtime do array. O curry e reproduzido aqui para manter a cobertura do
// caminho dinamico; o caminho estatico (`slice(values, 0, 1)` direto) tem o
// tipo de retorno inferido pelo compilador e nao passa pela validacao.
func TestSliceThroughAnyPreservesGenericCollectionRuntimeSchema(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Node
    value: int
end
func wrap(native_slice: any) -> func(any, int, int) -> any
    return func(array: any, start: int, _end: int) -> any
        return native_slice(array, start, _end)
    end
end
let dyn_slice: any = wrap(slice)
let node: Node = Node(1)
let values: (ref Node)[] = [ref node]
let sliced: (ref Node)[] = dyn_slice(values, 0, 1)
test_report(length(sliced))`)
	testExpectedObject(t, 1, got)
}

func TestWildcardExportsRemainStableAcrossModuleCache(t *testing.T) {
	source := `
use strings select *
let shout: string = to_upper("abc")
let parts: SplitResult = split("a,b,c", ",")
test_report(length(shout) * 10 + parts.count)`
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
	results := make([]int64, 0, 2)
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		results = append(results, args[0].Int())
		return value.NewNull()
	})
	for i := 0; i < 2; i++ {
		if err := machine.Interpret(code); err != nil {
			t.Fatalf("iteration %d: %v", i+1, err)
		}
	}
	if len(results) != 2 || results[0] != 33 || results[1] != 33 {
		t.Fatalf("wildcard first/cache results=%v, want [33 33]", results)
	}
	module, ok := machine.GetModule("strings")
	if !ok {
		t.Fatal("strings was not cached")
	}
	exports := module.Obj.(*value.ObjMap).Snapshot()
	if _, ok := exports["to_upper"]; !ok {
		t.Fatal("strings omitted to_upper export")
	}
	if _, ok := exports["strings_to_upper"]; ok {
		t.Fatal("strings leaked the raw native as a public export")
	}
}

func TestTypedJSONLoadsRejectsCallableAndChannelConstruction(t *testing.T) {
	t.Run("empty bare function array after pop", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func identity(value: int) -> int
    return value
end
let target: func[] = [identity]
pop(ref target)
let ok: bool = json_loads("[42]", ref target)
if ok then test_report(999) else test_report(length(target)) end`)
		testExpectedObject(t, 0, got)
	})

	t.Run("callable map value", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
func identity(value: int) -> int
    return value
end
let target: map[string, func] = {"seed": identity}
delete(ref target, "seed")
let ok: bool = json_loads("{\"item\":42}", ref target)
if ok then test_report(999) else test_report(length(keys(target))) end`)
		testExpectedObject(t, 0, got)
	})

	t.Run("reference callable element", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let target: (ref func)?[] = [null]
pop(ref target)
let ok: bool = json_loads("[42]", ref target)
if ok then test_report(999) else test_report(length(target)) end`)
		testExpectedObject(t, 0, got)
	})

	t.Run("typed channel element", func(t *testing.T) {
		got := runTypedFunctionProgram(t, `
let channel: chan int = make_chan(1)
let target: (chan int)[] = [channel]
pop(ref target)
let ok: bool = json_loads("[42]", ref target)
if ok then test_report(999) else test_report(length(target)) end`)
		testExpectedObject(t, 0, got)
	})
}

func TestTypedJSONLoadsPreservesOmittedCallableStructField(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func identity(value: int) -> int
    return value
end
struct Holder
    version: int
    callback: func(int) -> int
end
let target: Holder = Holder(1, identity)
let ok: bool = json_loads("{\"version\":2}", ref target)
if ok then test_report(target.callback(7)) else test_report(999) end`)
	testExpectedObject(t, 7, got)
}

func TestDynamicAppendRejectsMalformedReferenceItemWithoutMutation(t *testing.T) {
	source := `
let invoke: any = append
let values: (ref int)?[] = []
invoke(ref values, malformed_ref())
test_report(length(values))`
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
	captured := value.NewNull()
	machine.DefineNative("malformed_ref", func(args []value.Value) value.Value {
		return value.Value{Type: value.VAL_REF, Obj: "not an ObjRef"}
	})
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) > 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("malformed append item must fail as a legacy no-op, got %v", err)
	}
	testExpectedObject(t, 0, captured)
}

func TestUnrelatedWildcardImportKeepsBuiltinRuntimeContract(t *testing.T) {
	got := runTypedFunctionProgram(t, `
use sys select *
let values: int[] = [1]
append(ref values, 2)
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
delete(ref mapping, "a")
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

func TestUncalledFunctionImportDoesNotChangeOuterBuiltinCompilation(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func unused() -> void
    use http_client select delete
end
let mapping: map[string, int] = {"a": 1}
delete(ref mapping, "a")
test_report(length(keys(mapping)))`)
	testExpectedObject(t, 0, got)
}

func TestWildcardWrapperFirstAndCachedLoadsExposeImportedBinding(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "first load", source: "use http select *\ntest_report(delete)"},
		// Issue #47 parte 2: a variante com `func delete` do usuario entre os
		// dois `use` virou colisao de nome (erro de compilacao, ver
		// TestCachedWildcardWrapperCollidesWithUserBinding no compilador).
		{name: "cached load", source: "use http\nuse http select *\ntest_report(delete)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runTypedFunctionProgram(t, tt.source)
			closure, ok := got.Obj.(*value.ObjClosure)
			if got.Type != value.VAL_FUNCTION || !ok || closure.Function.Arity != 1 {
				t.Fatalf("delete binding=%v, want imported one-argument closure", got)
			}
		})
	}
}

func TestPlainWrapperImportDoesNotLeakUnqualifiedBindings(t *testing.T) {
	got := runTypedFunctionProgram(t, "use http\ntest_report(delete)")
	if got.Type != value.VAL_NATIVE {
		t.Fatalf("delete binding=%v, want shared native builtin", got)
	}
}

func TestContextualReferenceCallsForwardStoredReferences(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "property",
			source: `
struct Holder
    values: ref int[]
end
let values: int[] = [1]
let holder: Holder = Holder(ref values)
func push(target: ref int[]) -> void
    append(target, 2)
end
push(holder.values)
test_report(length(values))`,
		},
		{
			name: "array index",
			source: `
let values: int[] = [1]
let stored: (ref int[])[] = [ref values]
func push(target: ref int[]) -> void
    append(target, 2)
end
push(stored[0])
test_report(length(values))`,
		},
		{
			name: "map index",
			source: `
let values: int[] = [1]
let stored: map[string, ref int[]] = {"values": ref values}
func push(target: ref int[]) -> void
    append(target, 2)
end
push(stored["values"])
test_report(length(values))`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testExpectedObject(t, 2, runTypedFunctionProgram(t, tt.source))
		})
	}
}

// Spec §2.4: o campo que guarda null e `ref int[]?`, e o null armazenado
// continua sendo encaminhado como null — nunca vira ref para o slot. Um
// `ref int[]?` nao entra num parametro `ref int[]` sem teste (erro de
// compilacao); pela base `any` o marcador de runtime rejeita o null na
// fronteira; e um parametro `ref int[]?` recebe o null como null.
func TestContextualReferenceCallPreservesStoredNullReference(t *testing.T) {
	const prelude = `
struct Holder
    values: ref int[]?
end
let holder: Holder = Holder(null)
`
	compileErr := interpretOrCompileErr(t, New(), prelude+`
func push(target: ref int[]) -> void
    append(target, 2)
end
push(holder.values)`)
	if compileErr == nil || !strings.Contains(compileErr.Error(), "expected ref int[], got ref int[]?") || !strings.Contains(compileErr.Error(), "'holder.values' may be null") {
		t.Fatalf("typed forwarding of a nullable ref field must be a compile error, got %v", compileErr)
	}
	runtimeErr := runTypedFunctionProgramError(t, prelude+`
func push(target: ref int[]) -> void
    append(target, 2)
end
let dyn: any = holder
push(dyn.values)`)
	if runtimeErr == nil || !strings.Contains(runtimeErr.Error(), "expected ref int[], got null") {
		t.Fatalf("null forwarded through any into ref int[] must fail at the boundary, got %v", runtimeErr)
	}
	got := runTypedFunctionProgram(t, prelude+`
func eh_nulo(target: ref int[]?) -> bool
    return target == null
end
if eh_nulo(holder.values) then
    test_report(42)
else
    test_report(0)
end`)
	testExpectedObject(t, 42, got)
}

// Spec §2.3 regra 2 / §4.2: um elemento ou valor de tipo estatico `ref T`
// que contem null e ENCAMINHADO como null — nao vira ref para o slot. Dentro
// da funcao `target == null` e verdadeiro, igual a uma variavel `ref T` nula.
// (Antes, o padrao fill-null-slot preenchia o slot `ref int` com um int cru
// via `*target = 42`.)
func TestContextualReferenceCallsForwardNullIndexSlots(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "array index",
			source: `
func eh_nulo(target: ref int?) -> bool
    return target == null
end
let stored: (ref int)?[] = [null]
if eh_nulo(stored[0]) then
    test_report(42)
else
    test_report(0)
end`,
		},
		{
			name: "map index",
			source: `
func eh_nulo(target: ref int?) -> bool
    return target == null
end
let stored: map[string, (ref int)?] = {"answer": null}
if eh_nulo(stored["answer"]) then
    test_report(42)
else
    test_report(0)
end`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testExpectedObject(t, 42, runTypedFunctionProgram(t, tt.source))
		})
	}
}

// Escrever atraves do null encaminhado e o erro claro de ref nula — nao ha
// slot por tras para preencher. Vale para elemento null de array, valor null
// de map e chave ausente de map (que le como null); para preencher, o
// chamador escreve no dono: `stored[0] = ref novo` / `m["k"] = ref novo`.
// Spec §2.4: o elemento/valor `(ref int)?` nao entra num `ref int` sem teste
// — o null so e encaminhado pela base `any`; a chave ausente le como `ref
// int` e continua encaminhando direto.
func TestWritingThroughForwardedNullIndexSlotIsRuntimeError(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "array element",
			source: `
func fill(target: ref int) -> void
    *target = 42
end
let stored: (ref int)?[] = [null]
let dyn: any = stored
fill(dyn[0])`,
		},
		{
			name: "map null value",
			source: `
func fill(target: ref int) -> void
    *target = 42
end
let stored: map[string, (ref int)?] = {"answer": null}
let dyn: any = stored
fill(dyn["answer"])`,
		},
		{
			name: "map missing key",
			source: `
func fill(target: ref int) -> void
    *target = 42
end
let stored: map[string, ref int] = {}
fill(stored["missing"])`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runTypedFunctionProgramError(t, tt.source)
			if err == nil || !strings.Contains(err.Error(), "cannot update null reference") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
