package vm

import "noxy-vm/internal/value"

func (vm *VM) readShort() uint16 {
	vm.ip += 2
	return uint16(vm.chunk.Code[vm.ip-2])<<8 | uint16(vm.chunk.Code[vm.ip-1])
}

// isFalsey returns true if the value is false or null
func isFalsey(v value.Value) bool {
	return v.Type == value.VAL_NULL || (v.Type == value.VAL_BOOL && !v.AsBool)
}

func valuesEqual(a, b value.Value) bool {
	if a.Type == b.Type {
		switch a.Type {
		case value.VAL_BOOL:
			return a.AsBool == b.AsBool
		case value.VAL_NULL:
			return true
		case value.VAL_INT:
			return a.AsInt == b.AsInt
		case value.VAL_FLOAT:
			return a.AsFloat == b.AsFloat
		case value.VAL_OBJ:
			// CoW: compostos comparam estruturalmente (identidade de ponteiro
			// ficou instável sob copy-on-write). Demais objetos (strings via
			// interface, closures, canais…) mantêm a comparação direta.
			switch ao := a.Obj.(type) {
			case *value.ObjArray:
				bo, ok := b.Obj.(*value.ObjArray)
				if !ok {
					return false
				}
				if ao == bo {
					return true
				}
				if len(ao.Elements) != len(bo.Elements) {
					return false
				}
				for i := range ao.Elements {
					if !valuesEqual(ao.Elements[i], bo.Elements[i]) {
						return false
					}
				}
				return true
			case *value.ObjMap:
				bo, ok := b.Obj.(*value.ObjMap)
				if !ok {
					return false
				}
				if ao == bo {
					return true
				}
				as, bs := ao.Snapshot(), bo.Snapshot()
				if len(as) != len(bs) {
					return false
				}
				for k, av := range as {
					bv, ok := bs[k]
					if !ok || !valuesEqual(av, bv) {
						return false
					}
				}
				return true
			case *value.ObjInstance:
				bo, ok := b.Obj.(*value.ObjInstance)
				if !ok {
					return false
				}
				if ao == bo {
					return true
				}
				if ao.Struct != bo.Struct || len(ao.Fields) != len(bo.Fields) {
					return false
				}
				for k, av := range ao.Fields {
					bv, ok := bo.Fields[k]
					if !ok || !valuesEqual(av, bv) {
						return false
					}
				}
				return true
			default:
				return a.Obj == b.Obj
			}
		case value.VAL_BYTES:
			return a.Obj.(string) == b.Obj.(string)
		case value.VAL_TASK:
			return a.Obj == b.Obj
		case value.VAL_REF:
			// Refs comparam por identidade de SLOT (não são dereferenciados —
			// o que também impede ciclos na comparação estrutural).
			ar, aok := a.Obj.(*value.ObjRef)
			br, bok := b.Obj.(*value.ObjRef)
			if !aok || !bok || ar == nil || br == nil {
				return a.Obj == b.Obj
			}
			if ar.RefType != br.RefType {
				return false
			}
			switch ar.RefType {
			case value.REF_GLOBAL:
				return ar.GlobalOwner == br.GlobalOwner && ar.Name == br.Name
			case value.REF_UPVALUE:
				return ar.Upvalue == br.Upvalue
			case value.REF_PTR:
				return ar.Ptr == br.Ptr
			case value.REF_PROPERTY:
				return ar.Container.Obj == br.Container.Obj && ar.Name == br.Name
			case value.REF_INDEX:
				return ar.Container.Obj == br.Container.Obj && valuesEqual(ar.Index, br.Index)
			}
			return false
		default:
			return false
		}
	}

	// Mixed types
	if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
		return float64(a.AsInt) == b.AsFloat
	}
	if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
		return a.AsFloat == float64(b.AsInt)
	}

	return false
}

func (vm *VM) readConstant() value.Value {
	// Assumes 1 byte operand for constant index
	index := vm.chunk.Code[vm.ip]
	vm.ip++
	return vm.chunk.Constants[index]
}

// stackOverflowPanic e o sentinela que push() lanca quando a pilha de
// operandos ja esta no teto; run() recupera SO este tipo e o converte no
// runtime error padrao. Qualquer outro panic continua subindo.
type stackOverflowPanic struct{}

func (vm *VM) push(v value.Value) {
	if vm.stackTop >= len(vm.stack) {
		vm.growStackForPush()
	}
	vm.stack[vm.stackTop] = v
	vm.stackTop++
}

// growStackForPush e o caminho FRIO de push(): cresce a pilha ou, ja no teto,
// lanca o sentinela. Mora em funcao propria, e com //go:noinline, DE
// PROPOSITO: com o corpo dentro de push o custo do inliner vai a 82 (orcamento
// 80) e push — a operacao mais quente do interpretador — deixa de ser inlinada;
// sem o pragma o inliner traz este corpo de volta e o custo sobe a 84. Com os
// dois, push fica em 77 e continua inlinada, como era antes das pilhas
// dinamicas. Conferir com `go build -gcflags='-m -m'` ao mexer aqui.
//
//go:noinline
func (vm *VM) growStackForPush() {
	if !vm.growStack() {
		panic(stackOverflowPanic{})
	}
}

// growStack dobra a pilha de operandos (ate StackMax) e reaponta os upvalues
// ABERTOS — os unicos ponteiros para dentro de vm.stack que sobrevivem a uma
// instrucao (fatias `args` passadas a natives sao lidas, nunca escritas, e os
// indices de Owned/StackBase nao mudam). Devolve false se ja esta no teto.
func (vm *VM) growStack() bool {
	if len(vm.stack) >= StackMax {
		return false
	}
	newLen := len(vm.stack) * 2
	if newLen > StackMax {
		newLen = StackMax
	}
	old := vm.stack
	grown := make([]value.Value, newLen)
	copy(grown, old)
	vm.stack = grown
	for upvalue := vm.openUpvalues; upvalue != nil; upvalue = upvalue.Next() {
		upvalue.Relocate(old, grown)
	}
	return true
}

// growFrames dobra o slice de frames (ate FramesMax) e reaponta
// vm.currentFrame, que sempre e &frames[frameCount-1] fora de uma chamada em
// andamento. Chamado so por ensureCallCapacity, ANTES de tomar &frames[n].
func (vm *VM) growFrames() {
	newLen := len(vm.frames) * 2
	if newLen > FramesMax {
		newLen = FramesMax
	}
	grown := make([]CallFrame, newLen)
	copy(grown, vm.frames)
	vm.frames = grown
	if vm.frameCount > 0 {
		vm.currentFrame = &vm.frames[vm.frameCount-1]
	}
}

func (vm *VM) pop() value.Value {
	vm.stackTop--
	val := vm.stack[vm.stackTop]
	vm.stack[vm.stackTop] = value.Value{} // Clear reference to help GC
	return val
}

func (vm *VM) peek(distance int) value.Value {
	return vm.stack[vm.stackTop-1-distance]
}

// ownSlot retém o composto no slot e o registra (slot, objeto) para release
// no fim do frame. O release do fim do frame libera o objeto GRAVADO aqui,
// nunca o ocupante atual do slot no momento do release — reuso de slot por
// temporários nunca retidos (locais de bloco mortos, ex.: OP_POP sem drop)
// tornaria o release por índice unsound (dec a menos, proibido pela spec).
//
// Ocupante novo NÃO-retível (null/escalar): a entrada do slot, se existir, é
// REMOVIDA — o site já liberou o ocupante velho (é o contrato deste helper),
// então mantê-la seria uma reivindicação FANTASMA sobre um objeto já pago, e
// o fim do frame (ou o bindOwnedSlot de uma iteração seguinte) o soltaria uma
// segunda vez (dec a mais, direção insegura).
func (f *CallFrame) ownSlot(vm *VM, slot int) {
	v := vm.stack[slot]
	retained := value.Retain(v)
	for i := range f.Owned {
		if f.Owned[i].slot == slot {
			if retained {
				// Sobrescrita do slot: o site ja liberou o ocupante velho; a
				// entrada passa a apontar o objeto novo (retido acima).
				f.Owned[i].obj = v
			} else {
				// Swap-remove: o slot k perde sua entrada (release ja aconteceu
				// no site chamador antes de nos chamar). O array de suporte
				// sobrevive a este frame (vm.frames e reusado, nao realocado),
				// entao o antigo indice `last` precisa ser zerado depois do
				// swap — senao continua nomeando um objeto ja liberado, preso
				// fora do alcance de qualquer `range f.Owned` (que respeita
				// len, nao cap) ate um append futuro por acaso sobrescreve-lo.
				last := len(f.Owned) - 1
				f.Owned[i] = f.Owned[last]
				f.Owned[last] = ownedEntry{}
				f.Owned = f.Owned[:last]
			}
			return
		}
	}
	if retained {
		f.Owned = append(f.Owned, ownedEntry{slot: slot, obj: v})
	}
}

// bindOwnedSlot e o OP_OWN_LOCAL: um vinculo NOVO nasce no slot (let, variavel
// de for-each, binding de case do select). Retem o ocupante e, se o frame ja
// tinha entrada para este indice, o vinculo que a criou MORREU (indices de slot
// sao reusados entre iteracoes de laco e blocos irmaos, e a lista nao e podada
// no fim do escopo) — o objeto gravado nela e pago AGORA, fechando o par
// retain/release daquele vinculo: e assim que cada elemento de um for-each
// recebe exatamente um retain (no bind da iteracao) e um release (no bind da
// iteracao seguinte, ou no fim do frame para o ultimo). Retain-antes-de-
// release: rebind do mesmo objeto (elemento repetido) nao passa por zero.
// Ocupante nao-composto (Retain falha) com entrada anterior: a entrada e
// removida depois de paga — deixa-la nomearia um objeto que um proximo
// OP_SET_LOCAL substituiria sem release (retain orfao, over-count).
func (f *CallFrame) bindOwnedSlot(vm *VM, slot int) {
	v := vm.stack[slot]
	retained := value.Retain(v)
	for i := range f.Owned {
		if f.Owned[i].slot != slot {
			continue
		}
		value.Release(f.Owned[i].obj)
		if retained {
			f.Owned[i].obj = v
		} else {
			// Swap-remove: mesmo cuidado de ownSlot acima — zerar `last`
			// depois do swap para nao reter, no backing array reusado entre
			// frames, o value.Value de um objeto ja liberado pela linha acima.
			last := len(f.Owned) - 1
			f.Owned[i] = f.Owned[last]
			f.Owned[last] = ownedEntry{}
			f.Owned = f.Owned[:last]
		}
		return
	}
	if retained {
		f.Owned = append(f.Owned, ownedEntry{slot: slot, obj: v})
	}
}

// captureUpvalue finds or creates an open upvalue for the given stack slot.
//
// RC: a caixa nasce POSSUIDORA. Quem a marca como emprestada e exclusivamente
// o OP_MARK_UPVALUE_BORROW que o compilador emite depois do OP_CLOSURE, pelo
// TIPO DECLARADO do slot capturado — a condicao e estatica. Inferi-la aqui a
// partir de frame.Owned seria errado nas duas direcoes (slot possuido com
// ocupante null/escalar na captura nao esta na lista; indice de slot reusado
// entre blocos irmaos deixa entrada morta na lista).
func (vm *VM) captureUpvalue(local *value.Value) *value.ObjUpvalue {
	// var prevUpvalue *value.ObjUpvalue // Unused for now
	upvalue := vm.openUpvalues

	// Walk list
	for upvalue != nil && !upvalue.PointsTo(local) {
		// prevUpvalue = upvalue
		upvalue = upvalue.Next()
	}

	if upvalue != nil {
		return upvalue
	}

	createdUpvalue := value.NewOpenUpvalue(local, vm.openUpvalues)
	vm.openUpvalues = createdUpvalue

	return createdUpvalue
}

// closeUpvalue fecha o box do upvalue aberto sobre o slot. A decisao de posse
// vem da PROPRIA CAIXA (marcada estaticamente pelo compilador via
// OP_MARK_UPVALUE_BORROW quando o slot capturado tem tipo `ref`): caixa
// possuidora assume a posse que o slot tinha; caixa emprestada nao retem —
// reter daria um dono a mais ao objeto emprestado e faria a mutacao atraves do
// empréstimo clonar, perdendo a escrita.
func (vm *VM) closeUpvalue(slot *value.Value) {
	var prev *value.ObjUpvalue
	curr := vm.openUpvalues

	for curr != nil {
		if curr.Close(slot) {
			// RC: o valor migra do slot do frame (liberado por
			// finalizeCurrentFrame) para o box do upvalue, que passa a ser
			// dono duravel independente do frame. So retem aqui - nunca
			// libera (o release do slot e responsabilidade do frame).
			if !curr.IsBorrowed() {
				value.Retain(*slot)
			}
			next := curr.Next()
			if prev == nil {
				vm.openUpvalues = next
			} else {
				prev.SetNext(next)
			}
			return
		}
		prev = curr
		curr = curr.Next()
	}
}
