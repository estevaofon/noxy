# Consistent Reference Semantics Design

**Status:** Draft for user review  
**Target:** Noxy 2.0  
**Branch:** `feat/consistent-reference-semantics`

## Summary

Noxy keeps its existing shallow pass-by-value behavior for ordinary user-defined function parameters. This design removes the inconsistencies around that model instead of replacing it:

- creating a reference at a call site becomes explicit;
- reading, updating, and rebinding a reference each have one documented meaning;
- dynamic calls validate reference parameters at runtime instead of manufacturing references implicitly;
- public native APIs declare whether a parameter is passed by value or by reference;
- native collection mutators use explicit reference arguments;
- `any`, bare `func`, plugins, and untyped native exports are documented as dynamic boundaries;
- normative tests become the executable definition of these rules.

This is a breaking change because existing calls that rely on contextual address-taking must add `ref`. Mutating builtins that currently receive composites directly must also add `ref`.

## Goals

1. Make reference creation visible in source code.
2. Apply the same public parameter contract to Noxy functions and native functions.
3. Preserve Noxy's established shallow-copy behavior for ordinary parameters.
4. Preserve automatic dereference for reads and explicit syntax for writes and rebinds.
5. Make dynamic boundaries honest and runtime-safe.
6. Produce precise diagnostics and conformance tests.

## Non-goals

- Deep-copy or copy-on-write semantics.
- An ownership, borrow-checking, `mut`, or `readonly` system.
- Changing assignment semantics for arrays, maps, or structs.
- Removing `any` or bare `func`.
- Redesigning resource identity for channels, files, sockets, wait groups, databases, or plugins.
- Splitting the VM monolith as part of this feature.

## 1. Copy and Sharing Semantics

### 1.1 Assignment

Assignment behavior remains unchanged:

- `int`, `float`, `bool`, `string`, `bytes`, and `null` are copied as scalar values;
- assigning an array, map, or struct shares the same heap-backed object;
- assigning a function, channel, wait group, native resource, or plugin value shares its identity;
- assigning a `ref T` copies the reference value and therefore preserves its target.

```noxy
let first: int[] = [1, 2]
let alias: int[] = first
alias[0] = 99
print(first[0]) // 99
```

### 1.2 Ordinary function parameters

A user-defined parameter whose declared type is not `ref T` receives a shallow copy of arrays, maps, and structs. Immediate top-level mutation stays local to the callee. Nested composite identities remain shared.

```noxy
func change(values: int[]) -> void
    values[0] = 99
    append(ref values, 3)
end

let original: int[] = [1, 2]
change(original)
print(original) // [1, 2]
```

Nested behavior remains intentionally shallow:

```noxy
func change_nested(matrix: int[][]) -> void
    matrix[0][0] = 99
end

let row: int[] = [1, 2]
let matrix: int[][] = [row]
change_nested(matrix)
print(matrix[0][0]) // 99
```

This is a call-boundary rule, not a claim of deep value semantics.

### 1.3 Reference parameters

A `ref T` parameter receives a reference to an addressable storage location and does not perform a shallow copy.

```noxy
func replace(values: ref int[]) -> void
    *values = [10, 20]
end

let original: int[] = [1, 2]
replace(ref original)
print(original) // [10, 20]
```

### 1.4 Returns

Returning a composite does not add a second implicit copy. The returned value is the container produced or held by the callee. A non-reference parameter has already been shallow-copied on entry.

## 2. Reference Semantics

### 2.1 Reference creation is explicit

`ref expression` is the only operation that creates a new reference. The expression must be an addressable identifier, struct field, array index, or map index.

```noxy
let value: int = 41
let pointer: ref int = ref value
```

References to literals, function results, and other temporaries are compile-time errors.

### 2.2 Exact calls never take an address contextually

When an exact function signature expects `ref T`, a value of type `T` is insufficient. The caller must construct the reference explicitly:

```noxy
func increment(value: ref int) -> void
    *value = value + 1
end

let answer: int = 41
increment(ref answer) // valid
increment(answer)     // compile-time error
increment(41)         // compile-time error
```

An expression whose type is already `ref T` is passed directly:

```noxy
let pointer: ref int = ref answer
increment(pointer)
```

A `ref T` struct field or indexed slot is also already a reference and is passed directly. The compiler must not create `ref ref T` implicitly.

### 2.3 Reading, updating, and rebinding

The existing three operations are retained and made normative:

- using `r: ref T` in a value expression reads the referenced `T` through automatic dereference;
- `*r = value` updates the referenced storage location;
- `r = ref other` rebinds the reference variable itself.

```noxy
let first: int = 10
let second: int = 20
let pointer: ref int = ref first

print(pointer)      // 10: automatic dereference for reading
*pointer = 11      // first becomes 11
pointer = ref second
*pointer = 21      // second becomes 21
```

`pointer = 21` is always an error. The diagnostic suggests `*pointer = 21`.

### 2.4 Null references

`null` remains compatible with `ref T`. It represents the absence of a target and may be compared or rebound. Dereferencing a null reference is a runtime error.

Passing a reference field whose current value is `null` still passes the field's addressable slot, allowing a callee to fill it. Passing the literal `null` passes no slot and therefore cannot be written through.

## 3. Dynamic Calls

Bare `func` remains a dynamic callable type. The compiler cannot infer its runtime parameter modes, so it never takes an address contextually.

```noxy
let dynamic: func = increment
dynamic(ref answer) // reference survives the dynamic boundary
dynamic(answer)     // runtime arity/type-mode error when target expects ref int
```

At runtime, every script function call validates its declared parameter modes before the frame starts:

- a `ref` parameter accepts `VAL_REF` or `null`;
- a non-reference parameter receives the existing shallow-copy treatment;
- a plain value passed to a `ref` parameter is rejected;
- a reference passed to an ordinary parameter is dereferenced by compiled exact calls; dynamic calls retain the explicit reference and must reject an incompatible runtime mode rather than guess.

Runtime errors identify the function, argument position, expected mode, and actual value.

## 4. Public Native APIs

### 4.1 Raw native ABI versus public signature

The Go `NativeFunc` ABI remains an internal mechanism. Publicly callable natives receive signature metadata describing arity, value types, return type, variadic behavior, and reference parameters.

Stdlib modules must expose typed `.nx` wrappers over internal native primitives. Internal primitives are dynamic implementation details and are not part of the documented public API; the wrapper is the public language contract.

Plugins without signature metadata remain explicit dynamic boundaries.

### 4.2 Native parameter behavior

For public native calls:

- an ordinary composite parameter behaves as if it received the same shallow copy as a Noxy function;
- a `ref T` parameter requires an explicit reference and receives the original storage slot;
- identity-bearing runtime values such as channels, wait groups, open files, sockets, statements, and database handles may change external resource state without becoming `ref` parameters; this does not rebind or mutate the caller's Noxy container;
- an implementation may elide an unobservable shallow copy for a proven read-only native, but this optimization must not change program behavior.

### 4.3 Mutating collection builtins

The builtins that mutate Noxy containers receive reference parameters:

```noxy
append(ref values, item)
let last: int = pop(ref values)
delete(ref mapping, key)
json_loads(json, ref target)
```

Their public contracts are:

```text
append(ref T[], T) -> void
pop(ref T[]) -> T
delete(ref map[K, V], K) -> void
json_loads(string, ref T) -> bool
```

The compiler continues to type-check these generic builtins specially because Noxy does not yet have user-facing generics. The first argument must be an explicit reference or an existing `ref` value.

Read-only builtins keep ordinary arguments:

```noxy
length(values)
contains(values, item)
keys(mapping)
has_key(mapping, key)
json_dumps(value)
```

### 4.4 Native errors

The Go `NativeFunc` ABI remains unchanged in this feature. A public native descriptor validates arity, parameter modes, and declared value types before invoking the Go closure. Validation failures become structured VM runtime errors and never invoke the closure.

Domain APIs may continue returning Noxy `Result` values when failure is part of their normal contract. Replacing every legacy native's internal `null` error convention is outside this feature; only descriptor-level contract failures are standardized here.

## 5. Static and Dynamic Boundaries

The specification will describe Noxy as a statically typed language with explicit dynamic boundaries, rather than claiming that every type error is detected before execution.

Normative rules:

- exact `func(T...) -> R` calls check arity, argument types, reference modes, and result types at compile time;
- widening an exact function to bare `func` is allowed;
- narrowing bare `func` to an exact signature remains forbidden;
- assigning any concrete value to `any` is allowed;
- narrowing `any` to a concrete or exact callable type remains forbidden by this feature;
- calls through bare `func`, untyped plugin exports, and untyped internal native exports validate at runtime;
- neither static nor dynamic boundaries manufacture references from plain values.

The phrase "immutable types" will be replaced with "type-stable variables": a variable's declared type does not change, although its value may be mutable.

## 6. Diagnostics

Required compile-time diagnostics include:

```text
argument 1 to 'increment': expected ref int, got int
hint: use 'ref answer'
```

```text
cannot take a reference to temporary value of type int
hint: store the value in a variable before using 'ref'
```

```text
cannot assign int to ref int
hint: use '*pointer = ...' to update the referenced value
```

Required runtime diagnostics for dynamic boundaries include:

```text
function 'dynamic' argument 1: expected ref int, got int
```

## 7. Migration

This feature requires a semver-major release.

Mechanical migrations:

```diff
- increment(answer)
+ increment(ref answer)

- append(values, item)
+ append(ref values, item)

- pop(values)
+ pop(ref values)

- delete(mapping, key)
+ delete(ref mapping, key)

- json_loads(json, target)
+ json_loads(json, ref target)
```

Calls that already pass a `ref T` value remain unchanged:

```noxy
let pointer: ref int = ref answer
increment(pointer)
```

The stdlib, examples, feature fixtures, error fixtures, concurrent runner, and documentation must migrate in the same branch. No compatibility flag or implicit fallback will remain in Noxy 2.0.

## 8. Implementation Boundaries

### Compiler

- Exact-call compilation accepts a plain l-value for `ref T` only when wrapped in an explicit `ref` prefix.
- Existing `ref T` expressions and reference-valued fields pass directly.
- Generic mutating builtins receive dedicated compile-time validation.
- Diagnostics distinguish missing `ref`, non-addressable values, and type mismatch.

### Runtime value model

- Function parameter metadata continues to record `IsRef`.
- Dynamic script calls validate `IsRef` against the actual `Value` mode.
- Public native descriptors add parameter-mode and signature metadata without importing AST types into the value package.
- Native reference arguments are resolved through the existing `ObjRef` mechanisms.

### VM

- User-defined non-reference parameters keep `copyValue` shallow copying.
- Native public-call preparation applies the declared parameter modes.
- Runtime mode mismatches return VM errors before invoking script or native code.
- No new ownership model, collector, or copy opcode is introduced.

## 9. Verification

### Compiler tests

- explicit local/global/property/index references compile;
- an existing `ref T` value compiles without another `ref`;
- implicit local/global/property/index address-taking fails with the expected hint;
- temporaries and literals fail;
- mutating builtin calls require references and validate element/key/value types;
- exact calls preserve return types and existing null rules.

### VM tests

- explicit reference calls update the intended local, global, property, and index;
- dynamic calls accept explicit references and reject plain values for reference parameters;
- update and rebind remain distinct;
- null reference dereference returns an error;
- ordinary parameters retain shallow-copy behavior, including nested sharing;
- public native value and reference parameters match user-function behavior;
- mutating collection builtins modify the intended caller container.

### Integration tests

- migrate and run all Noxy examples;
- add positive and negative reference-conformance fixtures;
- run `go test ./...`;
- run `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx`;
- run `go build ./...` and `go vet ./...` before completion.

## 10. Acceptance Criteria

1. No exact or dynamic call implicitly turns a plain value into a reference.
2. All newly created references are visible as `ref` in source.
3. Reading, updating, and rebinding references follow one rule everywhere.
4. Ordinary user-function shallow-copy behavior is unchanged.
5. Public native APIs declare and enforce the same parameter modes.
6. Mutating collection builtins require explicit reference arguments.
7. Static and dynamic boundaries are accurately documented.
8. The full Go and Noxy integration suites pass after migration.
