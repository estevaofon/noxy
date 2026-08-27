package vm

import "noxy-vm/internal/value"

func (vm *VM) readShort() uint16 {
	vm.ip += 2
	return uint16(vm.chunk.Code[vm.ip-2])<<8 | uint16(vm.chunk.Code[vm.ip-1])
}

// sameBorrowBase decide se dois empréstimos partem do MESMO lugar (issue #83).
//
// Comparar `Container.Obj` era fiel enquanto o contêiner era unicizado na
// criação do ref. Com o empréstimo denotando um lugar, esse campo virou um
// retrato do instante da criação: dois roots ainda preguiçosamente
// compartilhados (`let b = a`) dão o MESMO ponteiro, e `ref a[0].x == ref
// b[0].x` respondia `true` para dois lugares que o próprio programa prova
// serem diferentes — escrever num deixa o outro intacto.
//
// A identidade de um empréstimo é o caminho: o lugar do pai mais o passo. A
// recursão termina num ref de célula, que se compara por identidade de slot.
func sameBorrowBase(a, b *value.ObjRef) bool {
	if a.Base.Type == value.VAL_REF || b.Base.Type == value.VAL_REF {
		return valuesEqual(a.Base, b.Base)
	}
	// Nenhum dos dois tem lugar de pai (ObjRef construído fora do compilador):
	// resta o contêiner congelado.
	return a.Container.Obj == b.Container.Obj
}

func valuesEqual(a, b value.Value) bool {
	if a.Type == b.Type {
		switch a.Type {
		case value.VAL_BOOL:
			return a.Bool() == b.Bool()
		case value.VAL_NULL:
			return true
		case value.VAL_INT:
			return a.Int() == b.Int()
		case value.VAL_FLOAT:
			return a.Float() == b.Float()
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
				if ao.Struct != bo.Struct || len(ao.Slots) != len(bo.Slots) {
					return false
				}
				for i, av := range ao.Slots {
					if !valuesEqual(av, bo.Slots[i]) {
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
				return sameBorrowBase(ar, br) && ar.Name == br.Name
			case value.REF_INDEX:
				return sameBorrowBase(ar, br) && valuesEqual(ar.Index, br.Index)
			}
			return false
		default:
			return false
		}
	}

	// Mixed types
	if a.Type == value.VAL_INT && b.Type == value.VAL_FLOAT {
		return float64(a.Int()) == b.Float()
	}
	if a.Type == value.VAL_FLOAT && b.Type == value.VAL_INT {
		return a.Float() == float64(b.Int())
	}

	return false
}

func (vm *VM) readConstant() value.Value {
	// Assumes 1 byte operand for constant index
	index := vm.chunk.Code[vm.ip]
	vm.ip++
	return vm.chunk.Constants[index]
}

// stackOverflowPanic e o sentinela que push() lanca quando a folga garantida
// na entrada do frame acabou; run() recupera SO este tipo e o converte no
// runtime error padrao. Qualquer outro panic continua subindo.
type stackOverflowPanic struct{}

// errStackOverflow e o sentinela PRONTO (variavel, nao literal composto: o
// literal custa 1 no inliner e estoura o orcamento de 20 de run()).
var errStackOverflow = stackOverflowPanic{}

// push NAO cresce a pilha, de proposito. run() tem mais de 5000 nos de AST,
// entao o inliner o trata como "big function" e so inlina callees de custo
// <= 20 (inlineBigFunctionMaxCost), nao 80. Este corpo custa exatamente 20 e
// e inlinado nos 117 call sites de executor.go; qualquer no a mais (chamar
// growStack no ramo frio, um literal composto no panic, ou comparar com
// len(vm.stack) em vez de vm.stackLimit) o tira do inline em TODOS eles e
// custa ~20 % no interpretador. O crescimento mora em ensureCallCapacity
// (entrada do frame) e ensureStackHeadroom (defer / call_result); quando um
// unico frame esgota a folga de stackReserve, o sentinela abaixo vira runtime
// error limpo. internal/vm/inline_guard_test.go trava esta propriedade.
func (vm *VM) push(v value.Value) {
	if vm.stackTop >= vm.stackLimit {
		panic(errStackOverflow)
	}
	vm.stack[vm.stackTop] = v
	vm.stackTop++
}

// installStack e o funil unico de troca da pilha de operandos: mantem
// stackLimit em sincronia com len(stack). Atribuir vm.stack direto deixaria
// push() lendo um limite velho — pequeno demais (pushes rejeitados sem motivo)
// ou grande demais (escrita fora da pilha nova).
func (vm *VM) installStack(stack []value.Value) {
	vm.stack = stack
	vm.stackLimit = len(stack)
}

// growStack dobra a pilha de operandos (ate StackMax) e migra os upvalues
// ABERTOS — os unicos ponteiros para dentro de vm.stack que sobrevivem a uma
// instrucao (fatias `args` passadas a natives sao lidas, nunca escritas, e os
// indices de Owned/StackBase nao mudam). A copia do conteudo e feita por
// RelocateOpenUpvalues, com as caixas travadas, porque uma task pode estar
// escrevendo por uma delas nesta pilha. Devolve false so no TETO.
func (vm *VM) growStack() bool {
	if len(vm.stack) >= StackMax {
		return false
	}
	newLen := len(vm.stack) * 2
	if newLen == 0 {
		// Pilha vazia dobraria para zero e nao cresceria nunca — o laco de
		// ensureStackHeadroom giraria para sempre.
		newLen = stackInitial
	}
	if newLen > StackMax {
		newLen = StackMax
	}
	old := vm.stack
	grown := make([]value.Value, newLen)
	value.RelocateOpenUpvalues(vm.openUpvalues, old, grown)
	vm.installStack(grown)
	return true
}

// ensureStackHeadroom garante `slots` livres acima de stackTop, crescendo a
// pilha sob demanda. Devolve false SO quando o teto (StackMax) nao permite
// mais crescer — nunca por causa da alocacao atual, que e so o tamanho de
// agora. Todo guard de "cabe empilhar N?" tem de passar por aqui: medir contra
// len(vm.stack) reprovaria chamadas centenas de milhares de slots abaixo do
// limite real. Junto com ensureCallCapacity, e o unico lugar onde a pilha
// cresce — push() nao cresce (ver push).
func (vm *VM) ensureStackHeadroom(slots int) bool {
	for len(vm.stack)-vm.stackTop < slots {
		if !vm.growStack() {
			return false
		}
	}
	return true
}

// growFrames dobra o slice de frames (ate FramesMax) e reaponta
// vm.currentFrame, que sempre e &frames[frameCount-1] fora de uma chamada em
// andamento. Chamado so por growForCall, ANTES de tomar &frames[n]. Devolve
// false no teto, como growStack.
func (vm *VM) growFrames() bool {
	if len(vm.frames) >= FramesMax {
		return false
	}
	newLen := len(vm.frames) * 2
	if newLen == 0 {
		newLen = framesInitial
	}
	if newLen > FramesMax {
		newLen = FramesMax
	}
	grown := make([]CallFrame, newLen)
	copy(grown, vm.frames)
	vm.frames = grown
	if vm.frameCount > 0 {
		vm.currentFrame = &vm.frames[vm.frameCount-1]
	}
	return true
}

// pop desempilha e zera o slot (solta a referencia para o GC). A FORMA do
// corpo importa: a versao de sempre (`val := vm.stack[top]; vm.stack[top] =
// value.Value{}; return val`) custa 22 no inliner e deixava pop fora do
// inline em TODOS os ~84 sites de run() (orcamento 20 para callee de funcao
// grande — ver push); a atribuicao dupla com resultado nomeado faz o mesmo
// trabalho em 18 nos (um statement e uma declaracao a menos) e e inlinada em
// ~80 sites. Variantes "obvias" sao piores: `slot.Obj = nil` via
// `&vm.stack[top]` custa 26, limpar so Obj com duas indexacoes custa 23.
// inline_guard_test.go trava custo e contagem de sites, como faz com push
// (issue #37, "extra barato").
func (vm *VM) pop() (val value.Value) {
	vm.stackTop--
	val, vm.stack[vm.stackTop] = vm.stack[vm.stackTop], value.Value{} // Clear reference to help GC
	return
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
