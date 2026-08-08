# Typed Function Conformance Examples Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add one successful typed-function example and six isolated compile-error fixtures that continuously verify Noxy's exact function contracts.

**Architecture:** The successful `.nx` program remains at the top level of `noxy_examples/` so the existing concurrent runner executes it. Invalid programs live one directory lower under `noxy_examples/type_errors/`; a table-driven compiler test reads them directly and asserts one stable diagnostic fragment per file.

**Tech Stack:** Noxy source files, Go 1.24 `testing`, existing lexer/parser/compiler packages, existing Noxy concurrent example runner.

## Global Constraints

- These fixtures are internal tests; do not add or modify a README or public language documentation.
- Each invalid file contains exactly one compile-time violation.
- Build a new `noxy.exe` before invoking `run_all_tests_concurrent.nx`.
- Do not modify or commit the user's untracked `noxy_examples/oops.nx`.

---

### Task 1: Executable valid conformance example

**Files:**
- Create: `internal/compiler/function_conformance_examples_test.go`
- Create: `noxy_examples/typed_function_conformance.nx`

**Interfaces:**
- Consumes: `compileFunctionSource(t, input)` from `internal/compiler/function_types_test.go`.
- Produces: a successful Noxy program that the compiler test and concurrent example runner both consume.

- [ ] **Step 1: Write the failing valid-fixture test**

Create `internal/compiler/function_conformance_examples_test.go`:

```go
package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

func readConformanceFixture(t *testing.T, parts ...string) string {
	t.Helper()
	pathParts := append([]string{"..", "..", "noxy_examples"}, parts...)
	content, err := os.ReadFile(filepath.Join(pathParts...))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestTypedFunctionValidConformanceExampleCompiles(t *testing.T) {
	input := readConformanceFixture(t, "typed_function_conformance.nx")
	if _, err := compileFunctionSource(t, input); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run the focused test and verify the missing fixture fails**

Run: `go test ./internal/compiler -run TestTypedFunctionValidConformanceExampleCompiles -count=1`

Expected: FAIL because `noxy_examples/typed_function_conformance.nx` does not exist.

- [ ] **Step 3: Add the successful Noxy example**

Create `noxy_examples/typed_function_conformance.nx`:

```noxy
func add(a: int, b: int) -> int
    return a + b
end

func apply(operation: func(int, int) -> int, a: int, b: int) -> int
    return operation(a, b)
end

func make_multiplier(factor: int) -> func(int) -> int
    return func(value: int) -> int
        return value * factor
    end
end

func increment(value: ref int) -> void
    *value = value + 1
end

let operation: func(int, int) -> int = add
assert(apply(operation, 20, 22) == 42, "exact higher-order function must return 42")

let double: func(int) -> int = make_multiplier(2)
assert(double(21) == 42, "typed closure must preserve its signature")

let answer: int = 41
increment(answer)
assert(answer == 42, "ref parameter must update the caller")

let dynamic_operation: func = operation
assert(dynamic_operation(20, 22) == 42, "exact function must widen to bare func")

print("typed function conformance: PASS")
```

- [ ] **Step 4: Run the compiler test and the example**

Run:

```bash
go test ./internal/compiler -run TestTypedFunctionValidConformanceExampleCompiles -count=1
go run cmd/noxy/main.go noxy_examples/typed_function_conformance.nx
```

Expected: the Go test passes and the program prints `typed function conformance: PASS` with exit code 0.

- [ ] **Step 5: Commit the valid fixture**

```bash
git add internal/compiler/function_conformance_examples_test.go noxy_examples/typed_function_conformance.nx
git commit -m "test: add valid typed function conformance example"
```

---

### Task 2: Isolated invalid conformance fixtures

**Files:**
- Modify: `internal/compiler/function_conformance_examples_test.go`
- Create: `noxy_examples/type_errors/typed_function_wrong_arity.nx`
- Create: `noxy_examples/type_errors/typed_function_wrong_argument.nx`
- Create: `noxy_examples/type_errors/typed_function_wrong_return.nx`
- Create: `noxy_examples/type_errors/typed_function_incompatible_assignment.nx`
- Create: `noxy_examples/type_errors/typed_function_dynamic_narrowing.nx`
- Create: `noxy_examples/type_errors/typed_function_invalid_ref_argument.nx`

**Interfaces:**
- Consumes: `readConformanceFixture` and `compileFunctionSource` from Task 1.
- Produces: six stable compile-error fixtures paired with exact diagnostic fragments.

- [ ] **Step 1: Write the failing table-driven error test**

Add `strings` to the imports and append:

```go
func TestTypedFunctionInvalidConformanceExamplesFail(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{"wrong arity", "typed_function_wrong_arity.nx", "expects 2 arguments, got 1"},
		{"wrong argument", "typed_function_wrong_argument.nx", "argument 1 to 'add': expected int, got string"},
		{"wrong return", "typed_function_wrong_return.nx", "return type mismatch in 'invalid': expected int, got string"},
		{"incompatible assignment", "typed_function_incompatible_assignment.nx", "expected func(int) -> int, got func(string) -> int"},
		{"dynamic narrowing", "typed_function_dynamic_narrowing.nx", "expected func(int) -> int, got func"},
		{"invalid ref argument", "typed_function_invalid_ref_argument.nx", "reference argument must be a variable, property, index, or null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := readConformanceFixture(t, "type_errors", tt.file)
			_, err := compileFunctionSource(t, input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want diagnostic containing %q", err, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the focused test and verify missing fixtures fail**

Run: `go test ./internal/compiler -run TestTypedFunctionInvalidConformanceExamplesFail -count=1`

Expected: FAIL because the six files under `noxy_examples/type_errors/` do not exist.

- [ ] **Step 3: Create the wrong-arity and wrong-argument fixtures**

Create `noxy_examples/type_errors/typed_function_wrong_arity.nx`:

```noxy
func add(a: int, b: int) -> int
    return a + b
end

add(1)
```

Create `noxy_examples/type_errors/typed_function_wrong_argument.nx`:

```noxy
func add(a: int, b: int) -> int
    return a + b
end

add("one", 2)
```

- [ ] **Step 4: Create the wrong-return and incompatible-assignment fixtures**

Create `noxy_examples/type_errors/typed_function_wrong_return.nx`:

```noxy
func invalid() -> int
    return "not an integer"
end
```

Create `noxy_examples/type_errors/typed_function_incompatible_assignment.nx`:

```noxy
func text_length(value: string) -> int
    return strlen(value)
end

let operation: func(int) -> int = text_length
```

- [ ] **Step 5: Create the dynamic-narrowing and invalid-ref fixtures**

Create `noxy_examples/type_errors/typed_function_dynamic_narrowing.nx`:

```noxy
func identity(value: int) -> int
    return value
end

let dynamic_operation: func = identity
let exact_operation: func(int) -> int = dynamic_operation
```

Create `noxy_examples/type_errors/typed_function_invalid_ref_argument.nx`:

```noxy
func increment(value: ref int) -> void
    *value = value + 1
end

increment(41)
```

- [ ] **Step 6: Run the focused error tests and direct compiler checks**

Run:

```bash
go test ./internal/compiler -run TestTypedFunctionInvalidConformanceExamplesFail -count=1
go run cmd/noxy/main.go noxy_examples/type_errors/typed_function_wrong_arity.nx
go run cmd/noxy/main.go noxy_examples/type_errors/typed_function_wrong_argument.nx
```

Expected: the Go test passes. Each direct Noxy command exits nonzero with its matching compile-time diagnostic.

- [ ] **Step 7: Commit the invalid fixtures**

```bash
git add internal/compiler/function_conformance_examples_test.go noxy_examples/type_errors
git commit -m "test: add typed function compile-error fixtures"
```

---

### Task 3: Hermetic full verification

**Files:**
- Verify only; no source changes expected.

**Interfaces:**
- Consumes: all fixtures and tests from Tasks 1 and 2.
- Produces: evidence that the fixtures coexist with the complete Noxy suite.

- [ ] **Step 1: Format and run the complete Go suite**

Run:

```bash
gofmt -w internal/compiler/function_conformance_examples_test.go
go test ./...
go vet ./...
```

Expected: all commands exit 0.

- [ ] **Step 2: Build a fresh binary and run all successful examples**

Run:

```bash
go build -o noxy.exe ./cmd/noxy
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: build exits 0; the runner includes `typed_function_conformance.nx`, ignores the nested `type_errors/` directory, and reports zero failures.

- [ ] **Step 3: Confirm repository scope**

Run:

```bash
git diff --check
git status --short
```

Expected: no whitespace errors; only intentional task changes are tracked, and `noxy_examples/oops.nx` remains untracked and untouched.

- [ ] **Step 4: Push the completed commits to the existing feature branch**

```bash
git push origin feat/typed-function-signatures
```

Expected: the existing PR receives both conformance-example commits without removing its worktree or changing its base branch.
