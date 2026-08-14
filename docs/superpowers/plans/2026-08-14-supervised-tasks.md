# Supervised Tasks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add supervised Noxy tasks with opaque handles, repeatable result/error waits, non-terminal timeouts, structured worker failures, and panic recovery while preserving detached `spawn`.

**Architecture:** `VAL_TASK` wraps a self-contained `ObjTask` whose private terminal outcome is published once and broadcast by closing `done`. A prepared-call boundary validates and copies arguments synchronously, while an explicit execution boundary receives the terminal return value from `run` without scraping the child stack. `spawn_task` publishes success, typed runtime failure, or recovered panic; `task_await` renders a fresh envelope and uses completion-biased timeout checks.

**Tech Stack:** Go, Noxy bytecode VM, goroutines/channels, `sync.Once`, `time.Timer`, Go tests, and Noxy executable examples.

## Global Constraints

- Preserve all observable behavior of detached `spawn`, including immediate `null`, diagnostic text/destination, and non-propagation.
- Add no syntax, opcode, public source-level `task` type, cancellation, restart strategy, or task registry.
- `spawn_task` accepts only Noxy functions/closures and validates callable, arity, runtime argument types, and `ref` modes before starting a goroutine.
- Non-`ref` task arguments use normal top-level shallow-copy semantics; `ref` arguments preserve reference identity.
- Runtime-error stacks are captured at `runtimeError`, before frames can be unwound or cleared.
- A terminal outcome is stored before `done` closes and is replayable to sequential and concurrent waiters.
- Each wait returns a new envelope while its successful `value` preserves Noxy identity.
- Timeout is local to one wait, never cancels or consumes the task, and completion wins if observable before the wait returns.
- Run every production change through RED, GREEN, and focused regression tests before committing.

## File Structure

- Create `internal/value/task.go` and `task_test.go` for the opaque handle and single-publication state.
- Create `internal/vm/runtime_errors.go` for failure-point Noxy stack capture.
- Create `internal/vm/task_execution.go` and `task_execution_test.go` for call preparation and explicit results.
- Create `internal/vm/builtins_tasks.go` and `builtins_tasks_test.go` for launch, wait, envelopes, panic recovery, and timeout.
- Modify value/VM switches, builtin registration, executor `run` callers, registry tests, and detached-spawn characterization tests.
- Update `docs/concurrency.md`, `docs/NOXY_LANGUAGE_SPEC.md`, and add `noxy_examples/supervised_tasks.nx`.

---

### Task 1: Opaque task value and single-publication primitive

**Files:**
- Create: `internal/value/task.go`
- Create: `internal/value/task_test.go`
- Modify: `internal/value/value.go`
- Modify: `internal/vm/stack.go`
- Modify: `internal/vm/call_validation.go`
- Modify: `internal/vm/builtins_core.go`
- Modify: `internal/vm/references.go`
- Modify: `internal/vm/builtins_core_test.go`
- Modify: `internal/vm/builtins_json_test.go`
- Modify: `internal/vm/malformed_reference_test.go`

**Interfaces:**
- Produces: `value.NewTask() value.Value`
- Produces: `(*value.ObjTask).Done() <-chan struct{}`
- Produces: `(*value.ObjTask).Complete(value.TaskOutcome)`
- Produces: `(*value.ObjTask).Outcome() value.TaskOutcome`
- Produces: `value.TaskOutcome{Value value.Value, Failure *value.TaskFailure}`
- Produces: `value.TaskFailure{Kind, Message, Stack string; Cause error}`

- [ ] **Step 1: Write failing task-value tests**

```go
func TestTaskPublishesFirstOutcomeToEveryWaiter(t *testing.T) {
    handle := NewTask()
    task := handle.Obj.(*ObjTask)
    seen := make(chan Value, 4)
    for range 4 {
        go func() {
            <-task.Done()
            seen <- task.Outcome().Value
        }()
    }
    task.Complete(TaskOutcome{Value: NewInt(42)})
    task.Complete(TaskOutcome{Value: NewInt(99)})
    for range 4 {
        got := <-seen
        if got.Type != VAL_INT || got.AsInt != 42 {
            t.Fatalf("outcome = %v, want 42", got)
        }
    }
}

func TestTaskHandleIsOpaqueAndStable(t *testing.T) {
    handle := NewTask()
    if handle.Type != VAL_TASK || handle.String() != "<task>" {
        t.Fatalf("handle = %s (%v)", handle.String(), handle.Type)
    }
}
```

In VM tests, assert `valuesEqual(handle, handle)` is true while two separately
created tasks are unequal, `fmt("%T", handle)` produces `"task"`, strict JSON
returns `errJSONUnsupported`, and `validateReferencedValue` rejects a
`VAL_TASK` with a nil or wrongly typed payload.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/value -run TestTask -count=1`

Expected: compilation fails because the task types and `VAL_TASK` do not exist.

- [ ] **Step 3: Implement the minimal primitive**

Append `VAL_TASK` after existing tags. Create `internal/value/task.go`:

```go
type TaskFailure struct {
    Kind, Message, Stack string
    Cause                error
}
type TaskOutcome struct {
    Value   Value
    Failure *TaskFailure
}
type ObjTask struct {
    done chan struct{}
    once sync.Once
    mu sync.RWMutex
    outcome TaskOutcome
}
func NewTask() Value {
    return Value{Type: VAL_TASK, Obj: &ObjTask{done: make(chan struct{})}}
}
func (task *ObjTask) Done() <-chan struct{} { return task.done }
func (task *ObjTask) Complete(outcome TaskOutcome) {
    task.once.Do(func() {
        task.mu.Lock()
        task.outcome = outcome
        task.mu.Unlock()
        close(task.done)
    })
}
func (task *ObjTask) Outcome() TaskOutcome {
    task.mu.RLock()
    defer task.mu.RUnlock()
    return task.outcome
}
func (*ObjTask) String() string { return "<task>" }
```

Handle `VAL_TASK` in `Value.String`, `valuesEqual` (object identity), `runtimeValueMode` (`"task"`), `%T` (`"task"`), and `validateReferencedValue` (non-nil payload). Strict JSON continues rejecting the new tag through its default unsupported branch.

- [ ] **Step 4: Run focused and race tests and verify GREEN**

Run:

```bash
go test ./internal/value ./internal/vm -run 'TestTask|TestMalformedReference' -count=1
go test -race ./internal/value -run TestTaskPublishesFirstOutcomeToEveryWaiter -count=1
```

Expected: PASS without race reports.

- [ ] **Step 5: Commit**

```bash
git add internal/value/task.go internal/value/task_test.go internal/value/value.go internal/vm/stack.go internal/vm/call_validation.go internal/vm/builtins_core.go internal/vm/references.go internal/vm/builtins_core_test.go internal/vm/builtins_json_test.go internal/vm/malformed_reference_test.go
git commit -m "feat(value): add opaque task handles"
```

---

### Task 2: Failure-point stack capture and explicit closure results

**Files:**
- Create: `internal/vm/runtime_errors.go`
- Create: `internal/vm/task_execution.go`
- Create: `internal/vm/task_execution_test.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/executor.go`
- Modify: `internal/vm/modules.go`
- Modify: `internal/vm/builtins_concurrency.go`

**Interfaces:**
- Produces: `RuntimeError{Rendered, Stack string}` implementing `error`.
- Produces: `preparedTaskCall{Callable value.Value, Closure *value.ObjClosure, Arguments []value.Value}`.
- Produces: `(*VM).prepareTaskCall(value.Value, []value.Value) (preparedTaskCall, error)`.
- Produces: `(*VM).executePreparedTaskCall(preparedTaskCall) (value.Value, error)`.
- Changes: `(*VM).run(int, *value.Value) error`; a non-nil sink receives the terminal return before cleanup.

- [ ] **Step 1: Write failing execution-boundary tests**

```go
func TestPreparedTaskCallReturnsExplicitResult(t *testing.T) {
    machine := New()
    if err := interpretVMSource(t, machine, `
func worker(value: int) -> int
    return value * 2
end`); err != nil {
        t.Fatal(err)
    }
    callable, _ := machine.GetGlobal("worker")
    call, err := machine.prepareTaskCall(callable, []value.Value{value.NewInt(21)})
    if err != nil {
        t.Fatal(err)
    }
    child := NewWithShared(machine.shared, machine.Config)
    got, err := child.executePreparedTaskCall(call)
    if err != nil || got.Type != value.VAL_INT || got.AsInt != 42 {
        t.Fatalf("result=%v err=%v", got, err)
    }
}

func TestRuntimeErrorCapturesNoxyStackAtFailurePoint(t *testing.T) {
    machine := New()
    err := interpretVMSource(t, machine, `
func inner() -> int
    return 1 / 0
end
func outer() -> int
    return inner()
end
outer()`)
    var runtimeErr *RuntimeError
    if !errors.As(err, &runtimeErr) {
        t.Fatalf("error type = %T, want *RuntimeError", err)
    }
    if !strings.Contains(runtimeErr.Stack, "in inner") ||
        !strings.Contains(runtimeErr.Stack, "in outer") {
        t.Fatalf("stack = %q", runtimeErr.Stack)
    }
}
```

Add focused tests proving a normal array argument gets a different outer `ObjArray` with shared nested identity, a `ref` argument retains its `ObjRef` pointer, wrong arity/mode/type fails synchronously, and the child uses the caller configuration/environment.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/vm -run 'TestPreparedTaskCall|TestRuntimeErrorCaptures' -count=1`

Expected: compilation fails because the new boundary and error type do not exist.

- [ ] **Step 3: Capture a typed stack inside `runtimeError`**

Implement `runtime_errors.go`:

```go
type RuntimeError struct {
    Rendered string
    Stack    string
}
func (failure *RuntimeError) Error() string { return failure.Rendered }

func (vm *VM) newRuntimeError(c *chunk.Chunk, ip int, message string) error {
    file, line := runtimeLocation(c, ip)
    return &RuntimeError{
        Rendered: fmt.Sprintf("[%s:line %d] %s", file, line, message),
        Stack:    vm.captureNoxyStack(c, ip),
    }
}
```

`captureNoxyStack` walks live frames newest-to-oldest, uses supplied `c`/`ip` for the active frame and saved `frame.IP` for callers, and emits `[%s:line %d] in %s`. Change the existing `VM.runtimeError` body to call `newRuntimeError`; preserve its rendered text exactly.

- [ ] **Step 4: Implement preparation and explicit result delivery**

`prepareTaskCall` normalizes `ObjFunction` into `ObjClosure`, rejects natives and malformed values, checks arity and `validateParameterModes`, validates complete `RuntimeType` parameters with `runtimeValueMatchesType`, and copies only non-`ref` arguments.

Change `run` to accept `terminalResult *value.Value`. In terminal `OP_RETURN`, assign `result` to a non-nil sink before stack cleanup. Pass `nil` from `InterpretWithEnvironment`, module execution, and detached `spawn`.

```go
func (vm *VM) executePreparedTaskCall(call preparedTaskCall) (value.Value, error) {
    result := value.NewNull()
    vm.push(call.Callable)
    for _, argument := range call.Arguments {
        vm.push(argument)
    }
    frame := &CallFrame{
        Closure: call.Closure, IP: 0, Slots: 0,
        Environment: call.Closure.Environment,
    }
    vm.frames[0], vm.frameCount, vm.currentFrame = frame, 1, frame
    if err := vm.run(1, &result); err != nil {
        return value.NewNull(), err
    }
    return result, nil
}
```

- [ ] **Step 5: Run focused and regression tests and verify GREEN**

Run:

```bash
go test ./internal/vm -run 'TestPreparedTaskCall|TestRuntimeErrorCaptures|TestSpawn' -count=1
go test ./internal/... -count=1
```

Expected: PASS with unchanged legacy error strings.

- [ ] **Step 6: Commit**

```bash
git add internal/vm/runtime_errors.go internal/vm/task_execution.go internal/vm/task_execution_test.go internal/vm/vm.go internal/vm/executor.go internal/vm/modules.go internal/vm/builtins_concurrency.go
git commit -m "refactor(vm): add task execution boundary"
```

---

### Task 3: Supervised completion, failure replay, and panic recovery

**Files:**
- Create: `internal/vm/builtins_tasks.go`
- Create: `internal/vm/builtins_tasks_test.go`
- Modify: `internal/vm/builtins_concurrency.go`
- Modify: `internal/vm/builtins_registry_test.go`

**Interfaces:**
- Produces builtin: `spawn_task(function, ...arguments) -> any`.
- Produces initial builtin: `task_await(handle) -> map[string, any]`.
- Consumes: `preparedTaskCall`, `executePreparedTaskCall`, `value.ObjTask`, and `RuntimeError`.

- [ ] **Step 1: Write failing public behavior tests**

```go
func TestSpawnTaskReplaysSuccessfulResult(t *testing.T) {
    got := captureVMSource(t, `
func worker(value: int) -> int
    return value * 2
end
let task: any = spawn_task(worker, 21)
let first: any = task_await(task)
let second: any = task_await(task)
test_report(first["status"] == "ok" && first["value"] == 42 &&
    first["error"] == null && second["value"] == 42)`)
    assertBuiltinValue(t, got, value.NewBool(true))
}

func TestSpawnTaskReplaysRuntimeFailure(t *testing.T) {
    got := captureVMSource(t, `
func worker() -> int
    return 1 / 0
end
let task: any = spawn_task(worker)
let first: any = task_await(task)
let second: any = task_await(task)
test_report(first["status"] == "error" && first["value"] == null &&
    first["error"]["kind"] == "runtime" &&
    contains(first["error"]["message"], "division by zero") &&
    first["error"]["stack"] == second["error"]["stack"])`)
    assertBuiltinValue(t, got, value.NewBool(true))
}
```

Register a `panic_now` native, spawn a Noxy worker that calls it, then assert both waits return `kind == "panic"`, message `"boom"`, non-empty stack, `value == null`, and consistent fields. Add void/null and composite-return identity cases.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/vm -run TestSpawnTask -count=1`

Expected: failure because `spawn_task` and `task_await` are not registered.

- [ ] **Step 3: Implement launch, terminal publication, and envelopes**

Register both builtins contextually from `defineConcurrencyBuiltins`. Validate/copy before creating the task. Launch a child from `NewWithShared(machine.shared, machine.Config)`.

```go
func (vm *VM) startSupervisedTask(task *value.ObjTask, call preparedTaskCall) {
    defer func() {
        if recovered := recover(); recovered != nil {
            task.Complete(value.TaskOutcome{Failure: &value.TaskFailure{
                Kind: "panic", Message: fmt.Sprint(recovered),
                Stack: string(debug.Stack()),
            }})
        }
    }()
    result, err := vm.executePreparedTaskCall(call)
    if err != nil {
        stack := ""
        var runtimeErr *RuntimeError
        if errors.As(err, &runtimeErr) {
            stack = runtimeErr.Stack
        }
        task.Complete(value.TaskOutcome{Failure: &value.TaskFailure{
            Kind: "runtime", Message: err.Error(), Stack: stack, Cause: err,
        }})
        return
    }
    task.Complete(value.TaskOutcome{Value: result})
}
```

The initial `task_await` accepts exactly one valid `VAL_TASK`, blocks on `Done()`, and renders a fresh `value.NewMapWithData` envelope. Success sets `error: null`; failure sets `value: null` and creates a fresh nested `{kind,message,stack}` map.

- [ ] **Step 4: Update builtin registry guarantees**

Insert `spawn_task` and `task_await` into the sorted snapshot and the contextual-handler list. Assert `Contextual != nil`, `Fn == nil`, and `IsCallable()`.

- [ ] **Step 5: Run focused and race tests and verify GREEN**

Run:

```bash
go test ./internal/vm -run 'TestSpawnTask|TestBuiltinRegistry|TestStatefulBuiltins' -count=1
go test -race ./internal/vm -run TestSpawnTask -count=1
```

Expected: PASS; panic becomes data and does not escape the worker goroutine.

- [ ] **Step 6: Commit**

```bash
git add internal/vm/builtins_tasks.go internal/vm/builtins_tasks_test.go internal/vm/builtins_concurrency.go internal/vm/builtins_registry_test.go
git commit -m "feat(vm): add supervised task outcomes"
```

---

### Task 4: Deterministic non-terminal timeout and concurrent waits

**Files:**
- Modify: `internal/vm/builtins_tasks.go`
- Modify: `internal/vm/builtins_tasks_test.go`

**Interfaces:**
- Extends builtin: `task_await(handle, timeout_ms) -> map[string, any]`.
- Zero is an immediate poll; positive is bounded; negative/non-integer/overflow is a synchronous caller error.

- [ ] **Step 1: Write failing timeout and concurrency tests**

Use a custom native blocked on a Go channel so completion is test-controlled:

```go
func TestTaskTimeoutDoesNotConsumeLaterSuccess(t *testing.T) {
    gate := make(chan struct{})
    machine := New()
    machine.DefineNative("wait_for_gate", func([]value.Value) value.Value {
        <-gate
        return value.NewInt(42)
    })
    task := spawnCompiledTestTask(t, machine, `
func worker() -> int
    return wait_for_gate()
end`, "worker")
    timed := invokeTaskAwait(t, machine, task, value.NewInt(0))
    requireTaskEnvelope(t, timed, "timeout", value.NewNull(), value.NewNull())
    close(gate)
    requireTaskOK(t, invokeTaskAwait(t, machine, task), value.NewInt(42))
}
```

Add positive timeout, completed zero poll, negative/non-integer/overflow timeout, wrong arity, invalid and malformed handles. Start 16 concurrent waits, release one gate, and assert all receive `"ok"` and the same composite result identity. Add a controlled deadline-boundary test proving the final done recheck beats timeout.

- [ ] **Step 2: Run tests and verify RED**

Run: `go test ./internal/vm -run 'TestTaskTimeout|TestTaskAwait|TestConcurrentTask' -count=1`

Expected: FAIL because timeout and completion-biased logic are absent.

- [ ] **Step 3: Implement validated completion-biased waiting**

```go
func awaitTask(task *value.ObjTask, timeout *int64) (bool, error) {
    select {
    case <-task.Done(): return true, nil
    default:
    }
    if timeout == nil {
        <-task.Done()
        return true, nil
    }
    if *timeout < 0 {
        return false, fmt.Errorf("task timeout must be non-negative")
    }
    if *timeout == 0 {
        return false, nil
    }
    if *timeout > int64(math.MaxInt64)/int64(time.Millisecond) {
        return false, fmt.Errorf("task timeout is too large")
    }
    timer := time.NewTimer(time.Duration(*timeout) * time.Millisecond)
    defer timer.Stop()
    select {
    case <-task.Done():
        return true, nil
    case <-timer.C:
        select {
        case <-task.Done(): return true, nil
        default: return false, nil
        }
    }
}
```

Parse one or two `task_await` arguments before this helper. When incomplete, return a fresh `{"status":"timeout","value":null,"error":null}` and never modify the task.

- [ ] **Step 4: Run focused and race tests and verify GREEN**

Run:

```bash
go test ./internal/vm -run 'TestTaskTimeout|TestTaskAwait|TestConcurrentTask|TestSpawnTask' -count=1
go test -race ./internal/vm -run 'TestTask|TestConcurrentTask|TestSpawnTask' -count=1
```

Expected: PASS without nondeterministic timeout or race reports.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_tasks.go internal/vm/builtins_tasks_test.go
git commit -m "feat(vm): add non-terminal task timeouts"
```

---

### Task 5: Detached compatibility, documentation, example, and full validation

**Files:**
- Modify: `internal/vm/builtins_concurrency_test.go`
- Modify: `docs/concurrency.md`
- Modify: `docs/NOXY_LANGUAGE_SPEC.md`
- Create: `noxy_examples/supervised_tasks.nx`

**Interfaces:**
- Documents both task APIs, the exact envelope table, timeout precedence, identity/replay, failure kinds, panic boundary, and detached compatibility.
- Demonstrates success, runtime error, timeout followed by success, and repeated wait.

- [ ] **Step 1: Add detached-spawn characterization before any refactor**

Cover missing/invalid callable, wrong arity, immediate `null`, and no propagation. For asynchronous error/panic diagnostics, use a child copy of the Go test process and assert captured output and successful exit:

```go
func TestSpawnRemainsDetachedOnWorkerFailure(t *testing.T) {
    command := exec.Command(os.Args[0], "-test.run=TestSpawnDetachedHelper")
    command.Env = append(os.Environ(), "NOXY_SPAWN_HELPER=runtime-error")
    output, err := command.CombinedOutput()
    if err != nil {
        t.Fatalf("helper exit: %v\n%s", err, output)
    }
    if !bytes.Contains(output, []byte("Thread Error:")) {
        t.Fatalf("output = %q", output)
    }
}
```

`TestSpawnDetachedHelper` returns immediately unless the environment variable is set. Its Noxy worker uses a test-only native/channel handshake, then errors or calls a panic native. Add a second mode asserting `Thread Panic:`. Do not use arbitrary sleeps.

- [ ] **Step 2: Run characterization and verify the baseline is GREEN**

Run: `go test ./internal/vm -run 'TestSpawnRemains|TestSpawnDetachedHelper|TestSpawnWorker|TestSpawnUses' -count=1`

Expected: PASS against legacy `spawn`. If not, adjust the characterization to the actual observable behavior before changing production code.

- [ ] **Step 3: Add the executable example**

Create `noxy_examples/supervised_tasks.nx`:

```noxy
func double(value: int) -> int
    return value * 2
end
func fail() -> int
    return 1 / 0
end
func delayed() -> string
    time_sleep(25)
    return "ready"
end

let success: any = spawn_task(double, 21)
let first: any = task_await(success)
let second: any = task_await(success)
assert(first["status"] == "ok" && first["value"] == 42, "task success")
assert(second["value"] == 42, "task replay")

let failed: any = task_await(spawn_task(fail))
assert(failed["status"] == "error", "task failure")
assert(failed["error"]["kind"] == "runtime", "runtime kind")

let slow: any = spawn_task(delayed)
let timeout: any = task_await(slow, 0)
assert(timeout["status"] == "timeout", "non-terminal timeout")
let completed: any = task_await(slow)
assert(completed["status"] == "ok" && completed["value"] == "ready", "later wait")
print("supervised tasks: ok")
```

- [ ] **Step 4: Document the normative contract**

Add “Supervised Tasks” to both documents. Copy the three-row envelope table from the design. State that completion is preferred at the deadline, timeout never cancels/consumes, envelopes are fresh, successful values preserve identity, runtime stacks are Noxy stacks, panic stacks are Go stacks, shared refs/upvalues need coordination, and `spawn` remains detached.

- [ ] **Step 5: Format and run complete validation**

```bash
gofmt -w internal/value/task.go internal/value/task_test.go internal/vm/runtime_errors.go internal/vm/task_execution.go internal/vm/task_execution_test.go internal/vm/builtins_tasks.go internal/vm/builtins_tasks_test.go internal/vm/builtins_concurrency_test.go
go test ./internal/... -count=1
go test ./... -count=1
go test -race ./internal/vm -count=1
go vet ./...
go build ./...
go run cmd/noxy/main.go noxy_examples/supervised_tasks.nx
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: every Go command exits 0; the example prints `supervised tasks: ok`; the concurrent runner reports all included examples passing.

- [ ] **Step 6: Inspect and commit the completed feature**

Run: `git diff --check && git status --short && git diff --stat`

Expected: no whitespace errors and only planned files changed.

```bash
git add internal docs/concurrency.md docs/NOXY_LANGUAGE_SPEC.md noxy_examples/supervised_tasks.nx
git commit -m "feat(vm): document supervised tasks"
```
