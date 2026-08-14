package value

import "sync"

type TaskFailure struct {
	Kind, Message, Stack string
	Cause                error
}

type TaskOutcome struct {
	Value   Value
	Failure *TaskFailure
}

type ObjTask struct {
	done    chan struct{}
	once    sync.Once
	mu      sync.RWMutex
	outcome TaskOutcome
}

func NewTask() Value {
	return Value{Type: VAL_TASK, Obj: &ObjTask{done: make(chan struct{})}}
}

func (task *ObjTask) IsValid() bool {
	return task != nil && task.done != nil
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
