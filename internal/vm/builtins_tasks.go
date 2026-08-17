package vm

import (
	"fmt"
	"math"
	"runtime/debug"
	"time"

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
		if len(args) < 1 || len(args) > 2 {
			return value.NewNull(), fmt.Errorf("task_await expects 1 or 2 arguments, got %d", len(args))
		}
		if args[0].Type != value.VAL_TASK {
			return value.NewNull(), fmt.Errorf("task_await expects a task handle")
		}
		task, ok := args[0].Obj.(*value.ObjTask)
		if !ok || !task.IsValid() {
			return value.NewNull(), fmt.Errorf("task_await received a malformed task handle")
		}

		var timeout *int64
		if len(args) == 2 {
			if args[1].Type != value.VAL_INT {
				return value.NewNull(), fmt.Errorf("task_await timeout must be an integer")
			}
			timeoutValue := args[1].AsInt
			timeout = &timeoutValue
		}

		completed, err := awaitTask(task, timeout)
		if err != nil {
			return value.NewNull(), err
		}
		if !completed {
			return taskTimeoutEnvelope(), nil
		}
		return taskOutcomeEnvelope(task.Outcome()), nil
	})
}

func awaitTask(task *value.ObjTask, timeout *int64) (bool, error) {
	if timeout != nil {
		if *timeout < 0 {
			return false, fmt.Errorf("task timeout must be non-negative")
		}
		if *timeout > int64(math.MaxInt64)/int64(time.Millisecond) {
			return false, fmt.Errorf("task timeout is too large")
		}
	}

	select {
	case <-task.Done():
		return true, nil
	default:
	}

	if timeout == nil {
		<-task.Done()
		return true, nil
	}
	if *timeout == 0 {
		return false, nil
	}

	timer := time.NewTimer(time.Duration(*timeout) * time.Millisecond)
	defer stopAndDrainTaskTimer(timer)
	return awaitTaskUntilDeadline(task, timer.C), nil
}

func awaitTaskUntilDeadline(task *value.ObjTask, deadline <-chan time.Time) bool {
	select {
	case <-task.Done():
		return true
	case <-deadline:
		select {
		case <-task.Done():
			return true
		default:
			return false
		}
	}
}

func stopAndDrainTaskTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func (vm *VM) startSupervisedTask(task *value.ObjTask, call preparedTaskCall) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// RC: espelha o release de invokePreparedCall (defer.go) — a
			// captura feita em prepareTaskCall sai de escopo aqui tambem no
			// caminho de panico, antes de sinalizar conclusao da task.
			vm.releasePreparedArguments(call.Arguments, call.Closure.Function.Params)
			task.Complete(value.TaskOutcome{Failure: &value.TaskFailure{
				Kind:    "panic",
				Message: fmt.Sprint(recovered),
				Stack:   string(debug.Stack()),
			}})
		}
	}()

	result, err := vm.executePreparedTaskCall(call)
	// RC: solta a retencao de captura de prepareTaskCall assim que a
	// execucao termina (sucesso ou erro) — espelha o release pos-invocacao
	// de invokePreparedCall (defer.go). Chamado antes de task.Complete para
	// que quem sincroniza via task_await ja veja o release feito.
	vm.releasePreparedArguments(call.Arguments, call.Closure.Function.Params)
	if err != nil {
		task.Complete(value.TaskOutcome{Failure: &value.TaskFailure{
			Kind:    "runtime",
			Message: err.Error(),
			Stack:   deepestRuntimeStack(err),
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

func taskTimeoutEnvelope() value.Value {
	return value.NewMapWithData(map[string]value.Value{
		"status": value.NewString("timeout"),
		"value":  value.NewNull(),
		"error":  value.NewNull(),
	})
}
