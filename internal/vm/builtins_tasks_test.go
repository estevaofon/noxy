package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

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

func TestSpawnTaskReplaysCompositeIdentityInFreshEnvelopes(t *testing.T) {
	got := captureVMSource(t, `
func worker() -> int[]
    return [1, 2, 3]
end
let task: any = spawn_task(worker)
let first: any = task_await(task)
let second: any = task_await(task)
test_report(first != second && first["value"] == second["value"] && first["error"] == null && second["error"] == null)
`)
	assertBuiltinValue(t, got, value.NewBool(true))
}
