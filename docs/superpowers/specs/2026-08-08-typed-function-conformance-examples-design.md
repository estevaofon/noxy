# Typed Function Conformance Examples

## Goal

Provide executable internal examples that demonstrate the valid typed-function behavior and prove that invalid programs fail with the intended compile-time diagnostics.

## Scope

The change adds one successful example at `noxy_examples/typed_function_conformance.nx`. It exercises an exact higher-order function, a typed closure return, a reference parameter, an exact return type, and widening an exact function to bare `func`. It must exit successfully and remain eligible for the existing concurrent example runner.

Invalid cases live under `noxy_examples/type_errors/`, which the current top-level example runner does not enumerate. Each file contains exactly one error so the first compiler diagnostic cannot hide another case:

- incorrect arity;
- incompatible argument type;
- incompatible return type;
- assignment between different exact function signatures;
- narrowing bare `func` to an exact signature;
- passing a non-addressable expression to a `ref` parameter.

These are internal conformance fixtures. No README or public language documentation is added for them.

## Automated Verification

A table-driven Go test reads every invalid fixture, parses and compiles it, and checks a stable diagnostic fragment specific to that case. The test also compiles the valid example. Runtime behavior of the valid example remains covered by `run_all_tests_concurrent.nx` using a newly built Noxy binary.

The test must fail if an invalid fixture starts compiling, produces the wrong class of diagnostic, or cannot be parsed/read. This keeps the examples hermetic and prevents them from silently drifting away from compiler behavior.

## Acceptance Criteria

- The valid example compiles, runs, and asserts its expected results.
- Every invalid example exits through a compile-time error for its documented reason.
- `go test ./internal/compiler` passes with the fixture table.
- `go test ./...` passes.
- A freshly built binary completes `run_all_tests_concurrent.nx` with all examples passing.
- The user's untracked `noxy_examples/oops.nx` remains untouched.
