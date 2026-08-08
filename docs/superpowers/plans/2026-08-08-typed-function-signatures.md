# Typed Function Signatures Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add exact `func(T...) -> R` types and compile-time checking for statically known user functions while preserving bare `func` as Noxy's existing dynamic callable type.

**Architecture:** Parse exact callable types into the existing `ast.FunctionType`, predeclare top-level function signatures before emitting bytecode, and validate exact calls and returns during the normal compiler pass. Keep bare `func`, natives, plugins, and untyped module exports as explicit dynamic boundaries so existing programs remain viable.

**Tech Stack:** Go 1.24, Noxy lexer/parser/AST/compiler/bytecode VM, Go `testing` and benchmark tooling.

## Global Constraints

- Parse the source and build the AST once; function bodies and expressions must be compiled once.
- Keep compilation linear; the signature pass reads declaration metadata and emits no bytecode.
- Bare `func` remains a dynamic callable type in variables, parameters, fields, collections, and returns.
- Exact function types are invariant: arity, parameter types, `ref` modifiers, and return type must match.
- Exact function values may widen to bare `func`; bare `func` must not narrow implicitly to an exact signature.
- Omitted function return annotations mean `void`.
- Calls through bare `func`, `any`, natives, plugins, and untyped module exports retain runtime validation.
- Do not add generics, overloads, union types, optional parameters, function subtyping, or a new type package.
- Preserve the current runtime arity checks as defensive validation.
- Required completion checks: `go test ./internal/...`, `go test ./...`, `go vet ./...`, and the integration runner using a freshly built `noxy.exe`.

---

## File Structure

- Create `internal/compiler/function_types.go`: constructors, predicates, strict compatibility, signature predeclaration, and definite-return analysis for callable types.
- Create `internal/compiler/function_types_test.go`: focused compiler tests and shared source-compilation helpers.
- Create `internal/compiler/function_types_benchmark_test.go`: stable compilation benchmark usable before and after the feature.
- Create `internal/parser/function_type_test.go`: exact callable type grammar and malformed-type tests.
- Create `internal/vm/function_types_test.go`: execution tests for exact and dynamic higher-order functions.
- Modify `internal/parser/parser.go`: parse `void`, bare `func`, and exact `func(T...) -> R`; reject invalid type tokens.
- Modify `internal/compiler/compiler.go`: predeclare signatures, preserve exact function types, validate calls/returns, and emit implicit return only for `void`.
- Modify `internal/ast/ast.go`: render arrays of exact functions with disambiguating parentheses; do not create a second function-type representation.
- Modify `docs/NOXY_LANGUAGE_SPEC.md`: document exact and dynamic callable contracts and migration examples.

---

### Task 1: Establish the compilation benchmark and test helpers

**Files:**
- Create: `internal/compiler/function_types_benchmark_test.go`
- Create: `internal/compiler/function_types_test.go`

**Interfaces:**
- Produces: `compileFunctionSource(t *testing.T, input string) (*Compiler, error)` for later compiler tests.
- Produces: `BenchmarkCompileTypedFunctionCalls` as the before/after performance probe.

- [ ] **Step 1: Add the shared compilation helper**

```go
package compiler

import (
    "noxy-vm/internal/lexer"
    "noxy-vm/internal/parser"
    "testing"
)

func compileFunctionSource(t *testing.T, input string) (*Compiler, error) {
    t.Helper()
    l := lexer.New(input)
    p := parser.New(l)
    program := p.ParseProgram()
    if len(p.Errors()) > 0 {
        t.Fatalf("parser errors: %v", p.Errors())
    }
    c := New()
    _, _, err := c.Compile(program)
    return c, err
}
```

- [ ] **Step 2: Add a benchmark accepted by the current compiler**

Generate named functions with current syntax so the same benchmark runs before exact type parsing exists:

```go
package compiler

import (
    "fmt"
    "noxy-vm/internal/lexer"
    "noxy-vm/internal/parser"
    "strings"
    "testing"
)

func BenchmarkCompileTypedFunctionCalls(b *testing.B) {
    var source strings.Builder
    for i := 0; i < 200; i++ {
        fmt.Fprintf(&source, "func f%d(a: int, b: int) -> int\nreturn a + b\nend\n", i)
    }
    source.WriteString("func main() -> int\nlet total: int = 0\n")
    for i := 0; i < 200; i++ {
        fmt.Fprintf(&source, "total = total + f%d(1, 2)\n", i)
    }
    source.WriteString("return total\nend\nmain()\n")
    input := source.String()

    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        l := lexer.New(input)
        p := parser.New(l)
        program := p.ParseProgram()
        if len(p.Errors()) != 0 {
            b.Fatalf("parser errors: %v", p.Errors())
        }
        if _, _, err := New().Compile(program); err != nil {
            b.Fatal(err)
        }
    }
}
```

- [ ] **Step 3: Run the helper package and record the baseline**

Run:

```text
go test ./internal/compiler
go test -run '^$' -bench BenchmarkCompileTypedFunctionCalls -benchmem -count 5 ./internal/compiler
```

Expected: tests pass and five benchmark samples are printed. Copy the `ns/op`, `B/op`, and `allocs/op` samples into the commit message body or task notes for comparison in Task 8.

- [ ] **Step 4: Commit the benchmark harness**

```text
git add internal/compiler/function_types_test.go internal/compiler/function_types_benchmark_test.go
git commit -m "test: establish typed function compiler benchmark"
```

---

### Task 2: Parse exact function types

**Files:**
- Create: `internal/parser/function_type_test.go`
- Modify: `internal/ast/ast.go:43-51`
- Modify: `internal/parser/parser.go:536-657`

**Interfaces:**
- Consumes: existing `ast.FunctionType{Params []NoxyType, Return NoxyType}`.
- Produces: `parseFunctionType() ast.NoxyType` called when `parseAtomicType` sees `FUNC` followed by `LPAREN`.
- Produces: bare `func` as `*ast.PrimitiveType{Name: "func"}`.

- [ ] **Step 1: Write failing parser tests**

Add table tests that parse a `let` declaration and inspect `LetStmt.Type`:

```go
package parser

import (
    "noxy-vm/internal/ast"
    "noxy-vm/internal/lexer"
    "testing"
)

func TestParseFunctionTypes(t *testing.T) {
    tests := []struct {
        input string
        want  string
    }{
        {"let f: func(int, int) -> int", "func(int, int) -> int"},
        {"let f: func(ref int) -> void", "func(ref int) -> void"},
        {"let f: (func(int) -> int)[]", "(func(int) -> int)[]"},
        {"let f: map[string, func(string) -> bool]", "map[string, func(string) -> bool]"},
        {"let f: chan func(int) -> int", "chan func(int) -> int"},
        {"let f: ref func(int) -> int", "ref func(int) -> int"},
        {"let f: func() -> func(int) -> int", "func() -> func(int) -> int"},
        {"let f: func", "func"},
    }
    for _, tt := range tests {
        t.Run(tt.want, func(t *testing.T) {
            l := lexer.New(tt.input)
            p := New(l)
            program := p.ParseProgram()
            checkParserErrors(t, p)
            stmt := program.Statements[0].(*ast.LetStmt)
            if got := stmt.Type.String(); got != tt.want {
                t.Fatalf("type=%q, want %q", got, tt.want)
            }
        })
    }
}

func TestRejectMalformedFunctionTypes(t *testing.T) {
    inputs := []string{
        "let f: func(int)",
        "let f: func(unknown-token) -> int",
        "let f: func(int,) -> int",
    }
    for _, input := range inputs {
        l := lexer.New(input)
        p := New(l)
        p.ParseProgram()
        if len(p.Errors()) == 0 {
            t.Fatalf("expected parser error for %q", input)
        }
    }
}
```

Update `ArrayType.String` so the assertion remains unambiguous: a function element is rendered as `"(func(int) -> int)[]"`, while `"func(int) -> int[]"` continues to mean a function returning an integer array.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/parser -run 'TestParseFunctionTypes|TestRejectMalformedFunctionTypes' -v`

Expected: FAIL because `func(...) -> ...` and `void` are not parsed as types.

- [ ] **Step 3: Implement `void`, exact callable parsing, and invalid-type diagnostics**

In `parseAtomicType`, replace the current `FUNC` and fallback handling with:

```go
case token.TYPE_VOID:
    t = &ast.PrimitiveType{Name: "void"}
case token.FUNC:
    if p.peekTokenIs(token.LPAREN) {
        return p.parseFunctionType()
    }
    t = &ast.PrimitiveType{Name: "func"}
default:
    p.errors = append(p.errors, fmt.Sprintf(
        "[%d:%d] SyntaxError: expected type, found %s",
        p.curToken.Line, p.curToken.Column, p.curToken.Type.Display(),
    ))
    return nil
```

Add:

```go
func (p *Parser) parseFunctionType() ast.NoxyType {
    params := []ast.NoxyType{}
    if !p.expectPeek(token.LPAREN) {
        return nil
    }
    if !p.peekTokenIs(token.RPAREN) {
        p.nextToken()
        param := p.parseType()
        if param == nil {
            return nil
        }
        params = append(params, param)
        for p.peekTokenIs(token.COMMA) {
            p.nextToken()
            p.nextToken()
            param = p.parseType()
            if param == nil {
                return nil
            }
            params = append(params, param)
        }
    }
    if !p.expectPeek(token.RPAREN) || !p.expectPeek(token.ARROW) {
        return nil
    }
    p.nextToken()
    result := p.parseType()
    if result == nil {
        return nil
    }
    return &ast.FunctionType{Params: params, Return: result}
}
```

Update `ArrayType.String` to parenthesize a function element:

```go
element := at.ElementType.String()
if _, ok := at.ElementType.(*FunctionType); ok {
    element = "(" + element + ")"
}
return element + "[]"
```

- [ ] **Step 4: Run parser and internal tests**

Run:

```text
gofmt -w internal/ast/ast.go internal/parser/parser.go internal/parser/function_type_test.go
go test ./internal/parser ./internal/compiler
```

Expected: PASS.

- [ ] **Step 5: Commit the grammar**

```text
git add internal/ast/ast.go internal/parser/parser.go internal/parser/function_type_test.go
git commit -m "feat(parser): parse exact function types"
```

---

### Task 3: Define callable construction and compatibility rules

**Files:**
- Create: `internal/compiler/function_types.go`
- Modify: `internal/compiler/function_types_test.go`
- Modify: `internal/compiler/compiler.go:1870-1927`

**Interfaces:**
- Produces: `normalizeReturnType(ast.NoxyType) ast.NoxyType`.
- Produces: `newFunctionType([]*ast.Parameter, ast.NoxyType) *ast.FunctionType`.
- Produces: `isBareFunctionType(ast.NoxyType) bool` and `isCallableType(ast.NoxyType) bool`.
- Produces: `(*Compiler).areStrictTypesCompatible(expected, actual ast.NoxyType) bool`.

- [ ] **Step 1: Write failing compatibility tests**

Add `"noxy-vm/internal/ast"` to the existing import block in `function_types_test.go`, then add:

```go
func primitive(name string) ast.NoxyType { return &ast.PrimitiveType{Name: name} }

func TestFunctionTypeCompatibility(t *testing.T) {
    c := New()
    exact := &ast.FunctionType{Params: []ast.NoxyType{primitive("int")}, Return: primitive("int")}
    same := &ast.FunctionType{Params: []ast.NoxyType{primitive("int")}, Return: primitive("int")}
    different := &ast.FunctionType{Params: []ast.NoxyType{primitive("string")}, Return: primitive("int")}
    bare := primitive("func")

    if !c.areTypesCompatible(exact, same) {
        t.Fatal("equal exact signatures must be compatible")
    }
    if c.areTypesCompatible(exact, different) {
        t.Fatal("different exact signatures must be incompatible")
    }
    if !c.areTypesCompatible(bare, exact) {
        t.Fatal("exact function must widen to bare func")
    }
    if c.areTypesCompatible(exact, bare) {
        t.Fatal("bare func must not narrow to an exact signature")
    }
    if c.areTypesCompatible(bare, primitive("int")) {
        t.Fatal("bare func must accept only callable values")
    }
}

func TestStrictCompatibilityRejectsActualAny(t *testing.T) {
    c := New()
    if c.areStrictTypesCompatible(primitive("int"), primitive("any")) {
        t.Fatal("actual any must not satisfy concrete int in an exact contract")
    }
    if !c.areStrictTypesCompatible(primitive("any"), primitive("int")) {
        t.Fatal("expected any must accept int")
    }
}

func TestHeterogeneousCallableArrayWidensToBareFunction(t *testing.T) {
    _, err := compileFunctionSource(t, `
let callbacks: func[] = [
    func() -> int
        return 1
    end,
    func(value: int) -> int
        return value
    end
]`)
    if err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/compiler -run 'TestFunctionTypeCompatibility|TestStrictCompatibilityRejectsActualAny|TestHeterogeneousCallableArray' -v`

Expected: FAIL because bare `func` is currently treated like unrestricted `any`, and exact structural rules are incomplete.

- [ ] **Step 3: Implement callable helpers**

Create `function_types.go`:

```go
package compiler

import "noxy-vm/internal/ast"

func normalizeReturnType(t ast.NoxyType) ast.NoxyType {
    if t == nil {
        return &ast.PrimitiveType{Name: "void"}
    }
    return t
}

func newFunctionType(params []*ast.Parameter, result ast.NoxyType) *ast.FunctionType {
    types := make([]ast.NoxyType, len(params))
    for i, param := range params {
        types[i] = param.Type
    }
    return &ast.FunctionType{Params: types, Return: normalizeReturnType(result)}
}

func isBareFunctionType(t ast.NoxyType) bool {
    p, ok := t.(*ast.PrimitiveType)
    return ok && p.Name == "func"
}

func isCallableType(t ast.NoxyType) bool {
    if isBareFunctionType(t) {
        return true
    }
    _, ok := t.(*ast.FunctionType)
    return ok
}

func noxyTypeName(t ast.NoxyType) string {
    if t == nil {
        return "unknown"
    }
    return t.String()
}

func sameExactType(left, right ast.NoxyType) bool {
    if left == nil || right == nil {
        return left == nil && right == nil
    }
    switch l := left.(type) {
    case *ast.PrimitiveType:
        r, ok := right.(*ast.PrimitiveType)
        return ok && l.Name == r.Name
    case *ast.ArrayType:
        r, ok := right.(*ast.ArrayType)
        return ok && l.Size == r.Size && sameExactType(l.ElementType, r.ElementType)
    case *ast.MapType:
        r, ok := right.(*ast.MapType)
        return ok && sameExactType(l.KeyType, r.KeyType) && sameExactType(l.ValueType, r.ValueType)
    case *ast.ChanType:
        r, ok := right.(*ast.ChanType)
        return ok && sameExactType(l.ElementType, r.ElementType)
    case *ast.RefType:
        r, ok := right.(*ast.RefType)
        return ok && sameExactType(l.ElementType, r.ElementType)
    case *ast.FunctionType:
        r, ok := right.(*ast.FunctionType)
        if !ok || len(l.Params) != len(r.Params) || !sameExactType(l.Return, r.Return) {
            return false
        }
        for i := range l.Params {
            if !sameExactType(l.Params[i], r.Params[i]) {
                return false
            }
        }
        return true
    default:
        return false
    }
}

func (c *Compiler) areStrictTypesCompatible(expected, actual ast.NoxyType) bool {
    if expected == nil || actual == nil {
        return true
    }
    if isAny(expected) {
        return true
    }
    if isAny(actual) {
        return false
    }
    if isBareFunctionType(expected) {
        return isCallableType(actual)
    }
    if isBareFunctionType(actual) {
        return isBareFunctionType(expected)
    }
    if _, ok := expected.(*ast.FunctionType); ok {
        return sameExactType(expected, actual)
    }
    switch e := expected.(type) {
    case *ast.ArrayType:
        a, ok := actual.(*ast.ArrayType)
        return ok && (e.Size == 0 || e.Size == a.Size) &&
            c.areStrictTypesCompatible(e.ElementType, a.ElementType)
    case *ast.MapType:
        a, ok := actual.(*ast.MapType)
        return ok && c.areStrictTypesCompatible(e.KeyType, a.KeyType) &&
            c.areStrictTypesCompatible(e.ValueType, a.ValueType)
    case *ast.ChanType:
        a, ok := actual.(*ast.ChanType)
        return ok && c.areStrictTypesCompatible(e.ElementType, a.ElementType)
    case *ast.RefType:
        a, ok := actual.(*ast.RefType)
        return ok && c.areStrictTypesCompatible(e.ElementType, a.ElementType)
    default:
        return expected.String() == actual.String()
    }
}
```

Replace `isAny` and `areTypesCompatible` with:

```go
func isAny(t ast.NoxyType) bool {
    primitive, ok := t.(*ast.PrimitiveType)
    return ok && primitive.Name == "any"
}

func (c *Compiler) areTypesCompatible(expected, actual ast.NoxyType) bool {
    if expected == nil || actual == nil { return true }
    if isBareFunctionType(expected) { return isCallableType(actual) }
    if isBareFunctionType(actual) { return isBareFunctionType(expected) || isAny(expected) }
    if _, ok := expected.(*ast.FunctionType); ok {
        return c.areStrictTypesCompatible(expected, actual)
    }
    if expected.String() == actual.String() { return true }

    if expectedMap, ok := expected.(*ast.MapType); ok {
        actualMap, ok := actual.(*ast.MapType)
        return ok && c.areTypesCompatible(expectedMap.KeyType, actualMap.KeyType) &&
            c.areTypesCompatible(expectedMap.ValueType, actualMap.ValueType)
    }
    if expectedArray, ok := expected.(*ast.ArrayType); ok {
        actualArray, ok := actual.(*ast.ArrayType)
        return ok && (expectedArray.Size == 0 || expectedArray.Size == actualArray.Size) &&
            c.areTypesCompatible(expectedArray.ElementType, actualArray.ElementType)
    }
    if isAny(expected) || isAny(actual) {
        return true
    }
    return false
}
```

This ordering is required both at the top level and inside arrays/maps: bare `func` is callable-dynamic, not an alias for unrestricted `any`. `sameExactType` keeps two exact signatures invariant, while `areStrictTypesCompatible` still permits an exact value to widen when the declared destination itself is bare `func`.

In `ArrayLiteral` and `MapLiteral`, replace the existing unconditional promotion to `any` after an incompatible value pair with callable-aware promotion:

```go
func commonInferredType(left, right ast.NoxyType) ast.NoxyType {
    if isCallableType(left) && isCallableType(right) {
        return &ast.PrimitiveType{Name: "func"}
    }
    return &ast.PrimitiveType{Name: "any"}
}
```

Use `elemType = commonInferredType(elemType, t)` for an array mismatch and `valType = commonInferredType(valType, vt)` for a map-value mismatch. Thus differently shaped functions infer as dynamic callables rather than unrestricted values; homogeneous exact functions retain their exact element type.

- [ ] **Step 4: Run compiler and integration-adjacent tests**

Run: `gofmt -w internal/compiler/function_types.go internal/compiler/function_types_test.go internal/compiler/compiler.go && go test ./internal/compiler ./internal/vm`

Expected: PASS.

- [ ] **Step 5: Commit compatibility rules**

```text
git add internal/compiler/function_types.go internal/compiler/function_types_test.go internal/compiler/compiler.go
git commit -m "feat(compiler): define callable type compatibility"
```

---

### Task 4: Predeclare exact named function signatures

**Files:**
- Modify: `internal/compiler/function_types.go`
- Modify: `internal/compiler/function_types_test.go`
- Modify: `internal/compiler/compiler.go:85-96,1410-1448`

**Interfaces:**
- Produces: `(*Compiler).predeclareFunctions([]ast.Statement) error`.
- Named user-function globals hold exact `*ast.FunctionType` values before any body is compiled.

- [ ] **Step 1: Write failing tests for forward and mutual references**

Add `"strings"` to the existing import block in `function_types_test.go`, then add:

```go
func TestPredeclaresForwardFunctionSignature(t *testing.T) {
    c, err := compileFunctionSource(t, `
func first() -> int
    return second()
end
func second() -> int
    return 42
end`)
    if err != nil { t.Fatal(err) }
    got, ok := c.GetGlobals()["second"].(*ast.FunctionType)
    if !ok || got.Return.String() != "int" {
        t.Fatalf("second signature=%v", c.GetGlobals()["second"])
    }
}

func TestPredeclaresMutualRecursion(t *testing.T) {
    _, err := compileFunctionSource(t, `
func even(n: int) -> bool
    if n == 0 then
        return true
    else
        return odd(n - 1)
    end
end
func odd(n: int) -> bool
    if n == 0 then
        return false
    else
        return even(n - 1)
    end
end`)
    if err != nil { t.Fatal(err) }
}

func TestRejectsDuplicateTopLevelFunctionNames(t *testing.T) {
    _, err := compileFunctionSource(t, `
func same() -> int
    return 1
end
func same() -> int
    return 2
end`)
    if err == nil || !strings.Contains(err.Error(), "duplicate function 'same'") {
        t.Fatalf("error=%v", err)
    }
}

func TestFunctionLiteralKeepsExactSignature(t *testing.T) {
    _, err := compileFunctionSource(t, `
let stringify: func(int) -> string = func(v: int) -> string
    return "value"
end`)
    if err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/compiler -run 'TestPredeclares|TestRejectsDuplicate|TestFunctionLiteralKeeps' -v`

Expected: FAIL because `second`/`odd` are unknown while earlier bodies compile.

- [ ] **Step 3: Implement the declaration-only scan**

```go
func (c *Compiler) predeclareFunctions(statements []ast.Statement) error {
    seen := make(map[string]struct{})
    for _, statement := range statements {
        fn, ok := statement.(*ast.FunctionStatement)
        if !ok { continue }
        if _, duplicate := seen[fn.Name]; duplicate {
            return fmt.Errorf("[line %d] duplicate function '%s'", fn.Token.Line, fn.Name)
        }
        seen[fn.Name] = struct{}{}
        c.globals[fn.Name] = newFunctionType(fn.Parameters, fn.ReturnType)
    }
    return nil
}
```

Call it at the beginning of the `ast.Program` compiler case. In the `FunctionStatement` case, reuse `newFunctionType` and never replace the predeclared return with `any`.

Replace the manual `FunctionType` construction in both function compiler cases with these exact returns:

```go
// FunctionStatement: the Program prepass already installed the same type.
c.globals[n.Name] = newFunctionType(n.Parameters, n.ReturnType)

// FunctionLiteral: return the literal's intrinsic exact type.
return c.currentChunk, newFunctionType(n.Parameters, n.ReturnType), nil
```

- [ ] **Step 4: Run compiler tests**

Run: `gofmt -w internal/compiler/function_types.go internal/compiler/compiler.go internal/compiler/function_types_test.go && go test ./internal/compiler`

Expected: PASS.

- [ ] **Step 5: Commit predeclaration**

```text
git add internal/compiler/function_types.go internal/compiler/function_types_test.go internal/compiler/compiler.go
git commit -m "feat(compiler): predeclare function signatures"
```

---

### Task 5: Validate exact calls and propagate return types

**Files:**
- Modify: `internal/compiler/function_types.go`
- Modify: `internal/compiler/function_types_test.go`
- Modify: `internal/compiler/compiler.go:1501-1722`

**Interfaces:**
- Produces: `callableName(ast.Expression) string` for diagnostics.
- Exact calls return `funcType.Return`; bare/dynamic calls return `any`.
- Existing `ref` bytecode emission remains the source of truth for addressable argument handling.

- [ ] **Step 1: Write failing arity, argument, return-propagation, and ref tests**

Use a table that expects error substrings:

```go
func TestExactCallDiagnostics(t *testing.T) {
    tests := []struct{ name, input, want string }{
        {"few", `func add(a: int, b: int) -> int
    return a + b
end
add(1)`, "expects 2 arguments, got 1"},
        {"many", `func add(a: int) -> int
    return a
end
add(1, 2)`, "expects 1 arguments, got 2"},
        {"type", `func add(a: int) -> int
    return a
end
add("x")`, "argument 1 to 'add': expected int, got string"},
        {"any", `func add(a: int) -> int
    return a
end
let x: any = 1
add(x)`, "expected int, got any"},
        {"ref", `func set(v: ref int) -> void
    return
end
set(1)`, "reference argument must be a variable, property, index, or null"},
        {"ref type", `func set(v: ref int) -> void
    return
end
let text: string = "x"
set(text)`, "argument 1 to 'set': expected ref int, got ref string"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := compileFunctionSource(t, tt.input)
            if err == nil || !strings.Contains(err.Error(), tt.want) {
                t.Fatalf("error=%v, want %q", err, tt.want)
            }
        })
    }
}

func TestExactCallPropagatesReturnType(t *testing.T) {
    _, err := compileFunctionSource(t, `
func bad() -> string
    return "x"
end
let value: int = bad()`)
    if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
        t.Fatalf("error=%v", err)
    }
}

func TestExactReferenceCallsCompile(t *testing.T) {
    _, err := compileFunctionSource(t, `
func set(v: ref int) -> void
    return
end
let value: int = 1
let values: int[] = [1]
set(value)
set(values[0])`)
    if err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/compiler -run 'TestExactCall' -v`

Expected: FAIL because normal calls currently skip exact checks and return `any`.

- [ ] **Step 3: Add exact call checks before emitting `OP_CALL`**

Add the diagnostic helper to `function_types.go`:

```go
func callableName(expression ast.Expression) string {
    if identifier, ok := expression.(*ast.Identifier); ok {
        return identifier.Value
    }
    return expression.String()
}
```

Immediately after resolving `funcType, isExact := fnType.(*ast.FunctionType)`:

```go
if isExact && len(n.Arguments) != len(funcType.Params) {
    return nil, nil, fmt.Errorf(
        "[line %d] function '%s' expects %d arguments, got %d",
        c.currentLine, callableName(n.Function), len(funcType.Params), len(n.Arguments),
    )
}
```

While compiling each non-reference argument, update the actual type after auto-dereference and validate:

```go
_, argType, err := c.Compile(arg)
if err != nil { return nil, nil, err }
if ref, ok := argType.(*ast.RefType); ok {
    c.emitByte(byte(chunk.OP_DEREF))
    argType = ref.ElementType
}
if isExact && !c.areStrictTypesCompatible(funcType.Params[i], argType) {
    return nil, nil, fmt.Errorf(
        "[line %d] argument %d to '%s': expected %s, got %s",
        c.currentLine, i+1, callableName(n.Function),
        funcType.Params[i].String(), argType.String(),
    )
}
```

Extract the current reference-emission branches into these helpers in `compiler.go`; both explicit `ref` expressions and exact-call reference arguments use this single path, so no argument is compiled twice:

```go
func unwrapRefType(t ast.NoxyType) ast.NoxyType {
    if ref, ok := t.(*ast.RefType); ok {
        return ref.ElementType
    }
    return t
}

func (c *Compiler) memberType(owner ast.NoxyType, member string) ast.NoxyType {
    owner = unwrapRefType(owner)
    primitive, ok := owner.(*ast.PrimitiveType)
    if !ok {
        return nil
    }
    definition, ok := c.structs[primitive.Name]
    if !ok {
        return nil
    }
    for _, field := range definition.FieldsList {
        if field.Name == member {
            return field.Type
        }
    }
    return nil
}

func indexElementType(container ast.NoxyType) ast.NoxyType {
    switch typed := unwrapRefType(container).(type) {
    case *ast.ArrayType:
        return typed.ElementType
    case *ast.MapType:
        return typed.ValueType
    default:
        return nil
    }
}

func (c *Compiler) compileReferenceArgument(expression ast.Expression) (ast.NoxyType, error) {
    if prefix, ok := expression.(*ast.PrefixExpression); ok && prefix.Operator == "ref" {
        expression = prefix.Right
    }

    switch target := expression.(type) {
    case *ast.Identifier:
        if slot, declared := c.resolveLocal(target.Value); slot != -1 {
            if ref, ok := declared.(*ast.RefType); ok {
                c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(slot))
                return ref.ElementType, nil
            }
            c.emitBytes(byte(chunk.OP_REF_LOCAL), byte(slot))
            c.locals[slot].IsCaptured = true
            return declared, nil
        }
        if c.resolveUpvalue(target.Value) != -1 {
            return nil, fmt.Errorf("[line %d] captured variables cannot be passed by reference", c.currentLine)
        }
        name := c.makeConstant(value.NewString(target.Value))
        if declared, ok := c.resolveGlobalType(target.Value); ok {
            if ref, ok := declared.(*ast.RefType); ok {
                c.emitBytes(byte(chunk.OP_GET_GLOBAL), byte(name))
                return ref.ElementType, nil
            }
            c.emitBytes(byte(chunk.OP_REF_GLOBAL), byte(name))
            return declared, nil
        }
        c.emitBytes(byte(chunk.OP_REF_GLOBAL), byte(name))
        return nil, nil
    case *ast.MemberAccessExpression:
        _, owner, err := c.Compile(target.Left)
        if err != nil { return nil, err }
        element := c.memberType(owner, target.Member)
        if _, ok := owner.(*ast.RefType); ok { c.emitByte(byte(chunk.OP_DEREF)) }
        name := c.makeConstant(value.NewString(target.Member))
        c.emitBytes(byte(chunk.OP_REF_PROPERTY), byte(name))
        return element, nil
    case *ast.IndexExpression:
        _, container, err := c.Compile(target.Left)
        if err != nil { return nil, err }
        element := indexElementType(container)
        if _, ok := container.(*ast.RefType); ok { c.emitByte(byte(chunk.OP_DEREF)) }
        _, indexType, err := c.Compile(target.Index)
        if err != nil { return nil, err }
        if _, ok := indexType.(*ast.RefType); ok { c.emitByte(byte(chunk.OP_DEREF)) }
        c.emitByte(byte(chunk.OP_REF_INDEX))
        return element, nil
    case *ast.NullLiteral:
        c.emitByte(byte(chunk.OP_NULL))
        return nil, nil
    default:
        return nil, fmt.Errorf(
            "[line %d] reference argument must be a variable, property, index, or null",
            c.currentLine,
        )
    }
}
```

In the exact-call loop, use the helper and validate the element type without emitting the argument a second time:

```go
if expectedRef, ok := funcType.Params[i].(*ast.RefType); ok {
    actualElement, err := c.compileReferenceArgument(arg)
    if err != nil { return nil, nil, err }
    if !c.areStrictTypesCompatible(expectedRef.ElementType, actualElement) {
        actual := &ast.RefType{ElementType: actualElement}
        return nil, nil, fmt.Errorf(
            "[line %d] argument %d to '%s': expected %s, got %s",
            c.currentLine, i+1, callableName(n.Function), expectedRef.String(), actual.String(),
        )
    }
    continue
}
```

For non-exact/dynamic callees, keep the current reference behavior. In the `PrefixExpression` `ref` case, call `compileReferenceArgument(n.Right)` and return `&ast.RefType{ElementType: element}` so explicit references share the same type resolution.

Return after call emission with:

```go
if isExact {
    return c.currentChunk, funcType.Return, nil
}
return c.currentChunk, &ast.PrimitiveType{Name: "any"}, nil
```

- [ ] **Step 4: Run compiler and VM tests**

Run: `gofmt -w internal/compiler/function_types.go internal/compiler/function_types_test.go internal/compiler/compiler.go && go test ./internal/compiler ./internal/vm`

Expected: PASS.

- [ ] **Step 5: Commit call checking**

```text
git add internal/compiler/function_types.go internal/compiler/function_types_test.go internal/compiler/compiler.go
git commit -m "feat(compiler): check exact function calls"
```

---

### Task 6: Validate returns and require complete non-void returns

**Files:**
- Modify: `internal/compiler/function_types.go`
- Modify: `internal/compiler/function_types_test.go`
- Modify: `internal/compiler/compiler.go:29-40,1382-1408,1968-1996`

**Interfaces:**
- Add compiler field: `currentFunctionName string`.
- Produces: `blockGuaranteesReturn(*ast.BlockStatement) bool`.
- Produces: `statementGuaranteesReturn(ast.Statement) bool`.

- [ ] **Step 1: Write failing return tests**

```go
func TestReturnChecking(t *testing.T) {
    tests := []struct{ name, input, want string }{
        {"wrong type", `func bad() -> int
    return "x"
end`, "return type mismatch in 'bad': expected int, got string"},
        {"missing value", `func bad() -> int
    return
end`, "must return int"},
        {"void value", `func bad() -> void
    return 1
end`, "void function 'bad' cannot return int"},
        {"fallthrough", `func bad(v: bool) -> int
    if v then
        return 1
    end
end`, "may finish without returning int"},
        {"actual any", `func bad(v: any) -> int
    return v
end`, "expected int, got any"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := compileFunctionSource(t, tt.input)
            if err == nil || !strings.Contains(err.Error(), tt.want) {
                t.Fatalf("error=%v, want %q", err, tt.want)
            }
        })
    }
}

func TestAllConditionalBranchesReturn(t *testing.T) {
    _, err := compileFunctionSource(t, `
func classify(v: int) -> string
    if v > 0 then
        return "positive"
    else
        return "other"
    end
end`)
    if err != nil { t.Fatal(err) }
}

func TestWhenGuaranteesReturnOnlyWithReturningDefault(t *testing.T) {
    returning := func() *ast.BlockStatement {
        return &ast.BlockStatement{Statements: []ast.Statement{&ast.ReturnStmt{}}}
    }
    complete := &ast.WhenStatement{Cases: []*ast.CaseClause{
        {Body: returning()},
        {IsDefault: true, Body: returning()},
    }}
    if !statementGuaranteesReturn(complete) {
        t.Fatal("when with a default and returning bodies must guarantee return")
    }
    withoutDefault := &ast.WhenStatement{Cases: []*ast.CaseClause{{Body: returning()}}}
    if statementGuaranteesReturn(withoutDefault) {
        t.Fatal("when without default must not guarantee return")
    }
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/compiler -run 'TestReturnChecking|TestAllConditionalBranchesReturn|TestWhenGuaranteesReturn' -v`

Expected: FAIL because return annotations are not enforced and every function receives an implicit null return.

- [ ] **Step 3: Implement structural definite-return analysis**

```go
func blockGuaranteesReturn(block *ast.BlockStatement) bool {
    if block == nil { return false }
    for _, statement := range block.Statements {
        if statementGuaranteesReturn(statement) { return true }
    }
    return false
}

func statementGuaranteesReturn(statement ast.Statement) bool {
    switch s := statement.(type) {
    case *ast.ReturnStmt:
        return true
    case *ast.BlockStatement:
        return blockGuaranteesReturn(s)
    case *ast.IfStatement:
        return s.Alternative != nil &&
            blockGuaranteesReturn(s.Consequence) &&
            blockGuaranteesReturn(s.Alternative)
    case *ast.WhenStatement:
        if len(s.Cases) == 0 { return false }
        hasDefault := false
        for _, clause := range s.Cases {
            hasDefault = hasDefault || clause.IsDefault
            if !blockGuaranteesReturn(clause.Body) { return false }
        }
        return hasDefault
    default:
        return false
    }
}
```

Do not treat loops as guaranteed returns in this version. A `when` guarantees return only when it has a `default` and every case body guarantees return.

- [ ] **Step 4: Enforce return contracts in `ReturnStmt` and `compileFunction`**

Add the field next to `funcReturnType` in `Compiler`:

```go
funcReturnType      ast.NoxyType
currentFunctionName string
```

Replace the `ReturnStmt` compiler case with:

```go
case *ast.ReturnStmt:
    expected := c.funcReturnType
    functionName := c.currentFunctionName

    if n.ReturnValue == nil {
        if expected != nil && expected.String() != "void" {
            return nil, nil, fmt.Errorf(
                "[line %d] function '%s' must return %s",
                n.Token.Line, functionName, expected.String(),
            )
        }
        c.emitByte(byte(chunk.OP_NULL))
        c.emitByte(byte(chunk.OP_RETURN))
        return c.currentChunk, nil, nil
    }

    _, actual, err := c.Compile(n.ReturnValue)
    if err != nil { return nil, nil, err }
    if expected == nil {
        c.emitByte(byte(chunk.OP_RETURN))
        return c.currentChunk, nil, nil
    }
    if expected.String() == "void" {
        return nil, nil, fmt.Errorf(
            "[line %d] void function '%s' cannot return %s",
            n.Token.Line, functionName, noxyTypeName(actual),
        )
    }
    if ref, ok := actual.(*ast.RefType); ok {
        if _, expectsRef := expected.(*ast.RefType); !expectsRef {
            c.emitByte(byte(chunk.OP_DEREF))
            c.emitByte(byte(chunk.OP_COPY))
            actual = ref.ElementType
        }
    }
    if !c.areStrictTypesCompatible(expected, actual) {
        return nil, nil, fmt.Errorf(
            "[line %d] return type mismatch in '%s': expected %s, got %s",
            n.Token.Line, functionName, expected.String(), noxyTypeName(actual),
        )
    }
    c.emitByte(byte(chunk.OP_RETURN))
    return c.currentChunk, nil, nil
```

At the start of `compileFunction`, normalize the contract and attach it to the child compiler:

```go
declaredReturn := normalizeReturnType(returnType)
fnCompiler.funcReturnType = declaredReturn
fnCompiler.currentFunctionName = name
```

Before compiling a non-void function body:

```go
if declaredReturn.String() != "void" && !blockGuaranteesReturn(body) {
    return value.Value{}, nil, fmt.Errorf(
        "[line %d] function '%s' may finish without returning %s",
        body.Token.Line, name, declaredReturn.String(),
    )
}
```

After body compilation, emit implicit `OP_NULL; OP_RETURN` only when the normalized return type is `void`.

```go
if declaredReturn.String() == "void" {
    fnCompiler.emitBytes(byte(chunk.OP_NULL), byte(chunk.OP_RETURN))
}
```

- [ ] **Step 5: Run all compiler tests**

Run: `gofmt -w internal/compiler/function_types.go internal/compiler/function_types_test.go internal/compiler/compiler.go && go test ./internal/compiler`

Expected: PASS.

- [ ] **Step 6: Commit return checking**

```text
git add internal/compiler/function_types.go internal/compiler/function_types_test.go internal/compiler/compiler.go
git commit -m "feat(compiler): enforce function return contracts"
```

---

### Task 7: Preserve dynamic callables and execute exact higher-order functions

**Files:**
- Modify: `internal/compiler/function_types_test.go`
- Create: `internal/vm/function_types_test.go`
- Modify: `noxy_examples/closure_examples.nx:42-45`

**Interfaces:**
- Bare `func` assignment accepts only callable values.
- Exact-to-bare widening preserves runtime behavior.
- Bare-to-exact narrowing fails at compile time.

- [ ] **Step 1: Add dynamic compatibility compiler tests**

```go
func TestBareFunctionCompatibility(t *testing.T) {
    valid := []string{
        `let f: func = func(v: int) -> int
    return v
end
f(1)`,
        `func apply(f: func, value: int) -> any
    return f(value)
end`,
        `func decorate(f: func) -> func
    return f
end`,
        `func register_handler(handler: func) -> void
    return
end`,
        `let fs: func[] = [
    func() -> int
        return 1
    end,
    func(v: int) -> int
        return v
    end
]`,
    }
    for _, input := range valid {
        if _, err := compileFunctionSource(t, input); err != nil { t.Fatal(err) }
    }

    _, err := compileFunctionSource(t, `
let dynamic: func = func(v: int) -> int
    return v
end
let exact: func(int) -> int = dynamic`)
    if err == nil || !strings.Contains(err.Error(), "expected func(int) -> int, got func") {
        t.Fatalf("error=%v", err)
    }
}
```

- [ ] **Step 2: Add VM execution tests**

Create `internal/vm/function_types_test.go` with a whole-program capture helper and the two execution tests:

```go
package vm

import (
    "noxy-vm/internal/compiler"
    "noxy-vm/internal/lexer"
    "noxy-vm/internal/parser"
    "noxy-vm/internal/value"
    "testing"
)

func runTypedFunctionProgram(t *testing.T, input string) value.Value {
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
    machine := New()
    captured := value.NewNull()
    machine.DefineNative("test_report", func(args []value.Value) value.Value {
        if len(args) != 0 { captured = args[0] }
        return value.NewNull()
    })
    if err := machine.Interpret(bytecode); err != nil {
        t.Fatalf("vm error: %v", err)
    }
    return captured
}

func TestExecutesExactHigherOrderFunction(t *testing.T) {
    got := runTypedFunctionProgram(t, `
func add(a: int, b: int) -> int
    return a + b
end
func apply(f: func(int, int) -> int, a: int, b: int) -> int
    return f(a, b)
end
test_report(apply(add, 20, 22))`)
    testExpectedObject(t, 42, got)
}

func TestExecutesExactClosureReturn(t *testing.T) {
    got := runTypedFunctionProgram(t, `
func makeAdder(base: int) -> func(int) -> int
    return func(value: int) -> int
        return base + value
    end
end
let add10: func(int) -> int = makeAdder(10)
test_report(add10(5))`)
    testExpectedObject(t, 15, got)
}

func TestExecutesExactReferenceArgument(t *testing.T) {
    got := runTypedFunctionProgram(t, `
func increment(value: ref int) -> void
    value = value + 1
end
let answer: int = 41
increment(answer)
test_report(answer)`)
    testExpectedObject(t, 42, got)
}
```

Expected captured values: integer `42` for the higher-order call, integer `15` for the returned closure, and integer `42` after reference mutation.

- [ ] **Step 3: Run focused and existing first-class tests**

The new return rule correctly rejects the existing `any -> int` cache return in `closure_examples.nx`. Tighten the already-integer cache before running compatibility coverage:

```noxy
let cache: map[int, int] = {}

return func(x: int) -> int
    let cached: int = cache[x]
```

Only those two annotations change; missing map entries continue to produce the runtime `null` sentinel used by the following condition.

Run:

```text
gofmt -w internal/compiler/function_types_test.go internal/vm/function_types_test.go
go test ./internal/compiler ./internal/vm
go run cmd/noxy/main.go noxy_examples/test_first_class.nx
go run cmd/noxy/main.go noxy_examples/closure_examples.nx
```

Expected: all Go tests pass; both existing examples complete successfully without syntax migration.

- [ ] **Step 4: Commit compatibility coverage**

```text
git add internal/compiler/function_types_test.go internal/vm/function_types_test.go noxy_examples/closure_examples.nx
git commit -m "test: cover exact and dynamic function values"
```

---

### Task 8: Document, benchmark, and verify the complete feature

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md`
- Modify: `internal/compiler/compiler.go:1310-1316` to remove the pre-existing unreachable tail reported by `go vet`.

**Interfaces:**
- Produces: public documentation for bare and exact callable types.
- Produces: clean required verification output.

- [ ] **Step 1: Update the language specification**

Insert this subsection after `4.1 Definition`, then rename the current `4.2 Parameter Passing Semantics` heading to `4.3 Parameter Passing Semantics`:

````markdown
### 4.2 Function Types

Every function value is callable. Noxy provides two ways to describe how precisely it may be called.

An **exact function type** records parameter and return types:

```noxy
func(int, int) -> int
func(string) -> void
func(ref int) -> void

let exact: func(int) -> int = double

func apply(f: func(int) -> int, value: int) -> int
    return f(value)
end
```

Calls through exact types are checked during compilation: arity, argument types, `ref` addressability, and return types must match. Exact signatures are invariant; parameter count, parameter types, `ref` modifiers, and return types must be identical. An omitted return annotation on a function declaration or literal means `void`.

Bare `func` is the **dynamic callable type**. It guarantees only that the value is callable:

```noxy
let dynamic: func = exact       // exact-to-dynamic widening is valid
let exact_again: func(int) -> int = dynamic // ERROR: no implicit narrowing
```

Calls through bare `func` are checked by the runtime because their arity and result type are not statically known. This keeps dynamic callbacks, decorators, handlers, and heterogeneous callable collections compatible:

```noxy
let callbacks: func[] = [no_arguments, two_arguments]
```

Use parentheses for an array whose elements are exact functions. Without parentheses, the array belongs to the return type:

```noxy
let transforms: (func(int) -> int)[] = [double, increment]
let factory: func(int) -> int[]
```

Exact function types may also appear in parameters, returns, struct fields, map values, channels, and references. `any`, native functions, plugins, and untyped module exports remain dynamic boundaries and retain runtime validation.
````

- [ ] **Step 2: Remove the known unreachable compiler tail**

Delete exactly this unreachable block after the `WhenStatement` case's unconditional return; do not change reachable loop cleanup logic elsewhere:

```go
// Pop Loop info
c.loops = c.loops[:len(c.loops)-1]

// 13. End Wrapper Scope (pops iterator vars)
c.endScope()

return c.currentChunk, nil, nil
```

- [ ] **Step 3: Format and run static/unit verification**

Run:

```text
gofmt -w internal/ast/ast.go internal/parser/parser.go internal/parser/function_type_test.go internal/compiler/compiler.go internal/compiler/function_types.go internal/compiler/function_types_test.go internal/compiler/function_types_benchmark_test.go internal/vm/function_types_test.go
go test ./internal/...
go test ./...
go vet ./...
```

Expected: all commands exit 0. `go vet` no longer reports `compiler.go:1311:3: unreachable code`.

- [ ] **Step 4: Compare the compiler benchmark**

Run:

```text
go test -run '^$' -bench BenchmarkCompileTypedFunctionCalls -benchmem -count 5 ./internal/compiler
```

Expected: five samples. Compare their median `ns/op` with Task 1. A regression above 10% requires profiling with:

```text
go test -run '^$' -bench BenchmarkCompileTypedFunctionCalls -cpuprofile typed-functions.cpu ./internal/compiler
go tool pprof -top typed-functions.cpu
```

Do not commit `typed-functions.cpu`.

- [ ] **Step 5: Run integration with a freshly built binary**

Run:

```text
go build -o noxy.exe ./cmd/noxy
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: the runner reports zero failures. Building `noxy.exe` first ensures worker processes use this branch rather than the old ignored binary from another checkout.

- [ ] **Step 6: Inspect final scope and commit**

Run:

```text
git status --short
git diff --check
git diff develop...HEAD --stat
```

Expected: only planned source, test, and documentation files are tracked; generated `noxy.exe` remains ignored.

Commit:

```text
git add docs/NOXY_LANGUAGE_SPEC.md internal/compiler/compiler.go
git commit -m "docs: specify exact and dynamic function types"
```

---

## Completion Criteria

- Direct named user-function calls reject incorrect arity and argument types at compile time.
- User-function returns are checked against their annotations, and non-void fallthrough is rejected.
- Exact higher-order functions and closures parse, compile, and execute.
- Existing bare-`func` examples execute without mandatory migration.
- Forward calls and mutual recursion resolve from predeclared signatures.
- Exact-to-bare widening succeeds; bare-to-exact narrowing fails.
- The compiler remains linear and the benchmark regression is at most 10%, or an explained/profiled exception is documented.
- Unit, full, vet, and freshly built integration checks pass.
