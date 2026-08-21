package vm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

func spawnCompiledTestTask(t *testing.T, machine *VM, source, functionName string) value.Value {
	t.Helper()
	if err := interpretVMSource(t, machine, source); err != nil {
		t.Fatalf("compile task source: %v", err)
	}
	callable, ok := machine.GetGlobal(functionName)
	if !ok {
		t.Fatalf("task function %q is not defined", functionName)
	}
	handle, err := requireBuiltin(t, machine, "spawn_task").Invoke(machine, []value.Value{callable})
	if err != nil {
		t.Fatalf("spawn_task: %v", err)
	}
	return handle
}

func invokeTaskAwait(machine *VM, args ...value.Value) (value.Value, error) {
	builtin, ok := machine.GetGlobal("task_await")
	if !ok {
		return value.NewNull(), nil
	}
	return builtin.Obj.(*value.ObjNative).Invoke(machine, args)
}

func taskEnvelopeField(t *testing.T, envelope value.Value, name string) value.Value {
	t.Helper()
	mapping, ok := envelope.Obj.(*value.ObjMap)
	if envelope.Type != value.VAL_OBJ || !ok || mapping == nil {
		t.Fatalf("task envelope = %#v, want map", envelope)
	}
	field, ok := mapping.Get(name)
	if !ok {
		t.Fatalf("task envelope has no %q field", name)
	}
	return field
}

func requireTaskEnvelope(t *testing.T, envelope value.Value, status string, result, failure value.Value) {
	t.Helper()
	if got := taskEnvelopeField(t, envelope, "status"); !valuesEqual(got, value.NewString(status)) {
		t.Fatalf("task status = %v, want %q", got, status)
	}
	if got := taskEnvelopeField(t, envelope, "value"); !valuesEqual(got, result) {
		t.Fatalf("task value = %v, want %v", got, result)
	}
	if got := taskEnvelopeField(t, envelope, "error"); !valuesEqual(got, failure) {
		t.Fatalf("task error = %v, want %v", got, failure)
	}
}

func requireTaskOK(t *testing.T, envelope, result value.Value) {
	t.Helper()
	requireTaskEnvelope(t, envelope, "ok", result, value.NewNull())
}

func TestSpawnTaskReplaysSuccessfulResult(t *testing.T) {
	got := captureVMSource(t, `
func worker(value: int) -> int
    return value * 2
end
let task: any = spawn_task(worker, 21)
let first: any = task_await(task)
let second: any = task_await(task)
test_report(first["status"] == "ok" && first["value"] == 42 && first["error"] == null && second["value"] == 42)
`)
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
test_report(first["status"] == "error" && first["value"] == null && first["error"]["kind"] == "runtime" && strings_contains(first["error"]["message"], "division by zero") && length(first["error"]["stack"]) > 0 && first["error"]["stack"] == second["error"]["stack"])
`)
	assertBuiltinValue(t, got, value.NewBool(true))
}

func TestSpawnTaskRecoversAndReplaysNativePanic(t *testing.T) {
	machine := New()
	machine.DefineNative("panic_now", func([]value.Value) value.Value {
		panic("boom")
	})
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})

	err := interpretVMSource(t, machine, `
func worker()
    panic_now()
end
let task: any = spawn_task(worker)
let first: any = task_await(task)
let second: any = task_await(task)
test_report(first["status"] == "error" && first["value"] == null && first["error"]["kind"] == "panic" && first["error"]["message"] == "boom" && length(first["error"]["stack"]) > 0 && first["error"]["kind"] == second["error"]["kind"] && first["error"]["message"] == second["error"]["message"] && first["error"]["stack"] == second["error"]["stack"])
`)
	if err != nil {
		t.Fatalf("vm error: %v", err)
	}
	assertBuiltinValue(t, captured, value.NewBool(true))
}

func TestSpawnTaskPreservesNullAndVoidResults(t *testing.T) {
	got := captureVMSource(t, `
func void_worker() -> void
end
func null_worker() -> any
    return null
end
let void_result: any = task_await(spawn_task(void_worker))
let null_result: any = task_await(spawn_task(null_worker))
test_report(void_result["status"] == "ok" && void_result["value"] == null && void_result["error"] == null && null_result["status"] == "ok" && null_result["value"] == null && null_result["error"] == null)
`)
	assertBuiltinValue(t, got, value.NewBool(true))
}

// Contrato CoW: == de compostos é estrutural, então dois envelopes de
// task_await com o mesmo conteúdo comparam IGUAIS (antes o teste usava
// identidade de ponteiro para provar que cada await devolve envelope fresco;
// essa propriedade de mecanismo deixou de ser observável em código Noxy).
func TestSpawnTaskAwaitEnvelopesCompareStructurally(t *testing.T) {
	got := captureVMSource(t, `
func worker() -> int[]
    return [1, 2, 3]
end
let task: any = spawn_task(worker)
let first: any = task_await(task)
let second: any = task_await(task)
test_report(first == second && first["value"] == second["value"] && first["error"] == null && second["error"] == null)
`)
	assertBuiltinValue(t, got, value.NewBool(true))
}

func TestSpawnTaskSynchronizesCapturedLocalHandoff(t *testing.T) {
	got := captureVMSource(t, `
func launch(value: int) -> any
    let captured: int = value
    func worker() -> int
        return captured
    end
    return spawn_task(worker)
end

let iteration: int = 0
let valid: bool = true
while iteration < 1000 do
    let outcome: any = task_await(launch(iteration))
    if outcome["status"] != "ok" || outcome["value"] != iteration then
        valid = false
    end
    iteration = iteration + 1
end
test_report(valid)
`)
	assertBuiltinValue(t, got, value.NewBool(true))
}

func TestSpawnTaskSynchronizesCapturedReferenceHandoff(t *testing.T) {
	got := captureVMSource(t, `
func launch(value: int) -> any
    let captured: int = value
    func worker() -> int
        let pointer: ref int = ref captured
        *pointer = pointer + 1
        return captured
    end
    return spawn_task(worker)
end

let iteration: int = 0
let valid: bool = true
while iteration < 1000 do
    let outcome: any = task_await(launch(iteration))
    if outcome["status"] != "ok" || outcome["value"] != iteration + 1 then
        valid = false
    end
    iteration = iteration + 1
end
test_report(valid)
`)
	assertBuiltinValue(t, got, value.NewBool(true))
}

func TestTaskTimeoutZeroPollDoesNotConsumeLaterSuccess(t *testing.T) {
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	machine := New()
	machine.DefineNative("wait_for_gate", func([]value.Value) value.Value {
		entered <- struct{}{}
		<-gate
		return value.NewInt(42)
	})
	task := spawnCompiledTestTask(t, machine, `
func worker() -> int
    return wait_for_gate()
end`, "worker")
	<-entered

	timed, err := invokeTaskAwait(machine, task, value.NewInt(0))
	if err != nil {
		t.Fatalf("zero poll: %v", err)
	}
	requireTaskEnvelope(t, timed, "timeout", value.NewNull(), value.NewNull())

	close(gate)
	completed, err := invokeTaskAwait(machine, task)
	if err != nil {
		t.Fatalf("later wait: %v", err)
	}
	requireTaskOK(t, completed, value.NewInt(42))
}

func TestTaskTimeoutPositiveWaitIsBoundedAndNonTerminal(t *testing.T) {
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	machine := New()
	machine.DefineNative("wait_for_gate", func([]value.Value) value.Value {
		entered <- struct{}{}
		<-gate
		return value.NewInt(7)
	})
	task := spawnCompiledTestTask(t, machine, `
func worker() -> int
    return wait_for_gate()
end`, "worker")
	<-entered

	timed, err := invokeTaskAwait(machine, task, value.NewInt(1))
	if err != nil {
		t.Fatalf("positive timeout: %v", err)
	}
	requireTaskEnvelope(t, timed, "timeout", value.NewNull(), value.NewNull())

	close(gate)
	completed, err := invokeTaskAwait(machine, task, value.NewInt(1000))
	if err != nil {
		t.Fatalf("later bounded wait: %v", err)
	}
	requireTaskOK(t, completed, value.NewInt(7))
}

func TestTaskAwaitCompletedTaskWinsZeroPoll(t *testing.T) {
	machine := New()
	handle := value.NewTask()
	handle.Obj.(*value.ObjTask).Complete(value.TaskOutcome{Value: value.NewInt(9)})

	got, err := invokeTaskAwait(machine, handle, value.NewInt(0))
	if err != nil {
		t.Fatalf("completed poll: %v", err)
	}
	requireTaskOK(t, got, value.NewInt(9))
}

func TestTaskAwaitRejectsInvalidArguments(t *testing.T) {
	machine := New()
	handle := value.NewTask()
	completedHandle := value.NewTask()
	completedHandle.Obj.(*value.ObjTask).Complete(value.TaskOutcome{Value: value.NewInt(1)})
	overflow := int64((1<<63-1)/int64(time.Millisecond) + 1)
	tests := []struct {
		name string
		args []value.Value
		want string
	}{
		{name: "no arguments", want: "1 or 2 arguments"},
		{name: "too many arguments", args: []value.Value{handle, value.NewInt(0), value.NewInt(0)}, want: "1 or 2 arguments"},
		{name: "invalid handle", args: []value.Value{value.NewInt(1)}, want: "task handle"},
		{name: "malformed nil handle", args: []value.Value{{Type: value.VAL_TASK, Obj: (*value.ObjTask)(nil)}}, want: "malformed task handle"},
		{name: "malformed handle object", args: []value.Value{{Type: value.VAL_TASK, Obj: "not a task"}}, want: "malformed task handle"},
		{name: "zero-value handle", args: []value.Value{{Type: value.VAL_TASK, Obj: &value.ObjTask{}}, value.NewInt(0)}, want: "malformed task handle"},
		{name: "negative timeout", args: []value.Value{handle, value.NewInt(-1)}, want: "non-negative"},
		{name: "negative timeout on completed task", args: []value.Value{completedHandle, value.NewInt(-1)}, want: "non-negative"},
		{name: "non-integer timeout", args: []value.Value{handle, value.NewFloat(1)}, want: "integer"},
		{name: "overflow timeout", args: []value.Value{handle, value.NewInt(overflow)}, want: "too large"},
		{name: "overflow timeout on completed task", args: []value.Value{completedHandle, value.NewInt(overflow)}, want: "too large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := invokeTaskAwait(machine, test.args...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestConcurrentTaskWaitersReplayCompositeIdentity(t *testing.T) {
	const waiterCount = 16
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	composite := value.NewArray([]value.Value{value.NewInt(1), value.NewInt(2), value.NewInt(3)})
	machine := New()
	machine.DefineNative("wait_for_gate", func([]value.Value) value.Value {
		entered <- struct{}{}
		<-gate
		return composite
	})
	task := spawnCompiledTestTask(t, machine, `
func worker() -> any
    return wait_for_gate()
end`, "worker")
	<-entered

	type waitResult struct {
		envelope value.Value
		err      error
	}
	started := make(chan struct{}, waiterCount)
	results := make(chan waitResult, waiterCount)
	var waiters sync.WaitGroup
	waiters.Add(waiterCount)
	for range waiterCount {
		go func() {
			defer waiters.Done()
			started <- struct{}{}
			envelope, err := invokeTaskAwait(machine, task)
			results <- waitResult{envelope: envelope, err: err}
		}()
	}
	for range waiterCount {
		<-started
	}
	close(gate)
	waiters.Wait()
	close(results)

	seenEnvelopes := make(map[any]struct{}, waiterCount)
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent wait: %v", result.err)
		}
		requireTaskOK(t, result.envelope, composite)
		if got := taskEnvelopeField(t, result.envelope, "value"); got.Obj != composite.Obj {
			t.Fatalf("result identity = %p, want %p", got.Obj, composite.Obj)
		}
		if _, duplicate := seenEnvelopes[result.envelope.Obj]; duplicate {
			t.Fatal("concurrent waits reused an envelope")
		}
		seenEnvelopes[result.envelope.Obj] = struct{}{}
	}
}

func TestTaskAwaitCompletionWinsAtDeadlineBoundary(t *testing.T) {
	for range 256 {
		handle := value.NewTask()
		task := handle.Obj.(*value.ObjTask)
		deadline := make(chan time.Time, 1)
		deadline <- time.Time{}
		task.Complete(value.TaskOutcome{Value: value.NewInt(1)})
		if !awaitTaskUntilDeadline(task, deadline) {
			t.Fatal("ready task lost to ready deadline")
		}
	}
}

func TestSpawnTaskPublishesResultAfterDeferredCleanup(t *testing.T) {
	machine := New()
	cleanup := make(chan int64, 1)
	machine.DefineNative("task_cleanup", func(args []value.Value) value.Value {
		cleanup <- args[0].AsInt
		return value.NewNull()
	})

	task := spawnCompiledTestTask(t, machine, `
func worker() -> int
    defer task_cleanup(7)
    return 42
end`, "worker")
	envelope, err := invokeTaskAwait(machine, task)
	if err != nil {
		t.Fatalf("task_await: %v", err)
	}
	requireTaskOK(t, envelope, value.NewInt(42))

	select {
	case got := <-cleanup:
		if got != 7 {
			t.Fatalf("cleanup = %d, want 7", got)
		}
	default:
		t.Fatal("task result was published before deferred cleanup")
	}
}

func TestSpawnTaskReportsDeferredCleanupFailure(t *testing.T) {
	machine := New()
	machine.DefineContextualNative("task_cleanup_fail", func(value.NativeContext, []value.Value) (value.Value, error) {
		return value.NewNull(), errors.New("cleanup boom")
	})

	task := spawnCompiledTestTask(t, machine, `
func worker() -> int
    defer task_cleanup_fail()
    return 42
end`, "worker")
	envelope, err := invokeTaskAwait(machine, task)
	if err != nil {
		t.Fatalf("task_await: %v", err)
	}
	if got := taskEnvelopeField(t, envelope, "status"); !valuesEqual(got, value.NewString("error")) {
		t.Fatalf("status = %v, want error", got)
	}
	failure := taskEnvelopeField(t, envelope, "error")
	if got := taskEnvelopeField(t, failure, "kind"); !valuesEqual(got, value.NewString("runtime")) {
		t.Fatalf("kind = %v, want runtime", got)
	}
	message := taskEnvelopeField(t, failure, "message")
	if message.Type != value.VAL_OBJ || !strings.Contains(message.Obj.(string), "cleanup boom") {
		t.Fatalf("message = %v, want cleanup failure", message)
	}
	stack := taskEnvelopeField(t, failure, "stack")
	if stack.Type != value.VAL_OBJ || stack.Obj.(string) == "" {
		t.Fatal("deferred cleanup failure lost its Noxy stack")
	}
}

func TestSpawnTaskPreservesDeepModuleFailureStack(t *testing.T) {
	root := t.TempDir()
	moduleSource := `
func nested_failure() -> int
    return 1 / 0
end
nested_failure()
`
	if err := os.WriteFile(filepath.Join(root, "broken_task.nx"), []byte(moduleSource), 0o600); err != nil {
		t.Fatal(err)
	}

	machine := NewWithConfig(VMConfig{RootPath: root})
	task := spawnCompiledTestTask(t, machine, `
func worker() -> int
    use broken_task
    return 1
end`, "worker")
	envelope, err := invokeTaskAwait(machine, task)
	if err != nil {
		t.Fatalf("task_await: %v", err)
	}
	failure := taskEnvelopeField(t, envelope, "error")
	stack := taskEnvelopeField(t, failure, "stack")
	if stack.Type != value.VAL_OBJ {
		t.Fatalf("stack = %v, want string", stack)
	}
	stackText := stack.Obj.(string)
	if !strings.Contains(stackText, "in nested_failure") || !strings.Contains(stackText, "in worker") {
		t.Fatalf("stack = %q, want module failure and worker frames", stackText)
	}
}

func TestSpawnTaskDeferredHeadroomFailureHasNoxyStack(t *testing.T) {
	machine := New()
	machine.DefineNative("headroom_cleanup", func([]value.Value) value.Value {
		return value.NewNull()
	})
	machine.DefineContextualNative("fill_task_stack", func(context value.NativeContext, _ []value.Value) (value.Value, error) {
		worker, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		worker.installStack(make([]value.Value, StackMax))
		worker.stackTop = StackMax
		return value.NewNull(), nil
	})

	task := spawnCompiledTestTask(t, machine, `
func worker() -> void
    defer headroom_cleanup(1)
    fill_task_stack()
end`, "worker")
	envelope, err := invokeTaskAwait(machine, task)
	if err != nil {
		t.Fatalf("task_await: %v", err)
	}
	if got := taskEnvelopeField(t, envelope, "status"); !valuesEqual(got, value.NewString("error")) {
		t.Fatalf("status = %v, want error", got)
	}
	failure := taskEnvelopeField(t, envelope, "error")
	stack := taskEnvelopeField(t, failure, "stack")
	if stack.Type != value.VAL_OBJ || !strings.Contains(stack.Obj.(string), "in worker") {
		t.Fatalf("stack = %v, want worker frame", stack)
	}
}
