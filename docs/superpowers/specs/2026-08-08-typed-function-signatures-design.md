# Typed Function Signatures Design

## Status

Approved for implementation on branch `feat/typed-function-signatures`.

## Context

Noxy function declarations already carry parameter and return annotations in the AST, and `ast.FunctionType` already models parameter and return types. The compiler currently discards part of that information: named functions and function literals are registered with an `any` return, normal calls always produce `any`, argument types and arity are not checked during compilation, and return statements are not checked against the declared return type.

Consequently, a function declared as returning `int` can return `string`, and the result can be assigned to an `int` variable without a compilation error.

## Goals

- Preserve the exact signature of named functions and function literals.
- Validate user-defined function arity and argument types at compile time.
- Validate every explicit return against the declared return type.
- Reject non-`void` functions that can finish without returning a value.
- Support forward calls and recursion without compiling bodies twice.
- Support exact function types in variables, parameters, fields, arrays, maps, and returns.
- Keep compilation linear and add a benchmark that detects material regressions.
- Preserve runtime arity checks as defensive validation for dynamic boundaries.

## Non-goals

- Moving the type model from `internal/ast` into a dedicated package.
- Fully typing native functions, plugins, or module export maps.
- Modularizing `internal/vm/vm.go`.
- Generics, overloads, union types, optional parameters, or function subtyping.
- Removing `any` from the language.
- Making the integration runner hermetic; that is a separate project.

## Language Surface

Named functions retain their current syntax:

```noxy
func add(a: int, b: int) -> int
    return a + b
end
```

An omitted return annotation means `void`:

```noxy
func log(message: string)
    print(message)
end
```

Exact function types use the following syntax:

```noxy
func(int, int) -> int
func(string) -> void
func(ref int) -> void
```

The type parser must recognize the existing `TYPE_VOID` token explicitly. While adding function-type parsing, an unexpected token in a type position must produce a parser diagnostic instead of falling back to `int`.

They can be used wherever another type is accepted:

```noxy
let operation: func(int, int) -> int = add

func apply(operation: func(int, int) -> int, a: int, b: int) -> int
    return operation(a, b)
end
```

The existing bare `func` type remains available only as an inferable annotation. When a declaration has a function initializer, the stored variable type becomes the initializer's exact signature:

```noxy
let operation: func = add
// Stored type: func(int, int) -> int
```

A bare `func` without an initializer, or in a parameter, field, collection element, or return annotation, is rejected with a diagnostic requesting an exact signature. This prevents a silent dynamic call boundary.

Function type compatibility is invariant in this version: parameter counts, parameter types, `ref` modifiers, and return types must match exactly. `any` inside an explicitly written signature remains explicit and follows normal `any` compatibility rules.

## Compiler Architecture

### Signature predeclaration

When compiling an `ast.Program`, the compiler first scans only top-level declarations. For every named function it creates an exact `ast.FunctionType` from the declared parameters and return type and stores it in the existing globals type table.

The predeclaration pass:

- visits declarations only;
- does not compile function bodies;
- does not emit bytecode;
- detects duplicate top-level function names;
- makes forward calls and mutual recursion resolvable.

The normal compiler pass then visits the AST once and emits bytecode as it does today. Function compilation must not replace an already predeclared exact signature with `any`.

### Function literals

Compiling a function literal returns an exact `ast.FunctionType`. A missing return annotation means `void`. When a `let` declaration uses bare `func`, its local or global type is replaced by the exact initializer type before registration.

### Calls

For a callee whose type is an exact `ast.FunctionType`, `CallExpression` performs these checks before emitting `OP_CALL`:

1. The argument count equals the parameter count.
2. Each non-reference argument is compatible with the corresponding parameter.
3. Existing automatic dereference behavior is applied only when the parameter expects a value.
4. A `ref` parameter receives an addressable identifier, member, or index with a compatible element type, or `null` where references currently permit it.
5. The expression type of the call is the signature's declared return type.

Calls through an explicitly dynamic or currently untyped boundary (`any`, native functions, plugins, or untyped module exports) retain the existing runtime behavior in this branch. They do not receive a false exact type. Fully typing those boundaries is separate follow-up work.

### Returns

`ReturnStmt` compares the produced type with the current function return type after applying the existing reference auto-dereference rule.

- A non-`void` function must use `return <expression>`.
- A `void` function may use `return` but may not return a non-void expression.
- A returned value must be compatible with the declared return type.
- The top-level script retains its implicit null return.
- Only function bodies declared `void` receive an implicit `OP_NULL; OP_RETURN`.

### Definite-return analysis

A lightweight structural analysis determines whether a statement guarantees return:

- `return` guarantees return.
- A block guarantees return if execution reaches a statement that guarantees return.
- `if` guarantees return only when it has an `else` and every `if`/`elif`/`else` branch guarantees return.
- A loop does not guarantee return, even when its condition is a literal `true`.
- `when` guarantees return only when it has a fallback/default path and every selectable branch guarantees return; if the current AST cannot express a default path, it never guarantees return in this version.
- Other statements do not guarantee return.

After compiling a non-`void` function body, compilation fails if the body does not guarantee a return. Unreachable-code diagnostics are outside this scope.

## Type Compatibility

The current compatibility behavior that accepts an unknown (`nil`) type remains for unresolved dynamic boundaries. For exact user-defined function signatures:

- parameter and return types are compared structurally;
- an expected `any` accepts any actual value;
- an actual `any` does not satisfy a concrete expected type inside a statically known user-function call or return;
- bare `func` is not treated as equivalent to every exact function type except during initializer inference;
- arrays, maps, channels, references, and exact function types recurse through the same compatibility function.

This targeted strictness prevents the known unsound user-function cases without forcing native and module metadata into the same branch.

## Diagnostics

Diagnostics include source line, function name when available, argument position, expected type, and actual type. Examples:

```text
[line 8] function 'add' expects 2 arguments, got 1
[line 8] argument 1 to 'add': expected int, got string
[line 3] return type mismatch in 'bad': expected int, got string
[line 1] function 'classify' may finish without returning string
```

Compiler errors must not emit partial success output or defer these failures to the VM.

## Performance

The parser runs once. The added predeclaration scan is linear in the number of top-level statements and reads only declaration metadata. Signature lookups use the existing map-backed globals table. Bodies and expressions are compiled once.

A compiler benchmark will compile a generated program containing many typed functions and calls. The initial performance budget is no more than 10% regression relative to the same benchmark before strict call checking. The benchmark result is observational rather than a platform-independent pass/fail assertion; unexpected regressions require profiling before completion.

## Testing

Parser tests cover exact function types, nested composite function types, bare `func`, and malformed signatures.
They also cover `void` and verify that unknown tokens in type positions are rejected rather than interpreted as `int`.

Compiler tests cover:

- correct named calls;
- forward calls, recursion, and mutual recursion;
- too few and too many arguments;
- incorrect argument types;
- incorrect return types;
- value/ref argument behavior;
- missing returns and complete conditional returns;
- exact function assignment and incompatibility;
- anonymous and higher-order functions;
- bare `func` inference and invalid non-inferable uses;
- explicit dynamic boundaries retaining their documented behavior.

VM tests confirm that valid typed calls, closures, higher-order functions, and references continue to execute correctly. Existing examples remain integration coverage, but the feature's correctness must be established by Go tests that compile source strings using the current packages.

Before completion, run:

```text
go test ./internal/...
go test ./...
go vet ./...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

The integration runner's use of a pre-existing executable will be reported explicitly and will not substitute for the Go tests or direct execution with the freshly built source.

## Rollout

This branch implements typed user-defined functions as one focused change. Native/module signature metadata, VM file organization, and a hermetic integration runner remain separate follow-up branches so each can be reviewed and benchmarked independently.
