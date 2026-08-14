package vm

import (
	"fmt"
	"runtime/debug"

	"noxy-vm/internal/value"
)

func (vm *VM) defineConcurrencyBuiltins() {
	// Concurrency Primitives
	vm.DefineContextualNative("spawn", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), nil
		}
		fnVal := args[0]
		if fnVal.Type != value.VAL_FUNCTION {
			// Only script functions are supported in spawn.
			fmt.Println("Runtime Error: spawn expects a function")
			return value.NewNull(), nil
		}

		threadArgs := args[1:]

		// Create new VM thread sharing state
		threadVM := NewWithShared(machine.shared, machine.Config)

		// Setup execution
		var closure *value.ObjClosure
		if cl, ok := fnVal.Obj.(*value.ObjClosure); ok {
			closure = cl
		} else if fn, ok := fnVal.Obj.(*value.ObjFunction); ok {
			closure = &value.ObjClosure{Function: fn, Upvalues: []*value.ObjUpvalue{}, Environment: fn.Environment}
		} else {
			fmt.Println("Runtime Error: spawn expects a function or closure")
			return value.NewNull(), nil
		}

		fnObj := closure.Function

		// Check arity
		if len(threadArgs) != fnObj.Arity {
			fmt.Printf("Runtime Error: spawn expected %d args, got %d\n", fnObj.Arity, len(threadArgs))
			return value.NewNull(), nil
		}

		// Push Function (Stack slot 0)
		threadVM.push(fnVal)

		// Push Args
		for _, arg := range threadArgs {
			threadVM.push(arg)
		}

		// Create Frame
		frame := &CallFrame{
			Closure:     closure,
			IP:          0,
			StackBase:   0,
			LocalBase:   0,
			Environment: closure.Environment,
		}

		threadVM.frames[0] = frame
		threadVM.frameCount = 1
		threadVM.currentFrame = frame

		// Launch Goroutine
		go func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("Thread Panic: %v\n%s", r, debug.Stack())
				}
			}()
			err := threadVM.run(1) // Run until finished (frame 0 popped)
			if err != nil {
				fmt.Printf("Thread Error: %v\n", err)
			}
		}()

		return value.NewNull(), nil
	})

	vm.DefineNative("make_chan", func(args []value.Value) value.Value {
		size := 0
		if len(args) > 0 {
			if args[0].Type == value.VAL_INT {
				size = int(args[0].AsInt)
			}
		}
		return value.NewChannel(size)
	})

	vm.DefineNative("chan_send", func(args []value.Value) value.Value {
		if len(args) != 2 {
			return value.NewNull()
		}
		if args[0].Type != value.VAL_CHANNEL {
			return value.NewNull()
		}
		ch := args[0].Obj.(*value.ObjChannel).Chan
		ch <- args[1]
		return args[1]
	})

	vm.DefineNative("chan_close", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		if args[0].Type != value.VAL_CHANNEL {
			return value.NewNull()
		}
		chObj := args[0].Obj.(*value.ObjChannel)

		chObj.Lock.Lock()
		defer chObj.Lock.Unlock()

		if !chObj.Closed {
			close(chObj.Chan)
			chObj.Closed = true
		}
		return value.NewNull()
	})

	vm.DefineNative("chan_is_closed", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewBool(false)
		}
		if args[0].Type != value.VAL_CHANNEL {
			return value.NewBool(false)
		}
		chObj := args[0].Obj.(*value.ObjChannel)

		chObj.Lock.Lock()
		defer chObj.Lock.Unlock()

		return value.NewBool(chObj.Closed)
	})

	vm.DefineNative("chan_recv", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		if args[0].Type != value.VAL_CHANNEL {
			return value.NewNull()
		}
		ch := args[0].Obj.(*value.ObjChannel).Chan
		val, ok := <-ch
		if !ok {
			return value.NewNull()
		}
		return val
	})

	vm.DefineNative("make_wg", func(args []value.Value) value.Value {
		return value.NewWaitGroup()
	})

	vm.DefineNative("wg_add", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		if args[0].Type != value.VAL_WAITGROUP {
			return value.NewNull()
		}
		delta := int(0)
		if args[1].Type == value.VAL_INT {
			delta = int(args[1].AsInt)
		}
		if delta == 0 {
			return value.NewNull()
		}
		wg := args[0].Obj.(*value.ObjWaitGroup).Wg
		wg.Add(delta)
		return value.NewNull()
	})

	vm.DefineNative("wg_done", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		if args[0].Type != value.VAL_WAITGROUP {
			return value.NewNull()
		}
		wg := args[0].Obj.(*value.ObjWaitGroup).Wg
		wg.Done()
		return value.NewNull()
	})

	vm.DefineNative("wg_wait", func(args []value.Value) value.Value {
		if len(args) != 1 {
			return value.NewNull()
		}
		if args[0].Type != value.VAL_WAITGROUP {
			return value.NewNull()
		}
		wg := args[0].Obj.(*value.ObjWaitGroup).Wg
		wg.Wait()
		return value.NewNull()
	})

}
