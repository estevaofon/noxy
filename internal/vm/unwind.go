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
	if vm.currentFrame == nil || vm.frameCount == 0 {
		return outcome
	}
	// Por INDICE, nao por ponteiro: invokePreparedCall reentra a VM e pode
	// realocar vm.frames (growFrames); um *CallFrame segurado atraves da
	// chamada apontaria para o array velho — a posse e o Closure seriam
	// "liberados" na copia morta e o slot novo ficaria sujo.
	index := vm.frameCount - 1
	frame := &vm.frames[index]

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
		frame = &vm.frames[index]
	}

	// RC: fecha os upvalues ANTES de soltar os vinculos do frame —
	// retain-antes-de-release mantem a contagem sem passar por zero na
	// migracao slot -> caixa. Cada caixa decide sozinha se assume a posse
	// (ela sabe, estaticamente, se empresta; ver closeUpvalue). O guard de
	// openUpvalues evita percorrer os slots no caso comum (frame sem nenhuma
	// captura aberta), que e a esmagadora maioria dos retornos.
	ownedTop := vm.stackTop
	if vm.openUpvalues != nil {
		for index := frame.StackBase; index < ownedTop; index++ {
			vm.closeUpvalue(&vm.stack[index])
		}
	}

	// RC: solta os vinculos duraveis do frame (retorno normal e unwind
	// passam ambos por aqui). Libera o OBJETO GRAVADO em cada entrada —
	// nunca o ocupante atual do slot, que apos reuso de slot por um
	// temporario nunca retido (locais de bloco mortos sem drop) poderia ser
	// um valor diferente do que foi retido. Sites de sobrescrita ja liberam
	// o velho e atualizam a entrada (ownSlot); nao ha guard de stackTop
	// porque o release agora e por objeto, nao por leitura de vm.stack.
	//
	// vm.frames e um slice de VALORES reusado entre chamadas (so realocado por
	// growFrames, em ensureCallCapacity) — este slot do slice sobrevive ao
	// frame e volta a ser escrito pela PROXIMA chamada que cair neste indice.
	// Por isso:
	//   1. cada entrada de Owned e ZERADA (nao so "esquecida" pelo truncamento
	//      abaixo) antes do truncamento: um `range frame.Owned` futuro respeita
	//      len, nao cap, entao uma entrada nao-zerada alem do novo len jamais
	//      seria re-liberada (nao e um bug de contagem) — mas continuaria
	//      nomeando, dentro do backing array que persiste, um objeto cujo RC ja
	//      caiu a zero, prendendo-o na memoria ate um append futuro por acaso
	//      sobrescrever aquele indice. E EXATAMENTE o vazamento que este
	//      desenho deve evitar.
	//   2. o truncamento e `frame.Owned = frame.Owned[:0]`, NUNCA
	//      `frame.Owned = nil` — setar nil descartaria a capacidade do slice e
	//      forcaria toda chamada seguinte a realocar via append em ownSlot,
	//      devolvendo o custo de alocacao por chamada que esta troca elimina.
	for i := range frame.Owned {
		value.Release(frame.Owned[i].obj)
		frame.Owned[i] = ownedEntry{}
	}
	frame.Owned = frame.Owned[:0]

	for index := frame.StackBase; index < ownedTop; index++ {
		vm.stack[index] = value.Value{}
	}

	// So Closure/Environment sao nil'ados aqui (nao a struct inteira): sao os
	// dois campos com referencia a heap que precisam soltar para o GC assim
	// que o frame termina; IP/StackBase/LocalBase sao escalares reescritos
	// incondicionalmente no proximo uso deste slot, e Owned/Deferred ja foram
	// esvaziados (com capacidade preservada) pelos blocos acima.
	frame.Closure = nil
	frame.Environment = nil
	vm.frameCount--
	vm.stackTop = frame.StackBase

	if vm.frameCount == 0 {
		vm.currentFrame = nil
		vm.stackTop = 0
		return outcome
	}

	vm.currentFrame = &vm.frames[vm.frameCount-1]
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
