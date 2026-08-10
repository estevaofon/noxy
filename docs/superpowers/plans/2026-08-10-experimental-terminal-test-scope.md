# Experimental Terminal Test Scope Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce the experimental terminal module's tests to durable safety and basic-usage invariants without changing production behavior.

**Architecture:** Prune implementation-coupled and duplicated cases from the runtime, builtin, and `sys.exit` test layers. Keep compiler export coverage, builtin registration/signature coverage, the public happy path, raw-mode lifecycle safety, and VM-sharing behavior unchanged.

**Tech Stack:** Go 1.24.0 with toolchain 1.24.11, Go `testing`, Noxy VM integration runner.

## Global Constraints

- Modify tests only; do not alter the terminal API, VM behavior, stdlib module, example, or language documentation.
- Treat the terminal API as experimental and free to evolve.
- Preserve strong coverage that prevents the VM from leaving a user's terminal in raw mode.
- Do not optimize for a fixed line count; every remaining test must protect a distinct durable invariant.
- Run `go test ./internal/...` and `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx` before completion.

---

### Task 1: Simplify terminal runtime coverage

**Files:**
- Modify: `internal/vm/terminal_runtime_test.go`
- Reference: `internal/vm/terminal_runtime.go`
- Reference: `docs/superpowers/specs/2026-08-10-experimental-terminal-test-scope-design.md`

**Interfaces:**
- Consumes: `terminalRuntime.openRaw() error`, `terminalRuntime.readKey() (string, error)`, `terminalRuntime.close() error`, `NewWithShared(*SharedState, VMConfig) *VM`, and `VM.Interpret(*chunk.Chunk) error`.
- Produces: focused test helpers `fakeTerminalDriver`, `newTestTerminalRuntime`, and `newVMWithActiveTestTerminal`; no production interface changes.

- [ ] **Step 1: Establish the focused baseline**

Run:

```powershell
go test ./internal/vm -run 'TestTerminal|TestSpawnedVMsShareTerminalRuntime|TestInterpret' -count=1
```

Expected: PASS before test reduction.

- [ ] **Step 2: Remove synchronization-mechanism coverage**

Delete the contiguous block beginning with `type gatedTerminalReader struct`
and ending after the closing brace of
`TestTerminalCloseMakesQueuedReadFailAfterActiveReadFinishes`. The block
contains these exact declarations: `gatedTerminalReader`,
`newGatedTerminalReader`, its `Read`, `releaseFirstRead`, and `readCount`
methods, `terminalReadResult`, `runQueuedTerminalRead`,
`waitForQueuedTerminalRead`, `receiveTerminalRead`, and the queued-read test.

These declarations inspect goroutine stacks and lock scheduling, which are not part of the experimental module's durable contract.

- [ ] **Step 3: Simplify restoration failure coverage**

Replace `TestTerminalCloseReportsRestoreFailure` with this behavior-only assertion:

```go
func TestTerminalCloseReportsRestoreFailure(t *testing.T) {
	driver := &fakeTerminalDriver{terminal: true, closeErr: errors.New("restore failed")}
	runtime := newTestTerminalRuntime(driver, "")

	if err := runtime.openRaw(); err != nil {
		t.Fatalf("openRaw() error = %v", err)
	}
	if err := runtime.close(); !errors.Is(err, driver.closeErr) {
		t.Fatalf("close() error = %v, want %v", err, driver.closeErr)
	}
}
```

Delete these exact error-precedence tests:

```go
func TestInterpretReportsRestoreErrorWhenExecutionSucceeds(t *testing.T)
func TestInterpretPreservesRuntimeErrorWhenRestoreAlsoFails(t *testing.T)
```

Keep `TestInterpretRestoresTerminalAfterSuccess` and `TestInterpretRestoresTerminalAfterRuntimeError` unchanged because they protect cleanup safety.

- [ ] **Step 4: Reduce imports to the final required set**

Use this import block:

```go
import (
	"bufio"
	"errors"
	"strings"
	"testing"
)
```

This removes `io`, the aliased Go runtime package, `sync`, and `time`, which were used only by the deleted concurrency test.

- [ ] **Step 5: Format and verify runtime tests**

Run:

```powershell
gofmt -w internal/vm/terminal_runtime_test.go
go test ./internal/vm -run 'TestTerminal|TestSpawnedVMsShareTerminalRuntime|TestInterpret' -count=1
```

Expected: PASS; the remaining tests cover terminal availability, lifecycle, key normalization, VM sharing, and cleanup.

- [ ] **Step 6: Confirm implementation-coupled helpers are gone**

Run:

```powershell
rg -n 'gatedTerminalReader|waitForQueuedTerminalRead|TestTerminalCloseMakesQueuedReadFailAfterActiveReadFinishes|TestInterpretReportsRestoreErrorWhenExecutionSucceeds|TestInterpretPreservesRuntimeErrorWhenRestoreAlsoFails' internal/vm/terminal_runtime_test.go
```

Expected: no matches; `rg` exits with status 1.

- [ ] **Step 7: Commit the runtime test reduction**

```powershell
git add -- internal/vm/terminal_runtime_test.go
git commit -m "test: simplify terminal runtime coverage"
```

---

### Task 2: Trim duplicated builtin and exit edge cases

**Files:**
- Modify: `internal/vm/builtins_terminal_test.go`
- Modify: `internal/vm/builtins_sys_test.go`
- Reference: `internal/vm/builtins_terminal.go`
- Reference: `internal/vm/builtins_sys.go`

**Interfaces:**
- Consumes: builtin names `terminal_is_terminal`, `terminal_open_raw`, `terminal_read_key`, `terminal_close`, and `sys_exit` through the existing `callBuiltin` test helper.
- Produces: one public terminal happy path, one inactive-read case, one non-terminal failure case, and one successful `sys.exit` cleanup case.

- [ ] **Step 1: Establish the builtin baseline**

Run:

```powershell
go test ./internal/vm -run 'TestTerminalBuiltins|TestSystemExit' -count=1
```

Expected: PASS before test reduction.

- [ ] **Step 2: Keep only distinct public builtin cases**

In `TestTerminalBuiltins`, keep these subtests unchanged:

```go
t.Run("reports terminal availability and successful raw session", func(t *testing.T) {
	machine := New()
	machine.shared.Terminal = newTestTerminalRuntime(&fakeTerminalDriver{terminal: true}, "A")
	resultDefinition := testTerminalResultDefinition()
	eventDefinition := testKeyEventDefinition()

	assertBuiltinValue(t, callBuiltin(t, machine, "terminal_is_terminal"), value.NewBool(true))
	assertTerminalResult(t, callBuiltin(t, machine, "terminal_open_raw", resultDefinition), resultDefinition, true, "")
	assertKeyEvent(t, callBuiltin(t, machine, "terminal_read_key", eventDefinition), eventDefinition, true, "a", "")
	assertBuiltinValue(t, callBuiltin(t, machine, "terminal_close"), value.NewBool(true))
})

t.Run("returns inactive read fields", func(t *testing.T) {
	machine := New()
	machine.shared.Terminal = newTestTerminalRuntime(&fakeTerminalDriver{terminal: true}, "A")
	eventDefinition := testKeyEventDefinition()

	assertKeyEvent(t, callBuiltin(t, machine, "terminal_read_key", eventDefinition), eventDefinition, false, "", "terminal is not in raw mode")
})

t.Run("returns operational failure fields", func(t *testing.T) {
	machine := New()
	machine.shared.Terminal = newTestTerminalRuntime(&fakeTerminalDriver{}, "")
	resultDefinition := testTerminalResultDefinition()

	assertTerminalResult(t, callBuiltin(t, machine, "terminal_open_raw", resultDefinition), resultDefinition, false, "standard input is not a terminal")
})
```

Delete the subtests named `returns read error fields`, `returns false on close failure`, `returns null for malformed internal calls`, and `returns null for typed nil struct definitions`.

- [ ] **Step 3: Remove the now-unused builtin test import**

Use this import block in `internal/vm/builtins_terminal_test.go`:

```go
import (
	"testing"

	"noxy-vm/internal/value"
)
```

- [ ] **Step 4: Retain only successful `sys.exit` cleanup**

Delete this test in full from `internal/vm/builtins_sys_test.go`:

```go
func TestSystemExitContinuesWhenTerminalRestoreFails(t *testing.T)
```

Remove `"errors"` from that file's imports. Keep `TestSystemExitFromChildVMRestoresSharedTerminalBeforeExiting` unchanged.

- [ ] **Step 5: Format and verify builtin tests**

Run:

```powershell
gofmt -w internal/vm/builtins_terminal_test.go internal/vm/builtins_sys_test.go
go test ./internal/vm -run 'TestTerminalBuiltins|TestSystemExit' -count=1
```

Expected: PASS; the tests still prove the public happy path, user-facing operational failures, and exit cleanup.

- [ ] **Step 6: Commit the builtin test reduction**

```powershell
git add -- internal/vm/builtins_terminal_test.go internal/vm/builtins_sys_test.go
git commit -m "test: trim experimental terminal edge cases"
```

---

### Task 3: Verify retained contracts and full regression suite

**Files:**
- Verify: `internal/compiler/module_exports_test.go`
- Verify: `internal/vm/builtins_registry_test.go`
- Verify: `internal/vm/builtins_sys_test.go`
- Verify: `internal/vm/builtins_terminal_test.go`
- Verify: `internal/vm/terminal_runtime_test.go`

**Interfaces:**
- Consumes: the reduced test suite from Tasks 1 and 2.
- Produces: verification evidence that compiler exports, builtin registration, cleanup safety, and existing Noxy examples still work.

- [ ] **Step 1: Verify the retained compiler and registry contracts**

Run:

```powershell
go test ./internal/compiler -run TestEmbeddedModuleTerminalCompilesWithTypedExports -count=1
go test ./internal/vm -run 'TestBuiltinRegistrySnapshot|TestBuiltinNativeSignatures' -count=1
```

Expected: both commands PASS.

- [ ] **Step 2: Verify no VM test references the game**

Run:

```powershell
rg -n 'SpaceInvaders|space_invaders' internal/vm --glob '*_test.go'
```

Expected: no matches; `rg` exits with status 1.

- [ ] **Step 3: Run all internal Go tests**

Run:

```powershell
go test ./internal/...
```

Expected: PASS with zero failing packages.

- [ ] **Step 4: Run the Noxy integration suite**

Run:

```powershell
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: the report ends with zero failures and `TODOS TESTES PASSARAM.`

- [ ] **Step 5: Confirm only approved test files changed during implementation**

Run:

```powershell
$terminalPlanCommit = git log -1 --format=%H -- docs/superpowers/plans/2026-08-10-experimental-terminal-test-scope.md
git diff --name-only "$terminalPlanCommit..HEAD"
```

Expected:

```text
internal/vm/builtins_sys_test.go
internal/vm/builtins_terminal_test.go
internal/vm/terminal_runtime_test.go
```

No production, stdlib, example, or language documentation file may appear.
