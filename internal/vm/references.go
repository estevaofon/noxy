package vm

import (
	"fmt"
	"reflect"

	"noxy-vm/internal/value"
)

func referenceMapKey(index value.Value) (interface{}, error) {
	switch index.Type {
	case value.VAL_INT:
		return index.Int(), nil
	case value.VAL_OBJ:
		if key, ok := index.Obj.(string); ok {
			return key, nil
		}
		return nil, fmt.Errorf("map reference key must be int or string")
	default:
		return nil, fmt.Errorf("map reference key must be int or string")
	}
}

type referenceSetter func(value.Value)

func validateReferencedValue(stored value.Value) error {
	switch stored.Type {
	case value.VAL_OBJ, value.VAL_FUNCTION, value.VAL_NATIVE, value.VAL_BYTES,
		value.VAL_CHANNEL, value.VAL_WAITGROUP, value.VAL_REF, value.VAL_TASK:
		// These tags require a concrete payload in Obj.
	default:
		return nil
	}
	if stored.Obj == nil {
		return fmt.Errorf("invalid referenced object")
	}
	object := reflect.ValueOf(stored.Obj)
	switch object.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		if object.IsNil() {
			return fmt.Errorf("invalid referenced object")
		}
	}
	if stored.Type == value.VAL_TASK {
		task, ok := stored.Obj.(*value.ObjTask)
		if !ok || !task.IsValid() {
			return fmt.Errorf("invalid referenced object")
		}
	}
	return nil
}

func extractReferenceValue(input value.Value) (*value.ObjRef, error) {
	if input.Type != value.VAL_REF {
		return nil, fmt.Errorf("expected reference value, got %s", runtimeValueMode(input))
	}
	ref, ok := input.Obj.(*value.ObjRef)
	if !ok || ref == nil {
		return nil, fmt.Errorf("invalid reference value")
	}
	return ref, nil
}

// maxBorrowPathDepth limita a caminhada de um empréstimo até a raiz. Um
// caminho real tem a profundidade do lvalue escrito no fonte; o teto só existe
// para que bytecode malformado (um ref que se aponta) erre em vez de girar.
const maxBorrowPathDepth = 256

// borrowContainer devolve o contêiner ATUAL de um empréstimo (issue #83).
//
// Com Base preenchido, o caminho raiz→contêiner é re-resolvido agora, e não no
// instante da criação do ref. Em modo de ESCRITA cada nível é unicizado e o
// clone gravado de volta no lugar do pai (unicizeThroughRefValue), que é o que
// impede tanto o vazamento — a escrita ia parar num objeto que uma cópia
// posterior passou a compartilhar — quanto a escrita perdida, em que o CoW já
// bifurcou o caminho e o objeto congelado ficou órfão.
//
// A §1.3 da spec avisa que unicizar o contêiner na escrita é PIOR que o bug:
// a escrita vai para um clone anônimo e some. O que a torna correta aqui é a
// GRAVAÇÃO DE VOLTA em cada nível — o clone não é anônimo, ele toma o lugar do
// original dentro do pai, até a raiz, que é um ref de célula que o CoW não move.
//
// Em modo de LEITURA nada é unicizado: só se resolve o lugar. Ler não pode
// clonar (CloneCountValue não pode subir por causa desta correção), e ler o
// valor atual do lugar já é o comportamento certo.
func (vm *VM) borrowContainer(ref *value.ObjRef, forWrite bool) (value.Value, error) {
	if ref.Base.Type != value.VAL_REF {
		// ObjRef construído sem lugar de pai (natives, JSON, bytecode de
		// teste): comportamento antigo, o contêiner congelado.
		return ref.Container, nil
	}
	container := ref.Base
	for depth := 0; ; depth++ {
		if depth > maxBorrowPathDepth {
			return value.Value{}, fmt.Errorf("reference path too deep")
		}
		var err error
		if forWrite {
			container, err = vm.unicizeThroughRefValue(container)
		} else {
			container, err = vm.resolveReferenceValue(container)
		}
		if err != nil {
			return value.Value{}, err
		}
		// Campo ou elemento declarado `ref T`: o lugar guarda uma referência,
		// e o contêiner de verdade é o referente. Mesmo auto-deref que
		// OP_REF_PROPERTY/OP_REF_INDEX já faziam na criação.
		if container.Type != value.VAL_REF {
			return container, nil
		}
	}
}

func (vm *VM) referenceStorage(ref *value.ObjRef) (stored value.Value, exists bool, store referenceSetter, err error) {
	return vm.referenceStorageMode(ref, false)
}

func (vm *VM) referenceStorageMode(ref *value.ObjRef, forWrite bool) (stored value.Value, exists bool, store referenceSetter, err error) {
	defer func() {
		if err == nil && exists {
			if validationErr := validateReferencedValue(stored); validationErr != nil {
				err = validationErr
				store = nil
			}
		}
	}()
	if ref == nil {
		return value.Value{}, false, nil, fmt.Errorf("invalid reference value")
	}
	switch ref.RefType {
	case value.REF_GLOBAL:
		if ref.GlobalOwner == nil {
			return value.Value{}, false, nil, fmt.Errorf("invalid global reference owner")
		}
		stored, ok := ref.GlobalOwner.GetLocal(ref.Name)
		if !ok {
			return value.Value{}, false, nil, fmt.Errorf("undefined global variable '%s'", ref.Name)
		}
		return stored, true, func(updated value.Value) { ref.GlobalOwner.SetLocal(ref.Name, updated) }, nil
	case value.REF_UPVALUE:
		stored, ok := ref.Upvalue.Load()
		if !ok {
			return value.Value{}, false, nil, fmt.Errorf("invalid upvalue reference")
		}
		return stored, true, func(updated value.Value) { ref.Upvalue.Store(updated) }, nil
	case value.REF_PTR:
		if ref.Ptr == nil {
			return value.Value{}, false, nil, fmt.Errorf("invalid pointer reference")
		}
		return *ref.Ptr, true, func(updated value.Value) { *ref.Ptr = updated }, nil
	case value.REF_PROPERTY:
		container, err := vm.borrowContainer(ref, forWrite)
		if err != nil {
			return value.Value{}, false, nil, err
		}
		instance, ok := container.Obj.(*value.ObjInstance)
		if container.Type != value.VAL_OBJ || !ok || instance == nil {
			return value.Value{}, false, nil, fmt.Errorf("Target is not an instance")
		}
		stored, ok := instance.Fields[ref.Name]
		if !ok {
			return value.Value{}, false, nil, fmt.Errorf("undefined property '%s'", ref.Name)
		}
		return stored, true, func(updated value.Value) { instance.Fields[ref.Name] = updated }, nil
	case value.REF_INDEX:
		container, err := vm.borrowContainer(ref, forWrite)
		if err != nil {
			return value.Value{}, false, nil, err
		}
		if array, ok := container.Obj.(*value.ObjArray); container.Type == value.VAL_OBJ && ok && array != nil {
			if ref.Index.Type != value.VAL_INT {
				return value.Value{}, false, nil, fmt.Errorf("array reference index must be integer")
			}
			index := int(ref.Index.Int())
			if index < 0 || index >= len(array.Elements) {
				return value.Value{}, false, nil, fmt.Errorf("Index out of bounds")
			}
			return array.Elements[index], true, func(updated value.Value) { array.Elements[index] = updated }, nil
		}
		if mapping, ok := container.Obj.(*value.ObjMap); container.Type == value.VAL_OBJ && ok && mapping != nil {
			key, err := referenceMapKey(ref.Index)
			if err != nil {
				return value.Value{}, false, nil, err
			}
			stored, exists := mapping.Get(key)
			if !exists {
				stored = value.NewNull()
			}
			return stored, exists, func(updated value.Value) { mapping.Set(key, updated) }, nil
		}
		return value.Value{}, false, nil, fmt.Errorf("Target is not indexable")
	default:
		return value.Value{}, false, nil, fmt.Errorf("invalid reference target")
	}
}

// refStorageBorrows informa que o lugar apontado pelo ref NAO possui o que
// guarda, e portanto a troca ali nao pode contar posse (soltar o que nunca se
// reteve e dec a menos). Hoje o unico lugar assim e a caixa de upvalue marcada
// como emprestada — caixa aberta sobre um slot que nao retem o que guarda.
//
// Caminho VIVO desde o OP_REF_LOCAL_BORROW: `ref` para um slot nao-possuidor
// (hoje, slot de tipo `ref T`) cria uma caixa REF_UPVALUE marcada emprestada,
// e a escrita atraves dela cai exatamente aqui. E a consulta que impede
// `setit(ref x, ...)` de soltar um objeto que o slot x nunca reteve.
func refStorageBorrows(ref *value.ObjRef) bool {
	return ref != nil && ref.RefType == value.REF_UPVALUE && ref.Upvalue.IsBorrowed()
}

// retargetOwnedSlot mantém honesta a lista de posse do frame quando uma escrita
// ATRAVÉS DE UM REF troca o ocupante de um slot de pilha POSSUÍDO.
//
// O funil de escrita faz retain(novo)/release(velho) porque o slot passa a
// possuir o valor novo — mas a entrada (slot, objeto) do frame continuava
// nomeando o objeto VELHO, e o release em massa do fim do frame o soltava DE
// NOVO: dec a mais, direção insegura (o objeto velho, ainda vivo em outro dono,
// passava a parecer único e a mutação seguinte acontecia no lugar). Reapontar a
// entrada para o valor novo fecha a conta: o release do velho é pago agora pelo
// funil, e o do novo pelo fim do frame.
//
// Devolve true quando encontrou (e reapontou) a entrada. Só varre as listas de
// posse dos frames vivos — pequenas — e só para refs que apontam para slot de
// pilha; os demais tipos de ref saem no primeiro teste.
//
// A varredura é de DENTRO PARA FORA (frame mais interno primeiro): índices
// absolutos de slot são reusados — a região de pilha de um frame chamado
// sobrepõe os índices onde blocos irmãos mortos do CHAMADOR deixaram entradas
// nunca podadas. Varrer de fora para dentro casava a entrada MORTA do chamador
// (mesmo endereço) e deixava a entrada VIVA do frame interno nomeando o objeto
// velho — solto duas vezes no fim do frame (dec a mais). A direção inversa é
// segura: as entradas de um frame interno são todas >= seu LocalBase, então
// nenhuma entrada interna pode aliasar um slot vivo de um frame externo.
func (vm *VM) retargetOwnedSlot(ref *value.ObjRef, updated value.Value) bool {
	if ref == nil || (ref.RefType != value.REF_UPVALUE && ref.RefType != value.REF_PTR) {
		return false
	}
	for i := vm.frameCount - 1; i >= 0; i-- {
		frame := &vm.frames[i]
		for j := range frame.Owned {
			slot := frame.Owned[j].slot
			if slot < 0 || slot >= len(vm.stack) {
				continue
			}
			occupant := &vm.stack[slot]
			if ref.RefType == value.REF_UPVALUE {
				// PointsTo é falso para caixa já fechada — nesse caso o valor não
				// mora mais no slot e não há entrada a reapontar.
				if !ref.Upvalue.PointsTo(occupant) {
					continue
				}
			} else if ref.Ptr != occupant {
				continue
			}
			frame.Owned[j].obj = updated
			return true
		}
	}
	return false
}

// retargetOwnedSlotForUpvalue e o mesmo reaponte do retargetOwnedSlot para os
// funis que escrevem ATRAVES DA CAIXA DE UPVALUE (OP_SET_UPVALUE e
// OP_GET_UPVALUE_MUT em caixa possuidora): enquanto a caixa esta ABERTA a
// escrita alcanca um slot de pilha, e se aquele slot e possuido a entrada
// (slot, objeto) do frame dono tem de passar a nomear o valor novo — senao o
// release em massa do fim do frame solta o velho uma SEGUNDA vez (dec a mais,
// direcao insegura: o velho, ainda vivo em outro dono, passa a parecer unico).
// Caixa fechada: PointsTo e falso para qualquer slot de pilha (o valor mora no
// proprio box) e nao ha entrada a reapontar. O guard de openUpvalues zera o
// custo no caso comum (nenhuma captura aberta). Varredura de DENTRO PARA FORA
// pela mesma razao do retargetOwnedSlot: uma entrada morta do chamador num
// indice reusado casaria primeiro e deixaria a entrada viva do frame interno
// obsoleta (dec a mais no fim do frame).
func (vm *VM) retargetOwnedSlotForUpvalue(upv *value.ObjUpvalue, updated value.Value) bool {
	if upv == nil || vm.openUpvalues == nil {
		return false
	}
	for i := vm.frameCount - 1; i >= 0; i-- {
		frame := &vm.frames[i]
		for j := range frame.Owned {
			slot := frame.Owned[j].slot
			if slot < 0 || slot >= len(vm.stack) {
				continue
			}
			if !upv.PointsTo(&vm.stack[slot]) {
				continue
			}
			frame.Owned[j].obj = updated
			return true
		}
	}
	return false
}

func (vm *VM) lookupReferenceValue(ref *value.ObjRef) (value.Value, error) {
	stored, _, _, err := vm.referenceStorage(ref)
	return stored, err
}

func (vm *VM) storeReferenceValue(input value.Value, updated value.Value) error {
	if input.Type == value.VAL_NULL {
		return fmt.Errorf("cannot update null reference")
	}
	ref, err := extractReferenceValue(input)
	if err != nil {
		return err
	}
	stored, exists, store, err := vm.referenceStorageMode(ref, true)
	if err != nil {
		return err
	}
	// issue #83: escrever através de uma referência cuja entrada não existe
	// mais RESSUSCITAVA a chave — `mapping.Set` insere. Um empréstimo denota um
	// lugar que existia; se o lugar foi apagado durante a vida dele, a escrita
	// é um acesso conflitante e falha alto, em vez de recriar em silêncio algo
	// que o programa mandou apagar. Só o ramo de map chega aqui com exists
	// falso: índice de array fora de faixa e campo inexistente já erram antes.
	if !exists {
		return fmt.Errorf("reference target no longer exists")
	}
	// RC: funil unico para OP_STORE_REF / OP_STORE_VIA_REF /
	// OP_SET_PROPERTY_DEREF - retain-antes-de-release em torno da troca. Lugar
	// que apenas empresta (caixa de upvalue emprestada) troca sem contar.
	if !refStorageBorrows(ref) {
		value.Retain(updated)
		// Se o destino e um slot de pilha possuido, a entrada de posse do frame
		// tem de passar a nomear o valor novo — senao o fim do frame soltaria o
		// velho uma segunda vez (dec a mais).
		vm.retargetOwnedSlot(ref, updated)
		value.Release(stored)
	}
	store(updated)
	return nil
}

func (vm *VM) resolveReferenceValue(input value.Value) (value.Value, error) {
	if input.Type == value.VAL_NULL {
		return value.Value{}, fmt.Errorf("cannot dereference null reference")
	}
	ref, err := extractReferenceValue(input)
	if err != nil {
		return value.Value{}, err
	}
	return vm.lookupReferenceValue(ref)
}
