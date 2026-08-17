package vm

import "noxy-vm/internal/value"

type frameOutcome struct {
	Result value.Value
	Err    error
}

func (vm *VM) finishFrame(outcome frameOutcome) frameOutcome {
	return vm.finalizeCurrentFrame(outcome)
}

func (vm *VM) unwindTo(targetFrameCount int, outcome frameOutcome) frameOutcome {
	for vm.frameCount > targetFrameCount {
		outcome = vm.finalizeCurrentFrame(outcome)
	}
	return outcome
}

func (vm *VM) finalizeCurrentFrame(outcome frameOutcome) frameOutcome {
	frame := vm.currentFrame
	if frame == nil || vm.frameCount == 0 {
		return outcome
	}

	for len(frame.Deferred) > 0 {
		last := len(frame.Deferred) - 1
		call := frame.Deferred[last]
		frame.Deferred[last] = PreparedCall{}
		frame.Deferred = frame.Deferred[:last]

		if err := vm.invokePreparedCall(call); err != nil {
			outcome.Err = appendDeferredError(outcome.Err, DeferredError{
				Registration: call.Registration,
				Cause:        err,
			})
		}
	}

	// RC: solta os vinculos duraveis do frame (retorno normal e unwind
	// passam ambos por aqui). Le o ocupante ATUAL do slot: sobrescritas
	// durante a vida do frame ja fizeram seu proprio release/retain.
	for _, slot := range frame.Owned {
		if slot < vm.stackTop {
			value.Release(vm.stack[slot])
		}
	}
	frame.Owned = nil

	ownedTop := vm.stackTop
	for index := frame.StackBase; index < ownedTop; index++ {
		vm.closeUpvalue(&vm.stack[index])
		vm.stack[index] = value.Value{}
	}

	frameIndex := vm.frameCount - 1
	vm.frames[frameIndex] = nil
	vm.frameCount--
	vm.stackTop = frame.StackBase

	if vm.frameCount == 0 {
		vm.currentFrame = nil
		vm.stackTop = 0
		return outcome
	}

	vm.currentFrame = vm.frames[vm.frameCount-1]
	if outcome.Err == nil {
		vm.push(outcome.Result)
	}
	return outcome
}

func appendDeferredError(primary error, deferred DeferredError) error {
	if unwind, ok := primary.(*UnwindError); ok {
		unwind.Deferred = append(unwind.Deferred, deferred)
		return unwind
	}
	return &UnwindError{
		Primary:  primary,
		Deferred: []DeferredError{deferred},
	}
}
