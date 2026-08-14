# Runtime Defer and Unwind Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Go-style `defer call(...)` and a bounded central unwind machine that runs LIFO cleanup on normal return and runtime error without corrupting frames, stack, instruction pointers, or upvalues.

**Architecture:** The frontend emits `OP_DEFER` after the same operand preparation used by ordinary calls. Each `CallFrame` stores already prepared calls and distinct stack/local bases; `finishFrame` handles one normal return, while `unwindTo` removes frames only to an active error boundary. Structured runtime and unwind errors preserve original and nested cleanup causes.

**Tech Stack:** Go 1.24.0, toolchain Go 1.24.11, Noxy bytecode VM, `modernc.org/sqlite`, Go `errors` and `testing` packages.

## Global Constraints

- Accept only `defer call(...)`; do not add deferred blocks, `try`, `catch`, or `finally`.
- Evaluate the callee and arguments left-to-right at registration time.
- Apply Noxy's shallow-copy rule once at registration for non-`ref` closure and signed-native parameters; preserve explicit references.
- Preserve legacy unsigned-native and struct-constructor argument behavior exactly as specified in the approved design.
- Execute deferred calls exactly once in LIFO order per frame on explicit return, implicit return, script/module completion, and runtime error.
- Normal return finalizes exactly one frame; runtime failure unwinds only to the active `run` boundary.
- Keep slots and upvalues live until all deferred calls owned by that frame finish.
- Continue older cleanups after a cleanup failure; preserve nested errors without flattening them.
- Keep existing public resource ownership, builtin signatures, result shapes, and suppressed underlying `Close()` behavior unchanged.
- Preserve detached `spawn` behavior; this feature only guarantees cleanup inside its independent VM.
- Use test-first red-green-refactor for every behavior.
- Keep unrelated user changes untouched.

---

## Planned File Structure

### New files

- `internal/vm/runtime_errors.go` â€” structured source, runtime, deferred, and unwind errors.
- `internal/vm/runtime_errors_test.go` â€” wrapping, ordering, nesting, and rendering tests.
- `internal/vm/defer.go` â€” prepared-call capture and invocation.
- `internal/vm/defer_test.go` â€” registration timing, callable matrix, LIFO, and cleanup behavior.
- `internal/vm/unwind.go` â€” single-frame finalization and bounded error unwind.
- `internal/vm/unwind_test.go` â€” boundary and runtime invariant tests.
- `noxy_examples/defer_lifo.nx` â€” executable language example.

### Modified files

- `internal/token/token.go`, `internal/lexer/lexer_test.go` â€” keyword and lexing.
- `internal/ast/ast.go`, `internal/parser/parser.go`, `internal/parser/parser_test.go` â€” call-only statement syntax.
- `internal/chunk/chunk.go` â€” opcode name and disassembly.
- `internal/compiler/compiler.go`, `internal/compiler/builtin_calls.go` and tests â€” selectable call emission.
- `internal/vm/vm.go`, `calls.go`, `executor.go`, `modules.go`, `builtins_concurrency.go` â€” preparation, frame layout, unwind, and boundaries.
- `internal/vm/module_exports_test.go`, `builtins_concurrency_test.go`, resource tests, and `architecture_test.go` â€” regressions and guards.
- `docs/NOXY_LANGUAGE_SPEC.md`, `CHANGELOG.md` â€” public contract.

---

### Task 1: Add `defer` syntax restricted to call expressions

**Files:**
- Modify: `internal/token/token.go:18-40,117-150`
- Modify: `internal/lexer/lexer_test.go`
- Modify: `internal/ast/ast.go:140-223`
- Modify: `internal/parser/parser.go:120-187,308-336`
- Modify: `internal/parser/parser_test.go`

**Interfaces:**
- Produces: `token.DEFER` and `*ast.DeferStmt` with `Call *ast.CallExpression`.
- Preserves: existing statement and newline parsing.

- [ ] **Step 1: Write failing lexer and parser tests**

```go
func TestDeferIsKeyword(t *testing.T) {
	lex := New("defer cleanup(1)\n")
	if got := lex.NextToken(); got.Type != token.DEFER || got.Literal != "defer" {
		t.Fatalf("token=%#v, want DEFER", got)
	}
}

func TestParseDeferCallStatement(t *testing.T) {
	p := New(lexer.New("defer cleanup(1)\n"))
	program := p.ParseProgram()
	checkParserErrors(t, p)
	stmt, ok := program.Statements[0].(*ast.DeferStmt)
	if !ok || stmt.Call == nil || stmt.Call.Function.String() != "cleanup" || len(stmt.Call.Arguments) != 1 {
		t.Fatalf("statement=%#v, want deferred cleanup call", program.Statements[0])
	}
}

func TestParseDeferRejectsNonCallExpression(t *testing.T) {
	for _, source := range []string{"defer value\n", "defer 1 + 2\n"} {
		p := New(lexer.New(source))
		_ = p.ParseProgram()
		if len(p.Errors()) == 0 || !strings.Contains(strings.Join(p.Errors(), "\n"), "defer expects a call") {
			t.Fatalf("source=%q errors=%v", source, p.Errors())
		}
	}
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run: `go test ./internal/lexer ./internal/parser -run 'TestDefer' -v`

Expected: build failure because `token.DEFER` and `ast.DeferStmt` do not exist.

- [ ] **Step 3: Add the token, AST node, and parser path**

Add `DEFER TokenType = "DEFER"` beside `RETURN`, add `"defer": DEFER` to
`keywords`, and add:

```go
type DeferStmt struct {
	Token token.Token
	Call  *CallExpression
}

func (ds *DeferStmt) statementNode()       {}
func (ds *DeferStmt) TokenLiteral() string { return ds.Token.Literal }
func (ds *DeferStmt) String() string       { return "defer " + ds.Call.String() }
```

Route `token.DEFER` from `parseStatement` to:

```go
func (p *Parser) parseDeferStatement() ast.Statement {
	stmt := &ast.DeferStmt{Token: p.curToken}
	p.nextToken()
	expression := p.parseExpression(LOWEST)
	call, ok := expression.(*ast.CallExpression)
	if !ok {
		p.errors = append(p.errors, fmt.Sprintf(
			"[%d:%d] SyntaxError: defer expects a call expression",
			stmt.Token.Line, stmt.Token.Column,
		))
		return nil
	}
	stmt.Call = call
	if p.peekTokenIs(token.NEWLINE) { p.nextToken() }
	return stmt
}
```

- [ ] **Step 4: Run syntax tests and the parser suite**

Run: `go test ./internal/lexer ./internal/parser -run 'TestDefer' -v`

Run: `go test ./internal/lexer ./internal/parser`

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/token/token.go internal/lexer/lexer_test.go internal/ast/ast.go internal/parser/parser.go internal/parser/parser_test.go
git commit -m "feat(parser): add defer call syntax"
```

---

### Task 2: Emit `OP_DEFER` through every real call path

**Files:**
- Modify: `internal/chunk/chunk.go:7-80,82-190,330-390`
- Modify: `internal/compiler/compiler.go:1390-1520,1570-1752`
- Modify: `internal/compiler/builtin_calls.go`
- Modify: `internal/compiler/compiler_test.go`
- Modify: `internal/compiler/builtin_calls_test.go`

**Interfaces:**
- Produces: `chunk.OP_DEFER`, `callEmission`, `emitCall`, and selectable immediate/deferred dispatch.
- Consumes: `ast.DeferStmt` from Task 1.

- [ ] **Step 1: Write failing compiler and opcode tests**

```go
func compiledFunction(t *testing.T, source, name string) *value.ObjFunction {
	t.Helper()
	code, _, err := New().Compile(parse(source))
	if err != nil { t.Fatal(err) }
	for _, constant := range code.Constants {
		if constant.Type == value.VAL_FUNCTION {
			fn := constant.Obj.(*value.ObjFunction)
			if fn.Name == name { return fn }
		}
	}
	t.Fatalf("function %q not found", name)
	return nil
}

func TestCompileDeferEmitsArgCount(t *testing.T) {
	fn := compiledFunction(t, `
func cleanup(value: int) -> void
end
func run() -> void
    defer cleanup(7)
end`, "run")
	code := fn.Chunk.(*chunk.Chunk).Code
	for index, instruction := range code {
		if chunk.OpCode(instruction) == chunk.OP_DEFER {
			if index+1 >= len(code) || code[index+1] != 1 { t.Fatalf("bad OP_DEFER operand") }
			return
		}
	}
	t.Fatal("compiled function omitted OP_DEFER")
}

func TestCompileDeferRejectsAddrPseudoCall(t *testing.T) {
	_, _, err := New().Compile(parse("let value: int = 1\ndefer addr(ref value)\n"))
	if err == nil || !strings.Contains(err.Error(), "cannot defer addr") { t.Fatalf("error=%v", err) }
}
```

Add this source table to `builtin_calls_test.go` and compile each `run` body:

```go
tests := []struct{ name, body string }{
	{"append", "let items: int[] = [1]\ndefer append(ref items, 2)"},
	{"pop", "let items: int[] = [1]\ndefer pop(ref items)"},
	{"delete", "let items: map[string, int] = {\"x\": 1}\ndefer delete(ref items, \"x\")"},
	{"json_loads", "let target: int = 0\ndefer json_loads(\"1\", ref target)"},
	{"chan_send", "let ch: chan int = make_chan(1)\ndefer chan_send(ch, 1)"},
	{"chan_recv", "let ch: chan int = make_chan(1)\ndefer chan_recv(ch)"},
}
for _, test := range tests {
	t.Run(test.name, func(t *testing.T) {
		fn := compiledFunction(t, "func run() -> void\n"+test.body+"\nend\n", "run")
		if !containsOpcode(fn.Chunk.(*chunk.Chunk).Code, chunk.OP_DEFER) {
			t.Fatalf("%s omitted OP_DEFER", test.name)
		}
	})
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/compiler -run 'TestCompileDefer' -v`

Expected: build failure because `chunk.OP_DEFER` is undefined.

- [ ] **Step 3: Add opcode naming and disassembly**

Add `OP_DEFER` immediately after `OP_CALL`, map it in `OpCode.String`, and
disassemble it with `byteInstruction("OP_DEFER", offset)`.

- [ ] **Step 4: Factor selectable call emission**

```go
type callEmission uint8

const (
	emitImmediateCall callEmission = iota
	emitDeferredCall
)

func (c *Compiler) emitCall(argCount int, emission callEmission) {
	op := chunk.OP_CALL
	if emission == emitDeferredCall { op = chunk.OP_DEFER }
	c.emitBytes(byte(op), byte(argCount))
}
```

Extract the current call-expression compiler to
`compileCallExpression(call *ast.CallExpression, emission callEmission)`.
Make the expression case immediate and the `DeferStmt` case deferred. Reject
`addr`. Pass `emission` through `compileBuiltinCall` and the `chan_send` and
`chan_recv` branches, preserving all type/reference markers before the final
selected opcode.

- [ ] **Step 5: Run compiler and chunk tests**

Run: `go test ./internal/compiler ./internal/chunk -run 'TestCompileDefer|Test.*Builtin' -v`

Run: `go test ./internal/compiler ./internal/chunk`

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/chunk/chunk.go internal/compiler/compiler.go internal/compiler/builtin_calls.go internal/compiler/compiler_test.go internal/compiler/builtin_calls_test.go
git commit -m "feat(compiler): emit deferred calls"
```

---

### Task 3: Preserve runtime and cleanup errors structurally

**Files:**
- Create: `internal/vm/runtime_errors.go`
- Create: `internal/vm/runtime_errors_test.go`
- Modify: `internal/vm/vm.go:13-24`
- Modify: `internal/vm/calls.go:40-88`
- Modify: `internal/vm/executor.go:996-1007`

**Interfaces:**
- Produces: `SourceLocation`, `RuntimeError`, `DeferredError`, `UnwindError`, and `runtimeErrorCause`.
- Preserves: the existing leading `[file:line N]` error text.

- [ ] **Step 1: Write failing structured-error tests**

```go
func TestRuntimeErrorPreservesCause(t *testing.T) {
	sentinel := errors.New("native sentinel")
	err := &RuntimeError{Location: SourceLocation{File: "main.nx", Line: 4}, Message: "native failed", Cause: sentinel}
	if !errors.Is(err, sentinel) || err.Error() != "[main.nx:line 4] native failed: native sentinel" {
		t.Fatalf("error=%q unwrap=%v", err, errors.Is(err, sentinel))
	}
}

func TestUnwindErrorPreservesLIFOOrderAndNestedCause(t *testing.T) {
	original := errors.New("original")
	nested := &UnwindError{Deferred: []DeferredError{{Registration: SourceLocation{File: "cleanup.nx", Line: 9}, Cause: errors.New("nested")}}}
	err := &UnwindError{Primary: original, Deferred: []DeferredError{
		{Registration: SourceLocation{File: "main.nx", Line: 8}, Cause: nested},
		{Registration: SourceLocation{File: "main.nx", Line: 7}, Cause: errors.New("older")},
	}}
	if !errors.Is(err, original) { t.Fatal("primary cause lost") }
	text := err.Error()
	if strings.Index(text, "line 8") > strings.Index(text, "line 7") || strings.Count(text, "cleanup.nx:line 9") != 1 {
		t.Fatalf("bad nested rendering:\n%s", text)
	}
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/vm -run 'TestRuntimeError|TestUnwindError' -v`

Expected: build failure for missing structured error types.

- [ ] **Step 3: Implement error types and constructors**

```go
type SourceLocation struct { File string; Line int }
type RuntimeError struct { Location SourceLocation; Message string; Cause error }
type DeferredError struct { Registration SourceLocation; Cause error }
type UnwindError struct { Primary error; Deferred []DeferredError }

func (e *RuntimeError) Unwrap() error { return e.Cause }
func (e DeferredError) Unwrap() error { return e.Cause }
func (e *UnwindError) Unwrap() []error {
	causes := make([]error, 0, len(e.Deferred)+1)
	if e.Primary != nil { causes = append(causes, e.Primary) }
	for index := range e.Deferred { causes = append(causes, &e.Deferred[index]) }
	return causes
}
```

Implement deterministic `Error()` rendering with nested indentation. Add
`sourceLocation(c, ip)`, `runtimeErrorCause`, and keep `runtimeError` as the
cause-free convenience constructor.

- [ ] **Step 4: Preserve native and module causes**

Use `runtimeErrorCause(c, ip, err, "native '%s' failed", native.Name)` for
native errors. Wrap import failures with an explicit `Cause`; never pass an
existing error through `%s`.

- [ ] **Step 5: Run focused and VM regression tests**

Run: `go test ./internal/vm -run 'TestRuntimeError|TestUnwindError|Test.*Module.*Error' -v`

Run: `go test ./internal/vm`

Expected: PASS and existing error substrings remain compatible.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm/runtime_errors.go internal/vm/runtime_errors_test.go internal/vm/vm.go internal/vm/calls.go internal/vm/executor.go
git commit -m "refactor(vm): preserve structured runtime errors"
```

---

### Task 4: Prepare deferred calls and separate frame bases

**Files:**
- Create: `internal/vm/defer.go`
- Create: `internal/vm/defer_test.go`
- Modify: `internal/vm/vm.go:26-57`
- Modify: `internal/vm/calls.go`
- Modify: `internal/vm/executor.go:31-42,190-220,425-440,880-895`
- Modify: `internal/vm/builtins_concurrency.go:55-70`

**Interfaces:**
- Produces: `PreparedCall`, `prepareDeferredCall`, `invokePreparedCall`, `CallFrame.StackBase`, and `CallFrame.LocalBase`.
- Consumes: `SourceLocation` from Task 3.
- Preserves: immediate-call behavior and compiler local-slot numbering.

- [ ] **Step 1: Write failing prepared-call and frame-layout tests**

Create `internal/vm/defer_test.go` and cover the four callable kinds. The
legacy-native case is:

```go
func TestPrepareDeferredCallLegacyNativeKeepsValueIdentity(t *testing.T) {
	machine := New()
	array := value.NewArray([]value.Value{value.NewInt(1)})
	legacy := value.NewNative("legacy", func([]value.Value) value.Value { return value.NewNull() })
	prepared, err := machine.prepareDeferredCall(legacy, []value.Value{array}, SourceLocation{File: "test.nx", Line: 1})
	if err != nil || prepared.Arguments[0].Obj != array.Obj {
		t.Fatalf("prepared=%#v error=%v, want unchanged identity", prepared, err)
	}
}
```

Add direct preparation assertions for every remaining row:

```go
closureValue := value.NewFunction("cleanup", 1, 0, []value.ParamInfo{{TypeName: "int[]"}}, chunk.New(), machine.shared.Root)
closure := &value.ObjClosure{Function: closureValue.Obj.(*value.ObjFunction), Environment: machine.shared.Root}
prepared, err := machine.prepareDeferredCall(value.Value{Type: value.VAL_FUNCTION, Obj: closure}, []value.Value{array}, SourceLocation{})
if err != nil || prepared.Arguments[0].Obj == array.Obj { t.Fatalf("closure did not shallow-copy array") }

reference := value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_GLOBAL, Name: "items", GlobalOwner: machine.shared.Root}}
closure.Function.Params[0].IsRef = true
prepared, err = machine.prepareDeferredCall(value.Value{Type: value.VAL_FUNCTION, Obj: closure}, []value.Value{reference}, SourceLocation{})
if err != nil || prepared.Arguments[0].Obj != reference.Obj { t.Fatalf("reference identity changed") }

signature := value.NativeSignature{Arity: 1, Params: []value.ParamInfo{{TypeName: "int[]"}}}
signed := value.NewNativeWithSignature("signed", signature, func([]value.Value) value.Value { return value.NewNull() })
prepared, err = machine.prepareDeferredCall(signed, []value.Value{array}, SourceLocation{})
if err != nil || prepared.Arguments[0].Obj == array.Obj { t.Fatalf("signed native did not shallow-copy array") }

constructor := value.NewStruct("Box", []string{"items"})
prepared, err = machine.prepareDeferredCall(constructor, []value.Value{array}, SourceLocation{})
if err != nil || prepared.Arguments[0].Obj != array.Obj { t.Fatalf("constructor capture changed identity") }

if _, err = machine.prepareDeferredCall(constructor, nil, SourceLocation{}); err == nil { t.Fatal("constructor arity accepted") }
```

Add the source-level local-layout regression:

```go
func TestScriptLocalBaseDoesNotCollideWithCalleeSlot(t *testing.T) {
	got := captureVMSource(t, `
if true then
    let local: int = 42
    test_report(local)
end`)
	if !valuesEqual(got, value.NewInt(42)) { t.Fatalf("local=%v, want 42", got) }
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/vm -run 'TestPrepareDeferredCall|TestScriptLocalBase' -v`

Expected: build failure for the missing prepared-call API and frame bases.

- [ ] **Step 3: Split `Slots` into `StackBase` and `LocalBase`**

Change `CallFrame` and every `.Slots` use:

- script frame: `StackBase: 0`, `LocalBase: 1`;
- ordinary call: both `vm.stackTop - argCount - 1`;
- spawn frame: both zero;
- `OP_GET_LOCAL`, `OP_SET_LOCAL`, `OP_REF_LOCAL`, and local-upvalue capture use
  `LocalBase`;
- terminal window cleanup will use `StackBase` in Task 5.

- [ ] **Step 4: Implement preparation by callable kind**

```go
type PreparedCall struct {
	Callee       value.Value
	Arguments    []value.Value
	Registration SourceLocation
}

func (vm *VM) prepareDeferredCall(callee value.Value, args []value.Value, registration SourceLocation) (PreparedCall, error)
```

Reuse `validateParameterModes`, native signatures, closure parameter metadata,
and `validateStructConstructorArguments`. Copy non-`ref` signed parameters with
`vm.copyValue`; copy only the `[]value.Value` slice for unsigned natives and
constructors. Never retain an operand-stack slice.

- [ ] **Step 5: Add prepared dispatch without a second copy**

Factor call dispatch so normal calls prepare arguments and prepared calls skip
that preparation:

```go
func (vm *VM) callPreparedValue(callee value.Value, argCount int, c *chunk.Chunk, ip int) (bool, error)
func (vm *VM) invokePreparedCall(call PreparedCall) error
```

`invokePreparedCall` records `base := vm.stackTop`, pushes callee and captured
arguments, dispatches, runs any new Noxy frame to the owner boundary, discards
the result, clears temporary slots, and restores `stackTop = base` on every
exit path.

- [ ] **Step 6: Run preparation, call, and local-layout tests**

Run: `go test ./internal/vm -run 'TestPrepareDeferredCall|TestScriptLocalBase|Test.*ClosureCall|Test.*Native' -v`

Run: `go test ./internal/vm`

Expected: PASS with no immediate-call regression.

- [ ] **Step 7: Commit**

```powershell
git add internal/vm/defer.go internal/vm/defer_test.go internal/vm/vm.go internal/vm/calls.go internal/vm/executor.go internal/vm/builtins_concurrency.go
git commit -m "refactor(vm): prepare deferred calls"
```

---

### Task 5: Execute LIFO cleanup on normal return

**Files:**
- Create: `internal/vm/unwind.go`
- Create: `internal/vm/unwind_test.go`
- Modify: `internal/vm/executor.go:46-60,846-955`
- Modify: `internal/vm/defer.go`

**Interfaces:**
- Produces: `frameOutcome`, `finishFrame`, `finalizeCurrentFrame`, and executable `OP_DEFER`.
- Consumes: `PreparedCall` and structured errors.

- [ ] **Step 1: Write failing normal-return tests**

```go
func TestDeferredCallsRunLIFOOnExplicitReturn(t *testing.T) {
	machine := New()
	var order []int64
	machine.DefineNative("record", func(args []value.Value) value.Value {
		order = append(order, args[0].AsInt)
		return value.NewNull()
	})
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		captured = args[0]
		return value.NewNull()
	})
	err := interpretVMSource(t, machine, `
func work() -> int
    defer record(1)
    defer record(2)
    return 7
end
test_report(work())`)
	if err != nil || !slices.Equal(order, []int64{2, 1}) || !valuesEqual(captured, value.NewInt(7)) {
		t.Fatalf("error=%v order=%v result=%v", err, order, captured)
	}
}
```

Add these exact sources using the same `record` native:

```go
cases := []struct { source string; want []int64 }{
	{`func work() -> void
    defer record(1)
    record(0)
end
work()`, []int64{0, 1}},
	{`defer record(1)
defer record(2)
record(0)`, []int64{0, 2, 1}},
}
```

Interpret each source in a fresh VM and compare `order` with `want` using
`slices.Equal`.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/vm -run 'TestDeferredCallsRun' -v`

Expected: failure because `OP_DEFER` is not executed.

- [ ] **Step 3: Register prepared calls in the executor**

Handle `OP_DEFER`: read argument count; derive the registration location from
the opcode position; prepare `vm.peek(argCount)` and the arguments; consume
callee plus arguments; append to `frame.Deferred`; push no value. On failure,
append nothing and return a structured runtime error.

- [ ] **Step 4: Implement single-frame finalization**

Create `unwind.go`:

```go
type frameOutcome struct {
	Result value.Value
	Err    error
}

func (vm *VM) finishFrame(outcome frameOutcome) frameOutcome
func (vm *VM) finalizeCurrentFrame(outcome frameOutcome) frameOutcome
```

Remove each deferred entry before invocation. After all entries, close
upvalues, clear from `StackBase` to the old `stackTop`, nil the frame-array
entry, decrement `frameCount`, and update `currentFrame`. Push a successful
result only when a caller survives; terminal script/spawn completion leaves
`stackTop == 0`.

- [ ] **Step 5: Route `OP_RETURN` through `finishFrame`**

Replace inline return teardown with a successful `frameOutcome`. Return a
cleanup error; otherwise return from `run` only when below `minFrameCount`, or
reload the surviving frame/chunk/IP and continue.

- [ ] **Step 6: Verify upvalues and terminal state**

Add a deferred closure reading a captured local, then assert:

```go
if machine.frameCount != 0 || machine.currentFrame != nil || machine.stackTop != 0 || machine.frames[0] != nil || machine.openUpvalues != nil {
	t.Fatalf("dirty terminal VM: frames=%d current=%p stack=%d open=%p", machine.frameCount, machine.currentFrame, machine.stackTop, machine.openUpvalues)
}
```

Run: `go test ./internal/vm -run 'TestDeferred|Test.*Upvalue|TestStack' -v`

Run: `go test ./internal/vm`

Expected: PASS.

- [ ] **Step 7: Commit**

```powershell
git add internal/vm/unwind.go internal/vm/unwind_test.go internal/vm/defer.go internal/vm/defer_test.go internal/vm/executor.go
git commit -m "feat(vm): run defers on normal return"
```

---

### Task 6: Unwind runtime errors to the active boundary

**Files:**
- Modify: `internal/vm/unwind.go`
- Modify: `internal/vm/unwind_test.go`
- Modify: `internal/vm/executor.go:46-60`
- Modify: `internal/vm/defer_test.go`

**Interfaces:**
- Produces: `unwindTo(targetFrameCount, errorOutcome)` and the `run` error funnel.
- Preserves: inner successful returns continue the active run.

- [ ] **Step 1: Write failing runtime-error tests**

```go
func TestRuntimeErrorRunsAllDeferredCalls(t *testing.T) {
	machine := New()
	var order []int64
	machine.DefineNative("record", func(args []value.Value) value.Value {
		order = append(order, args[0].AsInt)
		return value.NewNull()
	})
	err := interpretVMSource(t, machine, `
func fail() -> void
    defer record(1)
    defer record(2)
    let zero: int = 0
    print(1 / zero)
end
fail()`)
	if err == nil || !slices.Equal(order, []int64{2, 1}) { t.Fatalf("error=%v order=%v", err, order) }
}

func TestCleanupFailuresAggregateAndDoNotSkipOlderEntries(t *testing.T) {
	machine := New()
	first, second := errors.New("first cleanup"), errors.New("second cleanup")
	var completed bool
	machine.DefineContextualNative("fail_first", func(value.NativeContext, []value.Value) (value.Value, error) { return value.NewNull(), first })
	machine.DefineContextualNative("fail_second", func(value.NativeContext, []value.Value) (value.Value, error) { return value.NewNull(), second })
	machine.DefineNative("complete", func([]value.Value) value.Value { completed = true; return value.NewNull() })
	err := interpretVMSource(t, machine, "defer complete()\ndefer fail_first()\ndefer fail_second()\n")
	var unwind *UnwindError
	if !errors.As(err, &unwind) || !errors.Is(err, first) || !errors.Is(err, second) || !completed || len(unwind.Deferred) != 2 {
		t.Fatalf("error=%v unwind=%#v completed=%v", err, unwind, completed)
	}
}
```

Add a normal-return conversion source:

```noxy
func inner() -> void
    defer fail_first()
end
func outer() -> void
    defer complete()
    inner()
end
outer()
```

Assert `complete` runs and `errors.Is(err, first)` succeeds. Add the nested
aggregate source:

```noxy
func nested_cleanup() -> void
    defer fail_second()
    fail_first()
end
defer nested_cleanup()
```

Assert the outer `UnwindError` has one `Deferred` entry, its `Cause` is an
inner `*UnwindError`, the inner primary matches `first`, and its sole deferred
cause matches `second`.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/vm -run 'TestRuntimeErrorRuns|TestCleanupFailures|TestNormalReturnCleanupFailure|TestNestedCleanup' -v`

Expected: runtime-error cleanup or aggregation fails.

- [ ] **Step 3: Implement bounded error unwind**

```go
func (vm *VM) unwindTo(targetFrameCount int, outcome frameOutcome) frameOutcome {
	for vm.frameCount > targetFrameCount {
		outcome = vm.finalizeCurrentFrame(outcome)
	}
	return outcome
}
```

Keep the first runtime failure as `Primary`. Append cleanup failures in
execution order. When cleanup converts a normal return to error, finish that
frame's older entries before unwinding callers.

- [ ] **Step 4: Add the `run` error funnel and persist IP**

Make `run` return a named error. Its deferred epilogue persists cached IP to a
still-current cached frame and calls `unwindTo(minFrameCount-1, ...)` for every
non-nil error. Persist `frame.IP` before `OP_RETURN`, recursive execution, and
error transfer; reload frame/chunk/IP after any surviving inner run.

- [ ] **Step 5: Test VM reuse and terminal invariants**

After failed interpretation, run these exact assertions and reuse sequence:

```go
if machine.frameCount != 0 || machine.currentFrame != nil || machine.stackTop != 0 || machine.openUpvalues != nil {
	t.Fatalf("dirty VM after error: frames=%d current=%p stack=%d open=%p", machine.frameCount, machine.currentFrame, machine.stackTop, machine.openUpvalues)
}
if err := interpretVMSource(t, machine, "test_report(42)\n"); err != nil { t.Fatal(err) }
if !valuesEqual(captured, value.NewInt(42)) { t.Fatalf("reuse result=%v", captured) }
```

For pre-dispatch restoration, register `defer one_arg()` against a dynamic
one-argument Noxy function with zero supplied arguments; assert the registration
error leaves the same terminal invariants.

Run: `go test ./internal/vm -run 'TestRuntimeErrorRuns|TestCleanupFailures|TestNormalReturnCleanupFailure|TestNestedCleanup|TestVMReusable' -v`

Run: `go test ./internal/vm`

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm/unwind.go internal/vm/unwind_test.go internal/vm/defer_test.go internal/vm/executor.go
git commit -m "feat(vm): unwind defers on runtime error"
```

---

### Task 7: Preserve module and spawn execution boundaries

**Files:**
- Modify: `internal/vm/modules.go:178-209`
- Modify: `internal/vm/executor.go:996-1010`
- Modify: `internal/vm/module_exports_test.go`
- Modify: `internal/vm/builtins_concurrency_test.go`

**Interfaces:**
- Consumes: bounded `run`/unwind from Task 6.
- Produces: saved importer IP, structured module causes, and detached cleanup coverage.

- [ ] **Step 1: Write a failing nested-module boundary test**

Create the module and compile the importer exactly as follows:

```go
root := t.TempDir()
moduleSource := `
defer module_record("module-old")
defer module_fail()
let zero: int = 0
print(1 / zero)
`
if err := os.WriteFile(filepath.Join(root, "broken.nx"), []byte(moduleSource), 0o600); err != nil { t.Fatal(err) }
code := compileModuleProgram(t, root, `
defer module_record("importer-old")
defer module_record("importer-new")
use broken
`)
```

The module source is:

```noxy
defer module_record("module-old")
defer module_fail()
let zero: int = 0
print(1 / zero)
```

Register `module_record` to append strings and `module_fail` to return a
sentinel. Assert `order == []string{"module-old", "importer-new",
"importer-old"}`, `errors.Is(err, sentinel)`, and that the outer error contains
a structured import `RuntimeError` whose cause is the module `UnwindError`.

- [ ] **Step 2: Write a detached spawn cleanup test**

Register a native that sends an integer to a buffered Go channel:

```go
completed := make(chan int64, 1)
machine.DefineNative("spawned_record", func(args []value.Value) value.Value {
	completed <- args[0].AsInt
	return value.NewNull()
})
```

Interpret:

```noxy
func worker() -> void
    defer spawned_record(1)
end
spawn(worker)
```

Wait with:

```go
select {
case got := <-completed:
	if got != 1 { t.Fatalf("cleanup=%d, want 1", got) }
case <-time.After(2 * time.Second):
	t.Fatal("spawned defer did not execute")
}
```

Repeat with `let zero: int = 0; print(1 / zero)` after registration and assert
the same channel result. Do not assert parent propagation because spawn remains
detached.

- [ ] **Step 3: Verify RED**

Run: `go test ./internal/vm -run 'TestNestedModuleDeferBoundary|TestSpawnRunsDeferredCleanup' -v`

Expected: module boundary/IP or detached cleanup behavior fails.

- [ ] **Step 4: Persist importer state and wrap module errors**

Before `loadModule`, save `frame.IP = ip`. After return, reload the current
frame and its chunk/IP. Use `runtimeErrorCause` on failure. Keep
`run(startFrameCount)` scoped to module-owned frames so the importer survives
the inner boundary.

- [ ] **Step 5: Run module, spawn, and full VM tests**

Run: `go test ./internal/vm -run 'TestNestedModuleDeferBoundary|TestSpawnRunsDeferredCleanup|Test.*Module|Test.*Spawn' -v`

Run: `go test ./internal/vm`

Expected: PASS without changing cache or detached-task semantics.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm/modules.go internal/vm/executor.go internal/vm/module_exports_test.go internal/vm/builtins_concurrency_test.go
git commit -m "test(vm): cover defer execution boundaries"
```

---

### Task 8: Prove cleanup of files, sockets, and SQLite resources

**Files:**
- Modify: `internal/vm/defer_test.go`
- Modify: `internal/vm/builtins_io_test.go`
- Modify: `internal/vm/builtins_net_test.go`
- Modify: `internal/vm/builtins_sqlite_test.go`

**Interfaces:**
- Consumes: unchanged `io.close`, `net.socket_close`, `sqlite.finalize`, and `sqlite.close` APIs.
- Produces: integration evidence for registry and underlying-resource cleanup.

- [ ] **Step 1: Write failing file cleanup tests**

Use this table with a fresh VM and `path := filepath.Join(t.TempDir(),
"deferred.txt")`:

```go
tests := []struct{ name, suffix string; wantError bool }{
	{"normal", "", false},
	{"runtime error", "\nlet zero: int = 0\nprint(1 / zero)", true},
}
source := "use io\nlet file: io.File = io.open(" + strconv.Quote(path) + ", \"w\")\ndefer io.close(file)" + test.suffix
```

Capture the sole `FileResource` from the registry before cleanup via a
test-only recording native invoked after open. After interpretation assert
`len(machine.shared.Files.snapshot()) == 0`, `resource.closed`, error presence
matches `wantError`, and `os.Remove(path)` succeeds.

- [ ] **Step 2: Write failing socket/listener cleanup tests**

```noxy
use net
let listener: net.Socket = net.listen("127.0.0.1", 0)
defer net.socket_close(listener)
```

Use normal and division-by-zero suffixes as in the file table. After each run:

```go
if got := len(machine.shared.Listeners.snapshot()); got != 0 { t.Fatalf("listeners=%d", got) }
if got := len(machine.shared.Sockets.snapshot()); got != 0 { t.Fatalf("sockets=%d", got) }
```

Capture the created listener resource through a test native receiving the
`Socket`, lock `stateMu`, and assert `closed`. Run interpretation through the
existing bounded helper so networking cannot hang.

- [ ] **Step 3: Write failing SQLite LIFO cleanup tests**

```noxy
use sqlite
let db: sqlite.Database = sqlite.open(PATH)
defer sqlite.close(db)
let stmt: sqlite.Statement = sqlite.prepare(db, "SELECT 1")
defer sqlite.finalize(stmt)
```

Run normal and division-by-zero variants. Register a test native between the
database and statement defers:

```noxy
defer sqlite.close(db)
defer assert_statement_finalized()
defer sqlite.finalize(stmt)
```

`assert_statement_finalized` inspects the shared registries when invoked and
records that statements are already absent while the database is still
present. Before return, call another test native with `db` and `stmt` so the
test retains both resource pointers. After interpretation:

```go
if got := len(machine.shared.Statements.snapshot()); got != 0 { t.Fatalf("statements=%d", got) }
if got := len(machine.shared.Databases.snapshot()); got != 0 { t.Fatalf("databases=%d", got) }
if !statementResource.closed || !databaseResource.closed { t.Fatalf("resources remain open") }
```

Assert the intermediate native observed `(statements=0, databases=1)`, proving
statement-before-database order without production instrumentation.

- [ ] **Step 4: Add failure-among-cleanups coverage**

Register resource close first and a sentinel-error contextual native second:

```noxy
defer io.close(file)
defer cleanup_fail()
```

Assert `errors.Is(err, sentinel)`, `len(machine.shared.Files.snapshot()) == 0`,
and the captured resource is closed. Repeat the ordering pattern for listener
and SQLite resources. Do not assert aggregation of suppressed OS close errors.

- [ ] **Step 5: Verify RED, then GREEN without changing resource APIs**

Run RED: `go test ./internal/vm -run 'TestDeferred(File|Socket|SQLite|Resource)' -v`

Fix only unwind or test-observability issues. Then run:

```powershell
go test ./internal/vm -run 'TestDeferred(File|Socket|SQLite|Resource)' -v
go test -race ./internal/vm -run 'TestDeferred(File|Socket|SQLite|Resource)' -count=1
```

Expected: PASS and race detector clean.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm/defer_test.go internal/vm/builtins_io_test.go internal/vm/builtins_net_test.go internal/vm/builtins_sqlite_test.go
git commit -m "test(vm): cover deferred resource cleanup"
```

---

### Task 9: Add architecture guards, docs, example, and full validation

**Files:**
- Modify: `internal/vm/architecture_test.go`
- Modify: `docs/NOXY_LANGUAGE_SPEC.md`
- Modify: `CHANGELOG.md`
- Create: `noxy_examples/defer_lifo.nx`

**Interfaces:**
- Produces: documented public contract and a guard against split teardown.
- Consumes: all prior tasks.

- [ ] **Step 1: Write a failing architecture guard**

Assert `CallFrame` contains:

```go
want := map[string]string{
	"StackBase": "int",
	"LocalBase": "int",
	"Deferred":  "[]PreparedCall",
}
```

Parse production files and reject terminal mutations such as `frameCount--`,
nil frame assignment, or clearing from `StackBase` outside `unwind.go`. Permit
frame construction in `executor.go`, `calls.go`, and `builtins_concurrency.go`.

- [ ] **Step 2: Verify the architecture guard**

Run: `go test ./internal/vm -run 'Test.*Unwind.*Architecture|TestRuntimeFoundationRequires' -v`

Expected RED until old `OP_RETURN` teardown is absent; PASS afterward.

- [ ] **Step 3: Document the language feature**

Add `Defer and deterministic cleanup` to `docs/NOXY_LANGUAGE_SPEC.md` covering
call-only syntax, immediate evaluation, shallow-copy/ref capture, frame-level
LIFO, loops/scripts/modules/spawn, success/error cleanup, discarded ordinary
results, aggregated runtime errors, and suppressed underlying close errors.
Add concise `CHANGELOG.md` entries under `Added` and `Fixed`.

- [ ] **Step 4: Add an executable example**

Create `noxy_examples/defer_lifo.nx`:

```noxy
let order: string = ""

func record(label: string) -> void
    order = order + label
end

func cleanup_demo() -> void
    defer record("1")
    defer record("2")
end

cleanup_demo()
assert(order == "21", "defer must execute in LIFO order")
print("defer LIFO: " + order)
```

- [ ] **Step 5: Format and run targeted verification**

```powershell
gofmt -w internal/token/token.go internal/ast/ast.go internal/parser/parser.go internal/chunk/chunk.go internal/compiler/compiler.go internal/compiler/builtin_calls.go internal/vm/vm.go internal/vm/calls.go internal/vm/executor.go internal/vm/modules.go internal/vm/builtins_concurrency.go internal/vm/runtime_errors.go internal/vm/defer.go internal/vm/unwind.go
go test ./internal/lexer ./internal/parser ./internal/compiler ./internal/chunk ./internal/vm
go run cmd/noxy/main.go noxy_examples/defer_lifo.nx
```

Expected: PASS and `defer LIFO: 21`.

- [ ] **Step 6: Run project-required validation**

```powershell
go test ./internal/...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
go test ./...
go vet ./...
go build ./...
go test -race ./internal/vm -count=1
```

Expected: all Go checks pass, concurrent Noxy examples report no failures, and
the race detector is clean.

- [ ] **Step 7: Review scope and placeholders**

```powershell
git diff --check
git status --short
rg -n "TBD|TODO|implement later" internal docs noxy_examples/defer_lifo.nx
```

Confirm unrelated files are untouched and pre-existing TODOs outside changed
hunks are not attributed to this feature.

- [ ] **Step 8: Commit**

```powershell
git add internal/vm/architecture_test.go docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md noxy_examples/defer_lifo.nx
git commit -m "docs: specify defer and safe unwind"
```

---

## Plan Self-Review Checklist

- Every approved requirement maps to a task and test.
- `finishFrame` and `unwindTo` stay distinct.
- Script `StackBase = 0` and `LocalBase = 1` are tested.
- Every prepared callable kind has an explicit registration rule.
- Specialized real calls are supported; `addr` is rejected.
- Nested cleanup/module errors remain structured and unflattened.
- Resource tests do not claim visibility into suppressed OS close errors.
- No task introduces `try/finally`, auto-close, supervised tasks, or changed public close APIs.
