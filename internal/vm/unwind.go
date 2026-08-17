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
	// passam ambos por aqui). Libera o OBJETO GRAVADO em cada entrada —
	// nunca o ocupante atual do slot, que apos reuso de slot por um
	// temporario nunca retido (locais de bloco mortos sem drop) poderia ser
	// um valor diferente do que foi retido. Sites de sobrescrita ja liberam
	// o velho e atualizam a entrada (ownSlot); nao ha guard de stackTop
	// porque o release agora e por objeto, nao por leitura de vm.stack.
	for _, entry := range frame.Owned {
		value.Release(entry.obj)
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
