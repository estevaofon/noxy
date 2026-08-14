package value

import "testing"

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
