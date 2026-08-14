package vm

import (
	"errors"
	"fmt"
	"runtime/debug"

	"noxy-vm/internal/value"
)

func (vm *VM) defineTaskBuiltins() {
	vm.DefineContextualNative("spawn_task", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), fmt.Errorf("spawn_task expects a function")
		}

		call, err := machine.prepareTaskCall(args[0], args[1:])
		if err != nil {
			return value.NewNull(), err
		}

		handle := value.NewTask()
		task := handle.Obj.(*value.ObjTask)
		worker := NewWithShared(machine.shared, machine.Config)
		go worker.startSupervisedTask(task, call)
		return handle, nil
	})

	vm.DefineContextualNative("task_await", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		if _, err := nativeVM(context); err != nil {
			return value.NewNull(), err
		}
		if len(args) != 1 {
			return value.NewNull(), fmt.Errorf("task_await expects exactly 1 argument, got %d", len(args))
		}
		if args[0].Type != value.VAL_TASK {
			return value.NewNull(), fmt.Errorf("task_await expects a task handle")
		}
		task, ok := args[0].Obj.(*value.ObjTask)
		if !ok || task == nil {
			return value.NewNull(), fmt.Errorf("task_await received a malformed task handle")
		}

		<-task.Done()
		return taskOutcomeEnvelope(task.Outcome()), nil
	})
}

func (vm *VM) startSupervisedTask(task *value.ObjTask, call preparedTaskCall) {
	defer func() {
		if recovered := recover(); recovered != nil {
			task.Complete(value.TaskOutcome{Failure: &value.TaskFailure{
				Kind:    "panic",
				Message: fmt.Sprint(recovered),
				Stack:   string(debug.Stack()),
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
			Kind:    "runtime",
			Message: err.Error(),
			Stack:   stack,
			Cause:   err,
		}})
		return
	}

	task.Complete(value.TaskOutcome{Value: result})
}

func taskOutcomeEnvelope(outcome value.TaskOutcome) value.Value {
	if outcome.Failure == nil {
		return value.NewMapWithData(map[string]value.Value{
			"status": value.NewString("ok"),
			"value":  outcome.Value,
			"error":  value.NewNull(),
		})
	}

	failure := outcome.Failure
	return value.NewMapWithData(map[string]value.Value{
		"status": value.NewString("error"),
		"value":  value.NewNull(),
		"error": value.NewMapWithData(map[string]value.Value{
			"kind":    value.NewString(failure.Kind),
			"message": value.NewString(failure.Message),
			"stack":   value.NewString(failure.Stack),
		}),
	})
}
