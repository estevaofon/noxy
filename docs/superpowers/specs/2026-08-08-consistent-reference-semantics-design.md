# Consistent Reference Semantics Design

**Status:** Approved for implementation
**Branch:** `feat/consistent-reference-semantics`

## Summary

This feature closes observed gaps in Noxy's reference model without redesigning its established semantics.

Noxy already has a coherent core:

- ordinary composite parameters receive shallow copies;
- `ref T` parameters access addressable caller storage;
- exact calls may borrow an addressable value contextually;
- reference reads auto-dereference;
- `*reference = value` updates the target;
- `reference = ref other` rebinds the reference.

The work will preserve those rules, correct contradictory documentation, make native functions obey the same public contracts, validate reference modes across dynamic calls, and add missing reference support for captured variables.

## Goals

1. Specify contextual borrowing as a deliberate language rule.
2. Preserve current call syntax for exact functions and mutating builtins.
3. Make user-defined and public native functions follow the same parameter modes.
4. Validate reference modes at dynamic boundaries.
5. Support references to captured variables through upvalues.
6. Correct documentation and add executable conformance tests.

## Non-goals

- Requiring `ref` at every exact call site.
- Adding `mut`, `readonly`, ownership, or borrow checking.
- Changing shallow-copy behavior.
- Changing assignment semantics for arrays, maps, or structs.
- Adding deep-copy or copy-on-write semantics.
- Removing automatic dereference for reads.
- Removing `any` or bare `func`.
- Rewriting every native's domain-level error convention.

## 1. Copy and Sharing

### 1.1 Assignment

Assignment behavior remains unchanged:

- scalar values are copied;
- arrays, maps, structs, functions, channels, resources, and plugins preserve their heap-backed identity;
- assigning a `ref T` copies the reference value and preserves its target.

```noxy
let first: int[] = [1, 2]
let alias: int[] = first
alias[0] = 99
print(first[0]) // 99
```

### 1.2 Ordinary parameters

A user-defined parameter whose type is not `ref T` receives the existing shallow copy of arrays, maps, and structs. Top-level mutation remains local to the callee; nested composite identities remain shared.

```noxy
func change(values: int[]) -> void
    values[0] = 99
    append(values, 3)
end

let original: int[] = [1, 2]
change(original)
print(original) // [1, 2]
```

```noxy
func change_nested(matrix: int[][]) -> void
    matrix[0][0] = 99
end

let row: int[] = [1, 2]
let matrix: int[][] = [row]
change_nested(matrix)
print(matrix[0][0]) // 99
```

No new copy rule is introduced.

### 1.3 Returns

Returning a composite adds no new implicit copy. A non-reference parameter has already been shallow-copied on entry.

## 2. Contextual Borrowing

### 2.1 Exact reference parameters

When an exact signature expects `ref T`, an addressable expression of type `T` is converted contextually to a reference argument at the call site. The reference is not lifetime-limited to the call and may escape under the rules in Section 2.3.

```noxy
func increment(value: ref int) -> void
    *value = value + 1
end

let answer: int = 41
increment(answer)
print(answer) // 42
```

The compiler verifies that `answer` is addressable and emits the existing reference bytecode. This is a temporary call argument, not a new source-level variable.

The following expressions are addressable:

- local and global variables;
- struct fields;
- array and map index slots;
- captured variables once upvalue reference support is implemented.

Literals and non-reference temporaries are not addressable:

```noxy
increment(41)          // compile-time error
increment(make_int())  // compile-time error when make_int returns int
```

A function result whose declared type is already `ref T` is a reference value and may be passed.

### 2.2 Existing and explicit references

An existing `ref T` value passes directly:

```noxy
let pointer: ref int = ref answer
increment(pointer)
```

The explicit spelling also remains valid for an exact call:

```noxy
increment(ref answer)
```

The canonical concise spelling for an exact call is `increment(answer)`. `ref answer` creates an explicit first-class reference value and is useful when storing the reference or crossing a dynamic boundary.

### 2.3 Lifetime

The contextual conversion occurs at the call site, but the resulting reference may escape through a return, closure, field, or global. When a referenced local can escape, the compiler captures it through the existing upvalue model. Escaping references must target an upvalue-backed location, never an expired raw stack slot. This feature does not add borrow-checker restrictions or prohibit reference escape.

## 3. Reference Operations

The existing operations become normative.

### 3.1 Read

Using `r: ref T` where a value `T` is required automatically dereferences it:

```noxy
let result: int = r + 1
print(r)
```

### 3.2 Update

`*r = value` updates the referenced storage:

```noxy
func increment(value: ref int) -> void
    *value = value + 1
end
```

The documentation example `value = value + 1` is invalid and will be corrected. The compiler already rejects it because a bare assignment to `value: ref int` is a rebind and therefore requires `ref int` on the right-hand side.

### 3.3 Rebind

`r = ref other` changes the target stored in the reference variable:

```noxy
let first: int = 10
let second: int = 20
let pointer: ref int = ref first

pointer = ref second
*pointer = 21
```

Rebinding a reference parameter changes only the parameter's local reference value. It does not rebind a reference variable held by the caller. The existing compiler warning remains.

### 3.4 Null

`null` remains a valid `ref T` value. It has no target and may be stored, compared, returned, or rebound. Reading through automatic dereference propagates `null`, which keeps nullable-reference comparisons usable. Updating through `null` is a runtime error.

A field or index slot whose stored reference is currently `null` remains addressable; a contextual borrow can pass that slot so the callee may fill it. The literal `null` itself has no addressable slot.

## 4. Dynamic Calls

Bare `func` intentionally erases exact parameter information. Therefore contextual borrowing is unavailable when the compiler only knows `func`.

```noxy
let dynamic: func = increment

dynamic(ref answer) // passes an explicit reference
dynamic(answer)     // passes a plain int
```

At runtime, the second call fails before starting the target frame because the actual function expects a reference parameter.

Runtime call preparation will enforce:

- a declared reference parameter accepts a reference value or `null`;
- a plain value is never converted into a reference dynamically;
- an ordinary parameter retains shallow-copy behavior;
- an explicit reference passed to an incompatible ordinary parameter is rejected rather than guessed or silently dereferenced.

Reference-mode errors include the function name and argument position. Full runtime reconstruction of every static Noxy type is outside this feature; dynamic mode validation distinguishes reference from non-reference values.

## 5. Captured Variables

The specification already promises safe references to captured variables, but the compiler currently rejects passing a captured variable by reference.

This feature closes that implementation gap:

```noxy
func make_incrementer() -> func() -> void
    let value: int = 0

    return func() -> void
        increment(value)
    end
end
```

The compiler will emit a new `OP_REF_UPVALUE` when contextual borrowing or `ref value` targets a captured variable. The VM creates an `ObjRef` with `REF_UPVALUE`, using the existing `ObjUpvalue` location and closed-value lifetime.

Acceptance requirements:

- open captured locals refer to the live stack slot;
- closed captured locals refer to the heap-backed closed value;
- references remain valid after the defining function returns;
- multiple closures observe updates to the same captured slot;
- no escaping reference stores a raw pointer to an expired stack slot.

## 6. Public Native Contracts

### 6.1 Signature metadata

Public native APIs receive descriptors containing:

- name;
- arity and variadic behavior;
- parameter type names for diagnostics;
- parameter reference modes;
- return type.

The Go `NativeFunc` ABI remains unchanged. The VM descriptor validates arity and reference modes before invoking the Go closure. Exact static type validation belongs to a compiler-visible signature or typed `.nx` wrapper; the native implementation retains its domain validation at a dynamic boundary.

### 6.2 Same contextual rule

A compiler-visible public native signature uses the same contextual borrowing rule as an exact Noxy function. The runtime descriptor and the compiler signature are paired parts of one public contract; a descriptor registered only inside the VM cannot retroactively give the compiler exact call-site information.

Conceptual builtin signatures:

```text
append(ref T[], T) -> void
pop(ref T[]) -> T
delete(ref map[K, V], K) -> void
json_loads(string, ref T) -> bool
```

Source syntax remains unchanged:

```noxy
append(values, item)
let removed: int = pop(values)
delete(mapping, key)
json_loads(json, target)
```

Because these signatures are known by the compiler, it borrows the addressable arguments contextually. Their matching VM descriptors verify the reference modes again before native execution. These builtins no longer depend on an undocumented native exception.

Generic collection builtins remain compiler-recognized because Noxy does not yet expose user-defined generics.

### 6.3 Ordinary native parameters

An ordinary composite parameter has the same observable shallow-copy semantics as an ordinary Noxy parameter. A proven read-only native may operate directly on the value as an optimization because no mutation is observable.

Identity-bearing channels, wait groups, files, sockets, statements, and database handles may change external resource state through ordinary parameters. This does not rebind the caller's Noxy variable or change the language's composite-copy contract.

### 6.4 Stdlib and internal primitives

Public stdlib APIs must be typed `.nx` wrappers or paired compiler/VM native contracts. Raw implementation primitives and VM-only descriptors remain dynamic internals and are not documented public APIs.

Enforcing module-private native visibility is desirable but is a separate module-system change. Until then, direct calls to raw primitives are documented dynamic boundaries and receive only the runtime checks implemented by those primitives; they never gain contextual borrowing.

### 6.5 Contract errors

Descriptor-level arity and reference-mode failures become VM runtime errors and never invoke the native closure. Compiler-visible contracts reject exact type mismatches at compile time. Native domain errors may continue using existing Noxy `Result` contracts. Replacing every legacy `null` error convention is outside this feature.

## 7. Static and Dynamic Boundaries

The language specification will say:

> Noxy is statically typed, with explicit dynamic boundaries through `any`, bare `func`, untyped native primitives, and plugins without signatures.

Normative rules:

- exact calls validate arity, types, and reference modes at compile time;
- exact-to-bare-function widening remains valid;
- bare-to-exact narrowing remains invalid;
- concrete values widen to `any`;
- `any` does not narrow implicitly to concrete or exact callable types;
- dynamic boundaries validate what is known at runtime and never manufacture references;
- current plugin exports remain dynamic and never receive contextual borrowing; adding compiler-visible plugin signatures is separate work.

The misleading phrase "immutable types" will be replaced by "type-stable variables": a variable's declared type does not change, although values may be mutable.

## 8. Diagnostics

Required compile-time diagnostics include:

```text
reference argument '41' is not addressable
hint: use a variable, property, index, or null
```

```text
cannot assign int to ref int
hint: use '*value = ...' to update the referenced value
```

Required runtime diagnostics include:

```text
function 'increment' argument 1: expected ref int, got int
```

```text
cannot update null reference
```

## 9. Compatibility

Valid exact-call source syntax remains compatible:

```noxy
increment(answer)
append(values, item)
pop(values)
delete(mapping, key)
json_loads(json, target)
```

Behavioral tightening is limited to invalid or dynamic cases:

- dynamic calls no longer pass plain values into reference parameters;
- raw native calls with descriptor violations fail before invocation;
- captured-variable references that previously failed now compile and execute safely;
- incorrect documentation examples are fixed.

No compatibility flag or mass source migration is required.

## 10. Implementation Boundaries

### Compiler

- Preserve contextual borrowing for exact `ref T` parameters.
- Preserve direct passing of existing reference values.
- Add contextual borrowing for compiler-known public natives.
- Add `OP_REF_UPVALUE` generation.
- Improve diagnostics for non-addressable reference arguments and invalid ref assignment.

### Runtime value model

- Continue recording reference mode in function parameter metadata.
- Add native signature descriptors without importing AST types into the value package.
- Reuse `ObjRef` and `REF_UPVALUE` for captured storage.

### VM

- Validate parameter reference modes before starting dynamic script frames.
- Validate native descriptors before invoking native closures.
- Preserve shallow `copyValue` behavior for ordinary script parameters.
- Execute `OP_REF_UPVALUE` using the existing open/closed upvalue representation.

## 11. Verification

### Compiler tests

- exact contextual borrowing works for locals, globals, fields, indexes, and captured variables;
- explicit and existing references still work;
- literals and non-reference temporaries are rejected;
- update and rebind diagnostics are distinct;
- compiler-known native signatures apply the same contextual rules;
- exact-call return propagation remains unchanged.

### VM tests

- updates through local, global, property, index, and upvalue references reach the intended slot;
- dynamic calls accept explicit references and reject plain values for ref parameters;
- nullable reference reads propagate `null`, while updates through `null` fail at runtime;
- reference rebind remains local to the reference variable;
- ordinary parameters retain top-level isolation and nested sharing;
- public native parameter modes match user-function behavior.

### Integration tests

- add positive and negative conformance fixtures;
- run `go test ./...`;
- run `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx`;
- run `go build ./...` and `go vet ./...`.

## 12. Acceptance Criteria

1. Exact user calls and compiler-known public native calls use the same contextual borrowing rule.
2. Existing valid call syntax remains valid.
3. Dynamic calls never infer or manufacture references.
4. Reference read, update, and rebind semantics match compiler behavior and documentation.
5. Captured variables can be referenced safely after closure.
6. Ordinary shallow-copy semantics remain unchanged.
7. Static and dynamic boundaries are described accurately.
8. Conformance and integration suites pass.
