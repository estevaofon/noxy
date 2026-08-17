package vm

import (
	"fmt"
	"runtime/debug"

	"noxy-vm/internal/value"
)

func (vm *VM) defineConcurrencyBuiltins() {
	vm.defineTaskBuiltins()

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
		// CoW: a exceção legada do spawn (encaminhar identidade) foi removida;
		// args compostos são valores como em qualquer outra fronteira.
		// RC: retain aqui, sincrono, antes do goroutine ser lançado — a nova
		// thread passa a ser dona durável de cada composto empurrado.
		for _, arg := range threadArgs {
			value.MarkShared(arg)
			value.Retain(arg)
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

		// RC: registra os slots retidos acima no frame manual (spawn não passa
		// por callPreparedClosure, que faria isso via frame.ownSlot — aqui é o
		// ÚNICO lugar onde retain e registro ficam separados: o retain já
		// aconteceu no loop de push; aqui só registramos o slot para que
		// finalizeCurrentFrame libere quando o frame da thread terminar. NÃO
		// usar ownSlot aqui — ele retém de novo (double-retain).
		for i := range threadArgs {
			if value.OwnersCount(threadArgs[i]) >= 0 {
				frame.Owned = append(frame.Owned, 1+i)
			}
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
			err := threadVM.run(1, nil) // Run until finished (frame 0 popped)
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
		value.MarkShared(args[1]) // CoW: canal transporta valor, não identidade
		value.Retain(args[1])     // RC: o buffer do canal é dono durável enquanto o valor está nele
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
			// canal fechado e vazio: nao ha valor recebido, entao nao ha o
			// que liberar aqui.
			return value.NewNull()
		}
		value.Release(val) // RC: o valor saiu do buffer do canal (que era dono durável)
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
