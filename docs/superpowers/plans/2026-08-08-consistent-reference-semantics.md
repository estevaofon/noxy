# Consistent Reference Semantics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close Noxy's observed reference-semantics gaps while preserving shallow parameter copies, contextual borrowing for exact calls, and existing valid source syntax.

**Architecture:** Keep the compiler's current exact-call contextual conversion and extend it to captured upvalues and compiler-known public natives. Pair those compiler contracts with runtime native descriptors, while retaining the current Go native ABI and shallow `copyValue` behavior. Add runtime parameter-mode validation shared by script and native calls. Treat untyped native/plugin calls and VM-only descriptors as dynamic boundaries that never infer references.

**Tech Stack:** Go 1.24, Noxy lexer/parser/AST/compiler, bytecode chunk opcodes, stack VM, Go `testing`, Noxy conformance fixtures.

## Global Constraints

- Preserve ordinary composite parameter shallow-copy behavior exactly.
- Preserve assignment behavior for arrays, maps, and structs.
- Preserve `increment(value)`, `append(values, item)`, `pop(values)`, `delete(mapping, key)`, and `json_loads(json, target)` for exact/typed calls.
- Preserve automatic dereference for reads.
- `*reference = value` updates the target; `reference = ref other` rebinds the reference.
- Exact user calls and compiler-known public native calls use the same contextual reference conversion.
- Bare `func`, untyped native primitives, VM-only descriptors, and plugins without signatures never infer references.
- Do not add `mut`, `readonly`, ownership, borrow checking, deep copy, or copy-on-write.
- Do not change the Go `NativeFunc func([]Value) Value` ABI.
- Every behavior change starts with a failing test and ends with focused plus full verification.

## File Map

- `internal/compiler/compiler.go`: exact call lowering, reference-argument diagnostics, `OP_REF_UPVALUE` emission.
- `internal/compiler/builtin_calls.go`: new focused compiler logic for typed generic mutating builtins.
- `internal/compiler/function_types_test.go`: compiler conformance and diagnostics.
- `internal/chunk/chunk.go`: `OP_REF_UPVALUE` declaration, string name, and disassembly.
- `internal/value/value.go`: runtime parameter type names and optional native signatures.
- `internal/vm/call_validation.go`: new shared script/native arity and reference-mode validation.
- `internal/vm/references.go`: new focused reference resolution used by native mutators.
- `internal/vm/vm.go`: opcode execution, typed native registration, mutating native handlers, call integration.
- `internal/vm/function_types_test.go`: runtime reference-mode and upvalue conformance.
- `internal/vm/native_signatures_test.go`: new typed-native contract tests.
- `docs/NOXY_LANGUAGE_SPEC.md`: normative shallow copy, contextual conversion, dynamic boundaries, update/rebind correction.
- `docs/REF_SEMANTICS.md`: focused reference guide aligned with compiler behavior.
- `README.md`: accurate static/dynamic typing wording.
- `noxy_examples/typed_function_conformance.nx`: positive reference examples.
- `noxy_examples/type_errors/typed_function_invalid_ref_argument.nx`: non-addressable argument fixture.
- `CHANGELOG.md`: user-visible fixes and additions.

---

### Task 1: Make reference update diagnostics match the existing semantics

**Files:**
- Modify: `internal/compiler/function_types_test.go`
- Modify: `internal/compiler/compiler.go`

**Interfaces:**
- Consumes: existing local, upvalue, global, field, array-element, and map-value `ref T` assignment handling.
- Produces: `referenceAssignmentTypeError(name string, expected, actual ast.NoxyType, line int) error`, used whenever plain `T` is assigned to a reference-bearing storage slot.

- [ ] **Step 1: Write the failing compiler diagnostic test**

Add to `internal/compiler/function_types_test.go`:

```go
func TestReferenceValueAssignmentSuggestsDereference(t *testing.T) {
	_, err := compileFunctionSource(t, `
func increment(value: ref int) -> void
    value = value + 1
end`)
	if err == nil {
		t.Fatal("expected reference assignment error")
	}
	want := "cannot assign int to ref int"
	if !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "use '*value = ...'") {
		t.Fatalf("error=%q, want %q and dereference hint", err, want)
	}
}
```

- [ ] **Step 2: Run the test and verify the current generic diagnostic fails it**

Run:

```powershell
go test ./internal/compiler -run TestReferenceValueAssignmentSuggestsDereference -v
```

Expected: FAIL because the local parameter path currently reports only `type mismatch in assignment` and lacks the `*value` hint.

- [ ] **Step 3: Add one diagnostic helper and use it for every reference-bearing assignment target**

Add near the compiler type helpers in `internal/compiler/compiler.go`:

```go
func referenceAssignmentTypeError(line int, name string, expected, actual ast.NoxyType) error {
	return fmt.Errorf(
		"[line %d] cannot assign %s to %s\n  hint: use '*%s = ...' to update the referenced value",
		line, noxyTypeName(actual), noxyTypeName(expected), name,
	)
}
```

In every assignment branch, first preserve compatible `ref T`/`null` rebinds. If the destination type is `ref T` and the RHS is compatible plain `T`, call this helper with the exact source target (`value`, `holder.field`, or `items[index]`). Apply that ordering to locals, captured upvalues, globals, struct fields, array elements, and map values. Preserve the existing strict mismatch diagnostic when the RHS is neither compatible `T`, compatible `ref T`, nor `null`.

Add table-driven cases for a local reference parameter, a global reference variable, and a captured reference parameter. Retain the existing field/index behavior while routing their compatible-value diagnostics through the same helper.

- [ ] **Step 4: Run focused and compiler tests**

Run:

```powershell
go test ./internal/compiler -run 'TestReferenceValueAssignmentSuggestsDereference|TestExactReferenceCallsCompile' -v
go test ./internal/compiler
```

Expected: PASS.

- [ ] **Step 5: Commit the diagnostic improvement**

```powershell
git add internal/compiler/compiler.go internal/compiler/function_types_test.go
git commit -m "fix: clarify reference update diagnostics"
```

---

### Task 2: Support contextual references to captured upvalues

**Files:**
- Modify: `internal/chunk/chunk.go`
- Modify: `internal/compiler/compiler.go`
- Modify: `internal/compiler/function_types_test.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/function_types_test.go`

**Interfaces:**
- Consumes: `Compiler.resolveUpvalue`, `ObjClosure.Upvalues`, `ObjRef{RefType: REF_UPVALUE}`, and existing open/closed `ObjUpvalue` lifetime.
- Produces: bytecode `OP_REF_UPVALUE <slot>` and runtime construction of an upvalue-backed `VAL_REF`.

- [ ] **Step 1: Write the failing compiler test for captured contextual conversion**

Add to `internal/compiler/function_types_test.go`:

```go
func TestExactReferenceCallAcceptsCapturedVariable(t *testing.T) {
	_, err := compileFunctionSource(t, `
func increment(value: ref int) -> void
    *value = value + 1
end
func make_incrementer() -> func() -> int
    let value: int = 0
    return func() -> int
        increment(value)
        return value
    end
end`)
	if err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run it and verify the captured-variable restriction fails**

Run:

```powershell
go test ./internal/compiler -run TestExactReferenceCallAcceptsCapturedVariable -v
```

Expected: FAIL with `captured variables cannot be passed by reference`.

- [ ] **Step 3: Add `OP_REF_UPVALUE` to the chunk contract**

In `internal/chunk/chunk.go`, insert the opcode beside `OP_REF_LOCAL`:

```go
OP_REF_LOCAL
OP_REF_UPVALUE
OP_REF_GLOBAL
```

Add its string and disassembly cases:

```go
case OP_REF_UPVALUE:
	return "OP_REF_UPVALUE"
```

```go
case OP_REF_UPVALUE:
	return c.byteInstruction("OP_REF_UPVALUE", offset)
```

- [ ] **Step 4: Emit `OP_REF_UPVALUE` from `compileReferenceArgument`**

Replace the current captured-variable error in `internal/compiler/compiler.go`:

```go
if upvalue, declared := c.resolveUpvalue(target.Value); upvalue != -1 {
	if ref, ok := declared.(*ast.RefType); ok {
		c.emitBytes(byte(chunk.OP_GET_UPVALUE), byte(upvalue))
		return ref.ElementType, nil
	}
	c.emitBytes(byte(chunk.OP_REF_UPVALUE), byte(upvalue))
	return declared, nil
}
```

Apply the same lowering when a prefix expression `ref captured` reaches `compileReferenceArgument`.

- [ ] **Step 5: Execute `OP_REF_UPVALUE` in the VM**

Add beside `OP_REF_LOCAL` in `internal/vm/vm.go`:

```go
case chunk.OP_REF_UPVALUE:
	slot := int(c.Code[ip])
	ip++
	if slot < 0 || slot >= len(frame.Closure.Upvalues) {
		return vm.runtimeError(c, ip, "upvalue reference index out of bounds: %d", slot)
	}
	vm.push(value.Value{
		Type: value.VAL_REF,
		Obj: &value.ObjRef{
			RefType: value.REF_UPVALUE,
			Upvalue: frame.Closure.Upvalues[slot],
		},
	})
```

- [ ] **Step 6: Write the failing-then-passing closed-upvalue VM test**

Add to `internal/vm/function_types_test.go`:

```go
func TestContextualReferenceUpdatesClosedUpvalue(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func increment(value: ref int) -> void
    *value = value + 1
end
func make_incrementer() -> func() -> int
    let value: int = 40
    return func() -> int
        increment(value)
        return value
    end
end
let incrementer: func() -> int = make_incrementer()
incrementer()
test_report(incrementer())`)
	testExpectedObject(t, 42, got)
}

func TestExplicitReferenceToClosedUpvalueCanEscape(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func make_reference_factory() -> func() -> ref int
    let value: int = 41
    return func() -> ref int
        return ref value
    end
end
let get_reference: func() -> ref int = make_reference_factory()
let pointer: ref int = get_reference()
*pointer = 42
test_report(pointer)`)
	testExpectedObject(t, 42, got)
}
```

Run before and after the VM opcode implementation:

```powershell
go test ./internal/vm -run 'TestContextualReferenceUpdatesClosedUpvalue|TestExplicitReferenceToClosedUpvalueCanEscape' -v
```

Expected before: FAIL with unknown/missing opcode behavior. Expected after: PASS with `42`.

- [ ] **Step 7: Run focused packages and commit**

```powershell
gofmt -w internal/chunk/chunk.go internal/compiler/compiler.go internal/compiler/function_types_test.go internal/vm/vm.go internal/vm/function_types_test.go
go test ./internal/compiler ./internal/vm
git add internal/chunk/chunk.go internal/compiler/compiler.go internal/compiler/function_types_test.go internal/vm/vm.go internal/vm/function_types_test.go
git commit -m "feat: support references to captured upvalues"
```

Expected: compiler and VM packages PASS.

---

### Task 3: Validate runtime reference modes and null updates

**Files:**
- Create: `internal/vm/call_validation.go`
- Modify: `internal/value/value.go`
- Modify: `internal/compiler/compiler.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/function_types_test.go`

**Interfaces:**
- Consumes: `value.ParamInfo`, `ObjFunction.Params`, runtime argument slice.
- Produces: `validateParameterModes(name string, params []value.ParamInfo, args []value.Value) error`, reused by Task 4 for native descriptors.

- [ ] **Step 1: Store human-readable parameter type names**

Change `ParamInfo` in `internal/value/value.go`:

```go
type ParamInfo struct {
	IsRef    bool
	TypeName string
}
```

Update `compileFunction` in `internal/compiler/compiler.go`:

```go
paramsInfo = append(paramsInfo, value.ParamInfo{
	IsRef:    isRef,
	TypeName: param.Type.String(),
})
```

- [ ] **Step 2: Write dynamic-mode tests**

Add a helper to `internal/vm/function_types_test.go` that compiles and returns the VM error instead of failing immediately:

```go
func runTypedFunctionProgramError(t *testing.T, input string) error {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	bytecode, _, err := compiler.New().Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}
	return New().Interpret(bytecode)
}
```

Add:

```go
func TestDynamicCallRejectsPlainValueForReferenceParameter(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
func increment(value: ref int) -> void
    return
end
let dynamic: func = increment
let answer: int = 41
dynamic(answer)`)
	if err == nil || !strings.Contains(err.Error(), "argument 1: expected ref int, got int") {
		t.Fatalf("error=%v", err)
	}
}

func TestDynamicCallRejectsReferenceForValueParameter(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
func consume(value: int) -> void
    return
end
let dynamic: func = consume
let answer: int = 41
dynamic(ref answer)`)
	if err == nil || !strings.Contains(err.Error(), "argument 1: expected int, got ref") {
		t.Fatalf("error=%v", err)
	}
}

func TestDynamicReferenceParameterAcceptsNull(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
func consume(value: ref int) -> void
    return
end
let dynamic: func = consume
dynamic(null)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpdatingNullReferenceFailsClearly(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
let pointer: ref int = null
*pointer = 1`)
	if err == nil || !strings.Contains(err.Error(), "cannot update null reference") {
		t.Fatalf("error=%v", err)
	}
}
```

- [ ] **Step 3: Run tests and verify at least one current behavior lacks the pre-frame contract**

```powershell
go test ./internal/vm -run 'TestDynamicCallRejectsPlainValueForReferenceParameter|TestDynamicCallRejectsReferenceForValueParameter|TestDynamicReferenceParameterAcceptsNull|TestUpdatingNullReferenceFailsClearly' -v
```

Expected: FAIL because `call` currently copies/skips copying without validating actual reference mode or producing these diagnostics.

- [ ] **Step 4: Implement shared mode validation**

Create `internal/vm/call_validation.go`:

```go
package vm

import (
	"fmt"
	"noxy-vm/internal/value"
)

func runtimeValueMode(v value.Value) string {
	switch v.Type {
	case value.VAL_BOOL:
		return "bool"
	case value.VAL_NULL:
		return "null"
	case value.VAL_INT:
		return "int"
	case value.VAL_FLOAT:
		return "float"
	case value.VAL_OBJ:
		return "object"
	case value.VAL_FUNCTION:
		return "func"
	case value.VAL_NATIVE:
		return "native"
	case value.VAL_BYTES:
		return "bytes"
	case value.VAL_CHANNEL:
		return "chan"
	case value.VAL_WAITGROUP:
		return "waitgroup"
	case value.VAL_REF:
		return "ref"
	default:
		return "unknown"
	}
}

func validateParameterModes(name string, params []value.ParamInfo, args []value.Value) error {
	for i, param := range params {
		if i >= len(args) {
			break
		}
		actual := args[i]
		if param.IsRef && actual.Type != value.VAL_REF && actual.Type != value.VAL_NULL {
			return fmt.Errorf("function '%s' argument %d: expected %s, got %s", name, i+1, param.TypeName, runtimeValueMode(actual))
		}
		if !param.IsRef && actual.Type == value.VAL_REF {
			return fmt.Errorf("function '%s' argument %d: expected %s, got ref", name, i+1, param.TypeName)
		}
	}
	return nil
}
```

- [ ] **Step 5: Call validation before shallow-copy preparation**

In `VM.call`, build the argument slice before the copy loop:

```go
baseArgs := vm.stackTop - argCount
args := vm.stack[baseArgs:vm.stackTop]
if err := validateParameterModes(fn.Name, fn.Params, args); err != nil {
	return false, vm.runtimeError(c, ip, "%s", err)
}
```

Keep the existing non-reference `copyValue` loop unchanged after validation.

In the `OP_STORE_REF` handler, distinguish the nullable reference value before the generic non-reference error:

```go
if refVal.Type == value.VAL_NULL {
	return vm.runtimeError(c, ip, "cannot update null reference")
}
if refVal.Type != value.VAL_REF {
	return vm.runtimeError(c, ip, "cannot store via non-reference value")
}
```

Do not change `OP_DEREF` null propagation; nullable reference reads and comparisons continue to produce `null`.

- [ ] **Step 6: Run tests and commit**

```powershell
gofmt -w internal/value/value.go internal/compiler/compiler.go internal/vm/call_validation.go internal/vm/vm.go internal/vm/function_types_test.go
go test ./internal/compiler ./internal/vm
git add internal/value/value.go internal/compiler/compiler.go internal/vm/call_validation.go internal/vm/vm.go internal/vm/function_types_test.go
git commit -m "feat: validate runtime reference boundaries"
```

Expected: PASS, including the existing explicit dynamic reference test.

---

### Task 4: Add typed native descriptors and pre-invocation validation

**Files:**
- Modify: `internal/value/value.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/call_validation.go`
- Create: `internal/vm/native_signatures_test.go`

**Interfaces:**
- Consumes: Task 3 `value.ParamInfo` and `validateParameterModes`.
- Produces: `value.NativeSignature`, `value.NewNativeWithSignature`, and `VM.DefineNativeWithSignature` for Task 5.

- [ ] **Step 1: Write failing typed-native validation tests**

Create `internal/vm/native_signatures_test.go` with a helper that compiles dynamic native calls and registers a descriptor:

```go
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
		Arity: 1,
		Params: []value.ParamInfo{{IsRef: true, TypeName: "ref int"}},
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
```

- [ ] **Step 2: Run and verify missing descriptor APIs fail compilation**

```powershell
go test ./internal/vm -run TestTypedNativeRejectsModeBeforeInvocation -v
```

Expected: build FAIL because `NativeSignature` and `DefineNativeWithSignature` do not exist.

- [ ] **Step 3: Add optional signatures to native values**

In `internal/value/value.go`:

```go
type NativeSignature struct {
	Arity      int
	Variadic   bool
	Params     []ParamInfo
	ReturnType string
}

type ObjNative struct {
	Name      string
	Fn        NativeFunc
	Signature *NativeSignature
}

func NewNativeWithSignature(name string, signature NativeSignature, fn NativeFunc) Value {
	return Value{
		Type: VAL_NATIVE,
		Obj: &ObjNative{Name: name, Fn: fn, Signature: &signature},
	}
}
```

Keep `NewNative` creating `Signature: nil` for dynamic internal natives.

- [ ] **Step 4: Register and validate typed natives**

Add to `internal/vm/vm.go`:

```go
func (vm *VM) DefineNativeWithSignature(name string, signature value.NativeSignature, fn value.NativeFunc) {
	if _, ok := vm.GetGlobal(name); ok {
		return
	}
	vm.SetGlobal(name, value.NewNativeWithSignature(name, signature, fn))
}
```

In the native branch of `callValue`, validate arity and modes before `native.Fn(args)`. For a typed native, copy ordinary arguments with the same shallow-copy operation used by script functions; pass reference arguments unchanged. Untyped internal natives retain their current raw boundary:

```go
if native.Signature != nil {
	sig := native.Signature
	if !sig.Variadic && argCount != sig.Arity {
		return false, vm.runtimeError(c, ip, "native '%s' expects %d arguments, got %d", native.Name, sig.Arity, argCount)
	}
	if sig.Variadic && argCount < sig.Arity {
		return false, vm.runtimeError(c, ip, "native '%s' expects at least %d arguments, got %d", native.Name, sig.Arity, argCount)
	}
	params := sig.Params
	if sig.Variadic && len(params) > 0 && argCount > len(params) {
		expanded := make([]value.ParamInfo, argCount)
		copy(expanded, params)
		for i := len(params); i < argCount; i++ {
			expanded[i] = params[len(params)-1]
		}
		params = expanded
	}
	if err := validateParameterModes(native.Name, params, args); err != nil {
		return false, vm.runtimeError(c, ip, "%s", err)
	}
	callArgs := make([]value.Value, len(args))
	copy(callArgs, args)
	for i, param := range params {
		if i >= len(callArgs) {
			break
		}
		if !param.IsRef {
			callArgs[i] = vm.copyValue(callArgs[i])
		}
	}
	args = callArgs
}
```

- [ ] **Step 5: Add success, arity, ordinary-ref rejection, and shallow-copy cases**

Extend `internal/vm/native_signatures_test.go`:

```go
func TestTypedNativeAcceptsExplicitReference(t *testing.T) {
	called := false
	sig := value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{IsRef: true, TypeName: "ref int"}}, ReturnType: "void"}
	err := runWithTypedTestNative(t, "let n: int = 1\ntyped_test(ref n)", sig, &called)
	if err != nil || !called {
		t.Fatalf("called=%v error=%v", called, err)
	}
}
```

Add equivalent assertions for incorrect exact arity, fewer-than-minimum variadic arity, and passing `ref n` to `ParamInfo{IsRef:false, TypeName:"int"}`. Add this test to prove that a typed native receives an ordinary composite with the same shallow pass-by-value behavior as a script function:

```go
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
		Arity: 1,
		Params: []value.ParamInfo{{IsRef: false, TypeName: "int[]"}},
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
```

- [ ] **Step 6: Run tests and commit**

```powershell
gofmt -w internal/value/value.go internal/vm/vm.go internal/vm/call_validation.go internal/vm/native_signatures_test.go
go test ./internal/vm
git add internal/value/value.go internal/vm/vm.go internal/vm/call_validation.go internal/vm/native_signatures_test.go
git commit -m "feat: validate typed native contracts"
```

Expected: PASS.

---

### Task 5: Give mutating builtins exact contextual-reference behavior

**Files:**
- Create: `internal/compiler/builtin_calls.go`
- Create: `internal/compiler/builtin_calls_test.go`
- Modify: `internal/compiler/compiler.go`
- Create: `internal/vm/references.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/native_signatures_test.go`

**Interfaces:**
- Consumes: `Compiler.compileReferenceArgument`, Task 4 typed native descriptors, and existing `ObjRef` variants.
- Produces: `compileBuiltinCall(*ast.CallExpression) (handled bool, result ast.NoxyType, err error)` and `VM.resolveReferenceValue(value.Value) (value.Value, error)`.

- [ ] **Step 1: Write compiler tests that preserve existing builtin syntax**

Create `internal/compiler/builtin_calls_test.go`:

```go
package compiler

import (
	"strings"
	"testing"
)

func TestMutatingBuiltinsBorrowAddressableArguments(t *testing.T) {
	_, err := compileFunctionSource(t, `
let values: int[] = [1]
let mapping: map[string, int] = {"a": 1}
append(values, 2)
let removed: int = pop(values)
delete(mapping, "a")`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMutatingBuiltinsRejectNonAddressableArguments(t *testing.T) {
	_, err := compileFunctionSource(t, `append([1], 2)`)
	if err == nil || !strings.Contains(err.Error(), "not addressable") {
		t.Fatalf("error=%v", err)
	}
}

func TestAppendChecksElementType(t *testing.T) {
	_, err := compileFunctionSource(t, `
let values: int[] = [1]
append(values, "wrong")`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}
```

- [ ] **Step 2: Run and verify generic native calls lack these static contracts**

```powershell
go test ./internal/compiler -run 'TestMutatingBuiltins' -v
```

Expected: FAIL for non-addressable/type-check cases because these identifiers currently cross an untyped native boundary.

- [ ] **Step 3: Add focused builtin call compilation**

First, change the default branch of `compileReferenceArgument` to use a stable addressability diagnostic:

```go
return nil, fmt.Errorf(
	"[line %d] reference argument '%s' is not addressable\n  hint: use a variable, property, index, or null",
	c.currentLine, expression.String(),
)
```

Then create `internal/compiler/builtin_calls.go` with this complete lowering for `append`, `pop`, `delete`, and `json_loads`:

```go
package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
)

func builtinType(name string) ast.NoxyType {
	return &ast.PrimitiveType{Name: name}
}

func (c *Compiler) compileBuiltinValueArgument(expression ast.Expression) (ast.NoxyType, error) {
	_, actual, err := c.Compile(expression)
	if err != nil {
		return nil, err
	}
	if ref, ok := actual.(*ast.RefType); ok {
		c.emitByte(byte(chunk.OP_DEREF))
		return ref.ElementType, nil
	}
	return actual, nil
}

func (c *Compiler) compileBuiltinCall(call *ast.CallExpression) (bool, ast.NoxyType, error) {
	ident, ok := call.Function.(*ast.Identifier)
	if !ok {
		return false, nil, nil
	}
	name := ident.Value
	if name != "append" && name != "pop" && name != "delete" && name != "json_loads" {
		return false, nil, nil
	}
	if slot, _ := c.resolveLocal(name); slot != -1 {
		return false, nil, nil
	}
	if _, declared := c.resolveGlobalType(name); declared {
		return false, nil, nil
	}

	wantArity := map[string]int{"append": 2, "pop": 1, "delete": 2, "json_loads": 2}[name]
	if len(call.Arguments) != wantArity {
		return true, nil, fmt.Errorf(
			"[line %d] %s expects %d arguments, got %d",
			c.currentLine, name, wantArity, len(call.Arguments),
		)
	}
	if _, _, err := c.Compile(call.Function); err != nil {
		return true, nil, err
	}

	switch ident.Value {
	case "append":
		container, err := c.compileReferenceArgument(call.Arguments[0])
		if err != nil {
			return true, nil, err
		}
		array, ok := container.(*ast.ArrayType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] append expects an array, got %s", c.currentLine, noxyTypeName(container))
		}
		item, err := c.compileBuiltinValueArgument(call.Arguments[1])
		if err != nil {
			return true, nil, err
		}
		if !c.areStrictTypesCompatible(array.ElementType, item) {
			return true, nil, fmt.Errorf(
				"[line %d] argument 2 to 'append': expected %s, got %s",
				c.currentLine, noxyTypeName(array.ElementType), noxyTypeName(item),
			)
		}
		c.emitBytes(byte(chunk.OP_CALL), 2)
		return true, builtinType("void"), nil
	case "pop":
		container, err := c.compileReferenceArgument(call.Arguments[0])
		if err != nil {
			return true, nil, err
		}
		array, ok := container.(*ast.ArrayType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] pop expects an array, got %s", c.currentLine, noxyTypeName(container))
		}
		c.emitBytes(byte(chunk.OP_CALL), 1)
		return true, array.ElementType, nil
	case "delete":
		container, err := c.compileReferenceArgument(call.Arguments[0])
		if err != nil {
			return true, nil, err
		}
		mapping, ok := container.(*ast.MapType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] delete expects a map, got %s", c.currentLine, noxyTypeName(container))
		}
		key, err := c.compileBuiltinValueArgument(call.Arguments[1])
		if err != nil {
			return true, nil, err
		}
		if !c.areStrictTypesCompatible(mapping.KeyType, key) {
			return true, nil, fmt.Errorf(
				"[line %d] argument 2 to 'delete': expected %s, got %s",
				c.currentLine, noxyTypeName(mapping.KeyType), noxyTypeName(key),
			)
		}
		c.emitBytes(byte(chunk.OP_CALL), 2)
		return true, builtinType("void"), nil
	case "json_loads":
		jsonText, err := c.compileBuiltinValueArgument(call.Arguments[0])
		if err != nil {
			return true, nil, err
		}
		if !c.areStrictTypesCompatible(builtinType("string"), jsonText) {
			return true, nil, fmt.Errorf(
				"[line %d] argument 1 to 'json_loads': expected string, got %s",
				c.currentLine, noxyTypeName(jsonText),
			)
		}
		if _, err := c.compileReferenceArgument(call.Arguments[1]); err != nil {
			return true, nil, err
		}
		c.emitBytes(byte(chunk.OP_CALL), 2)
		return true, builtinType("bool"), nil
	default:
		return false, nil, nil
	}
}
```

Call the helper at the start of the `ast.CallExpression` branch, before generic native/dynamic handling:

```go
if handled, resultType, err := c.compileBuiltinCall(n); handled {
	return c.currentChunk, resultType, err
}
```

Add `TestMutatingBuiltinNamesCanBeShadowed` to define a user function named `append` and verify its declared signature takes precedence over builtin lowering.

- [ ] **Step 4: Write VM tests showing builtin mutation still reaches the caller**

Add to `internal/vm/native_signatures_test.go`:

```go
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
```

Run now:

```powershell
go test ./internal/vm -run 'TestTypedMutatingBuiltinsPreserveSourceSyntax|TestTypedDeleteMutatesCallerMap|TestTypedJSONLoadsPopulatesCallerTarget' -v
```

Expected after compiler lowering but before native changes: FAIL because the native receives `VAL_REF` instead of `VAL_OBJ`.

- [ ] **Step 5: Add centralized native reference resolution**

Create `internal/vm/references.go`:

```go
package vm

import (
	"fmt"
	"noxy-vm/internal/value"
)

func referenceMapKey(index value.Value) (interface{}, bool) {
	switch index.Type {
	case value.VAL_INT:
		return index.AsInt, true
	case value.VAL_OBJ:
		key, ok := index.Obj.(string)
		return key, ok
	default:
		return nil, false
	}
}

func (vm *VM) resolveReferenceValue(input value.Value) (value.Value, error) {
	if input.Type == value.VAL_NULL {
		return value.Value{}, fmt.Errorf("cannot dereference null reference")
	}
	if input.Type != value.VAL_REF {
		return value.Value{}, fmt.Errorf("expected reference value, got %s", runtimeValueMode(input))
	}
	ref := input.Obj.(*value.ObjRef)
	switch ref.RefType {
	case value.REF_GLOBAL:
		if vm.currentFrame != nil && vm.currentFrame.Globals != nil {
			if result, ok := vm.currentFrame.Globals[ref.Name]; ok {
				return result, nil
			}
		}
		if result, ok := vm.GetGlobal(ref.Name); ok {
			return result, nil
		}
	case value.REF_UPVALUE:
		return *ref.Upvalue.Location, nil
	case value.REF_PTR:
		return *ref.Ptr, nil
	case value.REF_PROPERTY:
		if instance, ok := ref.Container.Obj.(*value.ObjInstance); ok {
			return instance.Fields[ref.Name], nil
		}
	case value.REF_INDEX:
		if array, ok := ref.Container.Obj.(*value.ObjArray); ok {
			if ref.Index.Type != value.VAL_INT {
				return value.Value{}, fmt.Errorf("array reference index must be int")
			}
			index := int(ref.Index.AsInt)
			if index >= 0 && index < len(array.Elements) {
				return array.Elements[index], nil
			}
			return value.Value{}, fmt.Errorf("array reference index out of bounds")
		}
		if mapping, ok := ref.Container.Obj.(*value.ObjMap); ok {
			key, ok := referenceMapKey(ref.Index)
			if !ok {
				return value.Value{}, fmt.Errorf("map reference key must be int or string")
			}
			result, ok := mapping.Data[key]
			if !ok {
				return value.Value{}, fmt.Errorf("map reference key not found")
			}
			return result, nil
		}
	}
	return value.Value{}, fmt.Errorf("invalid reference target")
}
```

- [ ] **Step 6: Register and update the four mutating natives**

Change their registrations in `internal/vm/vm.go` to `DefineNativeWithSignature` with these exact descriptors:

```go
appendSignature := value.NativeSignature{
	Arity: 2,
	Params: []value.ParamInfo{
		{IsRef: true, TypeName: "ref array"},
		{IsRef: false, TypeName: "any"},
	},
	ReturnType: "void",
}

popSignature := value.NativeSignature{
	Arity: 1,
	Params: []value.ParamInfo{
		{IsRef: true, TypeName: "ref array"},
	},
	ReturnType: "any",
}

deleteSignature := value.NativeSignature{
	Arity: 2,
	Params: []value.ParamInfo{
		{IsRef: true, TypeName: "ref map"},
		{IsRef: false, TypeName: "any"},
	},
	ReturnType: "void",
}

jsonLoadsSignature := value.NativeSignature{
	Arity: 2,
	Params: []value.ParamInfo{
		{IsRef: false, TypeName: "string"},
		{IsRef: true, TypeName: "ref any"},
	},
	ReturnType: "bool",
}
```

Pass the corresponding descriptor to each registration. At the start of `append`, `pop`, and `delete`, resolve `args[0]`:

```go
arrVal, err := vm.resolveReferenceValue(args[0])
if err != nil {
	return value.NewNull()
}
```

Keep passing `json_loads` argument 2 directly to the existing `populateTarget` function because that path already consumes `VAL_REF` and writes through every supported reference target. Descriptor validation guarantees reference mode; the focused compiler lowering guarantees exact static types. Preserve existing domain return conventions (`null` for failed collection operations and `false` for failed JSON population).

- [ ] **Step 7: Run focused/full tests and commit**

```powershell
gofmt -w internal/compiler/builtin_calls.go internal/compiler/builtin_calls_test.go internal/compiler/compiler.go internal/vm/references.go internal/vm/vm.go internal/vm/native_signatures_test.go
go test ./internal/compiler ./internal/vm
go test ./...
git add internal/compiler/builtin_calls.go internal/compiler/builtin_calls_test.go internal/compiler/compiler.go internal/vm/references.go internal/vm/vm.go internal/vm/native_signatures_test.go
git commit -m "feat: type mutating native builtins"
```

Expected: all tests PASS and existing builtin call syntax remains unchanged.

---

### Task 6: Align language documentation and conformance fixtures

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md`
- Modify: `docs/REF_SEMANTICS.md`
- Modify: `README.md`
- Modify: `noxy_examples/typed_function_conformance.nx`
- Modify: `noxy_examples/type_errors/typed_function_invalid_ref_argument.nx`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: final compiler/VM behavior from Tasks 1-5.
- Produces: normative user documentation and runnable positive/negative examples.

- [ ] **Step 1: Correct update/rebind examples in the language spec**

Replace the invalid example:

```diff
 func double_it(val: ref int)
-    val = val * 2
+    *val = val * 2
 end
```

Add the normative contextual-call rule:

```noxy
let value: int = 10
double_it(value)       // exact signature borrows addressable value
double_it(ref value)   // explicit reference is also valid
```

State that non-null literals and plain function-result temporaries are not addressable. `null` remains the explicit nullable `ref T` value and is accepted without pretending that it has a storage slot.

- [ ] **Step 2: Rewrite the focused reference guide around three operations**

Ensure `docs/REF_SEMANTICS.md` contains this exact summary table:

```markdown
| Form | Meaning |
|---|---|
| `f(value)` where exact parameter is `ref T` | Contextual reference conversion |
| `ref value` | Create a first-class reference |
| `*reference = value` | Update referenced storage |
| `reference = ref other` | Rebind the reference value |
```

Document that bare `func` requires `ref value` because its signature is dynamic.

- [ ] **Step 3: Correct the static typing claim**

In `docs/NOXY_LANGUAGE_SPEC.md` and `README.md`, use:

```markdown
Noxy is statically typed, with explicit dynamic boundaries through `any`, bare
`func`, untyped native primitives, and plugins without signatures.
```

Replace “immutable types” with “type-stable variables” and explain that the declared type is stable while values may be mutable.

- [ ] **Step 4: Expand positive and negative fixtures**

In `noxy_examples/typed_function_conformance.nx`, retain `increment(answer)` and add:

```noxy
func make_counter() -> func() -> int
    let count: int = 40
    return func() -> int
        increment(count)
        return count
    end
end

let counter: func() -> int = make_counter()
assert(counter() == 41, "first captured ref update must produce 41")
assert(counter() == 42, "second captured ref update must produce 42")
```

Keep `noxy_examples/type_errors/typed_function_invalid_ref_argument.nx` as:

```noxy
func increment(value: ref int) -> void
    *value = value + 1
end

increment(41)
```

Expected diagnostic must include `not addressable`.

- [ ] **Step 5: Update changelog**

Add under a new unreleased section:

```markdown
## [Unreleased]

### Added

- Typed public native contracts and runtime reference-mode validation.
- Safe references to captured upvalues.

### Fixed

- Reference documentation now distinguishes contextual passing, update, and rebind.
- Dynamic calls reject incompatible reference modes before entering the callee.
```

- [ ] **Step 6: Run documentation-backed conformance tests and commit**

```powershell
go test ./internal/compiler ./internal/vm
go run cmd/noxy/main.go noxy_examples/typed_function_conformance.nx
go run cmd/noxy/main.go noxy_examples/type_errors/typed_function_invalid_ref_argument.nx
```

Expected: positive fixture prints `typed function conformance: PASS`; negative fixture exits non-zero with `not addressable`.

```powershell
git add docs/NOXY_LANGUAGE_SPEC.md docs/REF_SEMANTICS.md README.md noxy_examples/typed_function_conformance.nx noxy_examples/type_errors/typed_function_invalid_ref_argument.nx CHANGELOG.md
git commit -m "docs: specify contextual reference semantics"
```

---

### Task 7: Run full verification and close migration gaps

**Files:**
- Modify only files revealed by verification failures, within the approved reference/native scope.

**Interfaces:**
- Consumes: all earlier tasks.
- Produces: a clean branch whose unit, integration, build, formatting, and vet checks pass.

- [ ] **Step 1: Format all changed Go files**

```powershell
gofmt -w internal/chunk/chunk.go internal/compiler/compiler.go internal/compiler/builtin_calls.go internal/compiler/builtin_calls_test.go internal/compiler/function_types_test.go internal/value/value.go internal/vm/call_validation.go internal/vm/references.go internal/vm/vm.go internal/vm/function_types_test.go internal/vm/native_signatures_test.go
```

Expected: command exits 0.

- [ ] **Step 2: Run all Go tests**

```powershell
go test ./...
```

Expected: PASS for every package.

- [ ] **Step 3: Run race-enabled reference tests**

```powershell
go test -race ./internal/compiler ./internal/vm
```

Expected: PASS with no race report. If the installed Windows Go toolchain cannot build `runtime/race`, record that environment limitation and do not claim race verification.

- [ ] **Step 4: Build the CLI used by the integration runner**

```powershell
go build -o noxy.exe ./cmd/noxy
```

Expected: `noxy.exe` is produced and exits successfully for a simple example.

- [ ] **Step 5: Run the required concurrent Noxy suite**

```powershell
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: final report contains `Falhou: 0` and `TODOS TESTES PASSARAM.`

- [ ] **Step 6: Run build and vet gates**

```powershell
go build ./...
go vet ./...
```

Expected: both commands exit 0 with no diagnostics.

- [ ] **Step 7: Inspect the final diff and route failures back to their owning task**

```powershell
git diff --check
git status --short
git diff --stat develop...HEAD
```

Expected: no whitespace errors; only approved source, test, fixture, and documentation files differ.

If verification exposes a defect, return to the task that owns the affected file, add a focused regression test, implement the smallest in-scope correction, rerun that task's focused commands, and then repeat Steps 1-7. Do not create a generic verification-only commit.

- [ ] **Step 8: Record final evidence**

Capture the exact successful outputs for `go test ./...`, the concurrent Noxy suite, `go build ./...`, and `go vet ./...` in the handoff. Do not claim race validation if Step 3 was unavailable.
