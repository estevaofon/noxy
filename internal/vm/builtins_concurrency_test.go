package vm

import (
	"io"
	"os"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

const statefulBuiltinTimeout = 2 * time.Second

func captureConcurrencyStdout(t *testing.T, operation func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = previous }()

	operation()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func awaitBuiltinResult(t *testing.T, result <-chan value.Value, operation string) value.Value {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(statefulBuiltinTimeout):
		t.Fatalf("%s did not complete within %s", operation, statefulBuiltinTimeout)
		return value.NewNull()
	}
}

func TestChannelBuiltinsBufferedAndUnbufferedLifecycle(t *testing.T) {
	machine := New()

	buffered := callBuiltin(t, machine, "make_chan", value.NewInt(1))
	if buffered.Type != value.VAL_CHANNEL {
		t.Fatalf("make_chan type = %v, want channel", buffered.Type)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_is_closed", buffered), value.NewBool(false))
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_send", buffered, value.NewInt(7)), value.NewInt(7))
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_recv", buffered), value.NewInt(7))
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_close", buffered), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_close", buffered), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_is_closed", buffered), value.NewBool(true))
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_recv", buffered), value.NewNull())

	unbuffered := callBuiltin(t, machine, "make_chan", value.NewInt(0))
	send := requireBuiltin(t, machine, "chan_send")
	receive := requireBuiltin(t, machine, "chan_recv")
	sendResult := make(chan value.Value, 1)
	receiveResult := make(chan value.Value, 1)
	go func() {
		sendResult <- send.Fn([]value.Value{unbuffered, value.NewInt(42)})
	}()
	go func() {
		receiveResult <- receive.Fn([]value.Value{unbuffered})
	}()
	assertBuiltinValue(t, awaitBuiltinResult(t, receiveResult, "unbuffered receive"), value.NewInt(42))
	assertBuiltinValue(t, awaitBuiltinResult(t, sendResult, "unbuffered send"), value.NewInt(42))
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_close", unbuffered), value.NewNull())

	assertBuiltinValue(t, callBuiltin(t, machine, "chan_send", value.NewInt(1), value.NewInt(2)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_recv", value.NewInt(1)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "chan_is_closed", value.NewInt(1)), value.NewBool(false))
}

func TestWaitGroupBuiltinsCompleteWithinBound(t *testing.T) {
	machine := New()
	waitGroup := callBuiltin(t, machine, "make_wg")
	if waitGroup.Type != value.VAL_WAITGROUP {
		t.Fatalf("make_wg type = %v, want wait group", waitGroup.Type)
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "wg_add", waitGroup, value.NewInt(1)), value.NewNull())

	wait := requireBuiltin(t, machine, "wg_wait")
	waitResult := make(chan value.Value, 1)
	go func() {
		waitResult <- wait.Fn([]value.Value{waitGroup})
	}()
	assertBuiltinValue(t, callBuiltin(t, machine, "wg_done", waitGroup), value.NewNull())
	assertBuiltinValue(t, awaitBuiltinResult(t, waitResult, "wait group wait"), value.NewNull())

	assertBuiltinValue(t, callBuiltin(t, machine, "wg_add", waitGroup, value.NewInt(0)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "wg_wait", value.NewNull()), value.NewNull())
}

func TestSpawnWorkerSendsThroughChannelWithoutSleeping(t *testing.T) {
	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	code := compileVMSource(t, `
func worker(channel: any)
    chan_send(channel, 42)
end

let channel: any = make_chan(0)
spawn(worker, channel)
let received: any = chan_recv(channel)
test_report(to_int(received))
`)

	completed := make(chan error, 1)
	go func() {
		completed <- machine.Interpret(code)
	}()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatalf("vm error: %v", err)
		}
	case <-time.After(statefulBuiltinTimeout):
		t.Fatalf("spawn program did not complete within %s", statefulBuiltinTimeout)
	}
	assertBuiltinValue(t, captured, value.NewInt(42))
}

func TestSpawnUsesCallingVMConfig(t *testing.T) {
	parent := NewWithConfig(VMConfig{RootPath: "parent"})
	child := NewWithShared(parent.shared, VMConfig{RootPath: "child"})
	parent.DefineContextualNative("active_root", func(context value.NativeContext, _ []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewString(machine.Config.RootPath), nil
	})
	captured := value.NewNull()
	parent.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	code := compileVMSource(t, `
func worker(channel: any)
    chan_send(channel, active_root())
end
let channel: any = make_chan(0)
spawn(worker, channel)
test_report(chan_recv(channel))
`)
	completed := make(chan error, 1)
	go func() { completed <- child.Interpret(code) }()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(statefulBuiltinTimeout):
		t.Fatal("spawn did not complete")
	}
	assertBuiltinValue(t, captured, value.NewString("child"))
}

func TestSpawnPreservesLegacyDiagnosticsOnStdout(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func worker(item: int)
end`); err != nil {
		t.Fatal(err)
	}
	worker, _ := machine.GetGlobal("worker")
	spawn := requireBuiltin(t, machine, "spawn")

	tests := []struct {
		name string
		args []value.Value
		want string
	}{
		{name: "non-function", args: []value.Value{value.NewInt(1)}, want: "Runtime Error: spawn expects a function\n"},
		{name: "arity", args: []value.Value{worker}, want: "Runtime Error: spawn expected 1 args, got 0\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := captureConcurrencyStdout(t, func() {
				result, err := spawn.Invoke(machine, test.args)
				if err != nil {
					t.Fatal(err)
				}
				assertBuiltinValue(t, result, value.NewNull())
			})
			if got != test.want {
				t.Fatalf("stdout = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSpawnLaunchesDetachedWorkerWithoutSynchronousTypeValidation(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func worker(item: int, channel: any)
    chan_send(channel, item)
end`); err != nil {
		t.Fatal(err)
	}
	worker, _ := machine.GetGlobal("worker")
	channel := value.NewChannel(1)

	result, err := requireBuiltin(t, machine, "spawn").Invoke(machine, []value.Value{
		worker, value.NewString("legacy"), channel,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertBuiltinValue(t, result, value.NewNull())
	select {
	case got := <-channel.Obj.(*value.ObjChannel).Chan:
		assertBuiltinValue(t, got, value.NewString("legacy"))
	case <-time.After(statefulBuiltinTimeout):
		t.Fatal("legacy spawn did not launch worker with a runtime type mismatch")
	}
}

func TestSpawnPassesMutableArgumentsWithoutCopying(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
func worker(item: any, channel: any)
    chan_send(channel, item)
end`); err != nil {
		t.Fatal(err)
	}
	worker, _ := machine.GetGlobal("worker")
	channel := value.NewChannel(1)
	argument := value.NewArray([]value.Value{value.NewInt(1)})

	if _, err := requireBuiltin(t, machine, "spawn").Invoke(machine, []value.Value{worker, argument, channel}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-channel.Obj.(*value.ObjChannel).Chan:
		if got.Obj != argument.Obj {
			t.Fatal("legacy spawn copied a mutable argument before launching the worker")
		}
	case <-time.After(statefulBuiltinTimeout):
		t.Fatal("legacy spawn worker did not return its argument")
	}
}
