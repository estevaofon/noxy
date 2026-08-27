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

// referenceSetter descreve ONDE gravar, em vez de ser uma closure que grava.
//
// Perf (issue #83, follow-up): a versão closure era responsável por 68,8% das
// alocações de um laço que escreve através de um empréstimo — uma por NÍVEL do
// caminho e por acesso, inclusive na LEITURA, que descarta o setter sem nunca
// chamá-lo. Como struct de valor, ele não escapa e não aloca; o custo vira
// alguns campos na pilha.
//
// setterNone é o zero, e `valid()` é o que substitui o antigo `store == nil`.
// Um campo por tipo de alvo. Medi a alternativa de um `target interface{}` só,
// para encolher o struct: ficou ~10% MAIS LENTA (asserção de tipo no caminho
// quente vale mais que as palavras economizadas). Fica a versão larga.
type referenceSetter struct {
	array    *value.ObjArray
	mapping  *value.ObjMap
	instance *value.ObjInstance
	ptr      *value.Value
	upvalue  *value.ObjUpvalue
	globals  *value.GlobalEnvironment
	key      interface{}
	name     string
	index    int
	kind     setterKind
}

type setterKind uint8

const (
	setterNone setterKind = iota
	setterGlobal
	setterUpvalue
	setterPtr
	setterProperty
	setterArray
	setterMap
)

func (s referenceSetter) valid() bool { return s.kind != setterNone }

func (s referenceSetter) set(updated value.Value) {
	switch s.kind {
	case setterGlobal:
		s.globals.SetLocal(s.name, updated)
	case setterUpvalue:
		s.upvalue.Store(updated)
	case setterPtr:
		*s.ptr = updated
	case setterProperty:
		s.instance.MustSet(s.name, updated)
	case setterArray:
		s.array.Elements[s.index] = updated
	case setterMap:
		s.mapping.Set(s.key, updated)
	}
}

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
	// Coleta a cadeia de lugares até a raiz num array de PILHA, e depois
	// desce dela para cá. A primeira versão recorria — cada nível passava por
	// referenceStorageMode inteiro (despacho por RefType, construção de
	// setter, checagem de unicidade) só para servir de base ao nível
	// seguinte. Este laço faz o mesmo trabalho útil sem a recursão e sem
	// alocar: `chain` não escapa.
	var inline [8]*value.ObjRef
	chain := inline[:0]
	cursor := ref
	for cursor.Base.Type == value.VAL_REF {
		parent, ok := cursor.Base.Obj.(*value.ObjRef)
		if !ok || parent == nil {
			return value.Value{}, fmt.Errorf("invalid reference value")
		}
		if len(chain) > maxBorrowPathDepth {
			return value.Value{}, fmt.Errorf("reference path too deep")
		}
		chain = append(chain, parent)
		cursor = parent
	}
	// cursor é a raiz: uma referência de célula. O CoW não move a célula, mas
	// o CONTEÚDO dela é o primeiro nível do caminho e, numa escrita, precisa
	// ser unicizado e gravado de volta como qualquer outro nível.
	root := value.Value{Type: value.VAL_REF, Obj: cursor}
	var container value.Value
	var err error
	if forWrite {
		container, err = vm.unicizeThroughRefValue(root)
	} else {
		container, err = vm.resolveReferenceValue(root)
	}
	if err != nil {
		return value.Value{}, err
	}
	// chain[len-1] é a raiz, já resolvida acima. Os demais são os níveis
	// INTERMEDIÁRIOS, de fora para dentro. O passo final — o que `ref` mesmo
	// representa — não entra aqui: quem o aplica é referenceStorageMode, que
	// é quem precisa do setter daquele nível.
	for i := len(chain) - 2; i >= 0; i-- {
		if container, err = vm.descend(container, chain[i], forWrite); err != nil {
			return value.Value{}, err
		}
	}
	return vm.derefPlace(container, forWrite)
}

// derefPlace resolve um lugar que guarda uma REFERÊNCIA (campo ou elemento
// declarado `ref T`): o contêiner de verdade é o referente. Mesmo auto-deref
// que OP_REF_PROPERTY/OP_REF_INDEX já faziam na criação.
func (vm *VM) derefPlace(container value.Value, forWrite bool) (value.Value, error) {
	for depth := 0; container.Type == value.VAL_REF; depth++ {
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
	}
	return container, nil
}

// descend desce UM nível do caminho de um empréstimo, direto sobre o contêiner
// já resolvido do pai.
//
// Em modo de ESCRITA o filho é unicizado e o clone gravado de volta no pai aqui
// mesmo — é essa gravação de volta que torna a caminhada correta (a §1.3 da
// spec do #83 avisa que unicizar sem gravar de volta perde a escrita num clone
// anônimo). Em modo de LEITURA nada é unicizado: ler não pode clonar.
//
// `step` é o ObjRef daquele nível; só RefType+Name/Index são lidos, nunca a
// base dele — quem sequencia os níveis é borrowContainer.
func (vm *VM) descend(container value.Value, step *value.ObjRef, forWrite bool) (value.Value, error) {
	container, err := vm.derefPlace(container, forWrite)
	if err != nil {
		return value.Value{}, err
	}
	if container.Type != value.VAL_OBJ {
		return value.Value{}, fmt.Errorf("Target is not indexable")
	}
	if step.RefType == value.REF_PROPERTY {
		instance, ok := container.Obj.(*value.ObjInstance)
		if !ok || instance == nil {
			return value.Value{}, fmt.Errorf("Target is not an instance")
		}
		child, ok := instance.Get(step.Name)
		if !ok {
			return value.Value{}, fmt.Errorf("undefined property '%s'", step.Name)
		}
		if !forWrite {
			return child, nil
		}
		if unique, changed := vm.unicize(child); changed {
			value.Retain(unique)
			instance.MustSet(step.Name, unique)
			value.Release(child)
			return unique, nil
		}
		return child, nil
	}
	if array, ok := container.Obj.(*value.ObjArray); ok && array != nil {
		if step.Index.Type != value.VAL_INT {
			return value.Value{}, fmt.Errorf("array reference index must be integer")
		}
		index := int(step.Index.Int())
		if index < 0 || index >= len(array.Elements) {
			return value.Value{}, fmt.Errorf("Index out of bounds")
		}
		child := array.Elements[index]
		if !forWrite {
			return child, nil
		}
		if unique, changed := vm.unicize(child); changed {
			value.Retain(unique)
			array.Elements[index] = unique
			value.Release(child)
			return unique, nil
		}
		return child, nil
	}
	if mapping, ok := container.Obj.(*value.ObjMap); ok && mapping != nil {
		key, err := referenceMapKey(step.Index)
		if err != nil {
			return value.Value{}, err
		}
		child, exists := mapping.Get(key)
		if !exists {
			return value.Value{}, fmt.Errorf("reference target no longer exists")
		}
		if !forWrite {
			return child, nil
		}
		if unique, changed := vm.unicize(child); changed {
			value.Retain(unique)
			mapping.Set(key, unique)
			value.Release(child)
			return unique, nil
		}
		return child, nil
	}
	return value.Value{}, fmt.Errorf("Target is not indexable")
}

func (vm *VM) referenceStorage(ref *value.ObjRef) (stored value.Value, exists bool, store referenceSetter, err error) {
	return vm.referenceStorageMode(ref, false)
}

func (vm *VM) referenceStorageMode(ref *value.ObjRef, forWrite bool) (stored value.Value, exists bool, store referenceSetter, err error) {
	defer func() {
		if err == nil && exists {
			if validationErr := validateReferencedValue(stored); validationErr != nil {
				err = validationErr
				store = referenceSetter{}
			}
		}
	}()
	if ref == nil {
		return value.Value{}, false, referenceSetter{}, fmt.Errorf("invalid reference value")
	}
	switch ref.RefType {
	case value.REF_GLOBAL:
		if ref.GlobalOwner == nil {
			return value.Value{}, false, referenceSetter{}, fmt.Errorf("invalid global reference owner")
		}
		stored, ok := ref.GlobalOwner.GetLocal(ref.Name)
		if !ok {
			return value.Value{}, false, referenceSetter{}, fmt.Errorf("undefined global variable '%s'", ref.Name)
		}
		return stored, true, referenceSetter{kind: setterGlobal, globals: ref.GlobalOwner, name: ref.Name}, nil
	case value.REF_UPVALUE:
		stored, ok := ref.Upvalue.Load()
		if !ok {
			return value.Value{}, false, referenceSetter{}, fmt.Errorf("invalid upvalue reference")
		}
		return stored, true, referenceSetter{kind: setterUpvalue, upvalue: ref.Upvalue}, nil
	case value.REF_PTR:
		if ref.Ptr == nil {
			return value.Value{}, false, referenceSetter{}, fmt.Errorf("invalid pointer reference")
		}
		return *ref.Ptr, true, referenceSetter{kind: setterPtr, ptr: ref.Ptr}, nil
	case value.REF_PROPERTY:
		container, err := vm.borrowContainer(ref, forWrite)
		if err != nil {
			return value.Value{}, false, referenceSetter{}, err
		}
		instance, ok := container.Obj.(*value.ObjInstance)
		if container.Type != value.VAL_OBJ || !ok || instance == nil {
			return value.Value{}, false, referenceSetter{}, fmt.Errorf("Target is not an instance")
		}
		stored, ok := instance.Get(ref.Name)
		if !ok {
			return value.Value{}, false, referenceSetter{}, fmt.Errorf("undefined property '%s'", ref.Name)
		}
		return stored, true, referenceSetter{kind: setterProperty, instance: instance, name: ref.Name}, nil
	case value.REF_INDEX:
		container, err := vm.borrowContainer(ref, forWrite)
		if err != nil {
			return value.Value{}, false, referenceSetter{}, err
		}
		if array, ok := container.Obj.(*value.ObjArray); container.Type == value.VAL_OBJ && ok && array != nil {
			if ref.Index.Type != value.VAL_INT {
				return value.Value{}, false, referenceSetter{}, fmt.Errorf("array reference index must be integer")
			}
			index := int(ref.Index.Int())
			if index < 0 || index >= len(array.Elements) {
				return value.Value{}, false, referenceSetter{}, fmt.Errorf("Index out of bounds")
			}
			return array.Elements[index], true, referenceSetter{kind: setterArray, array: array, index: index}, nil
		}
		if mapping, ok := container.Obj.(*value.ObjMap); container.Type == value.VAL_OBJ && ok && mapping != nil {
			key, err := referenceMapKey(ref.Index)
			if err != nil {
				return value.Value{}, false, referenceSetter{}, err
			}
			stored, exists := mapping.Get(key)
			if !exists {
				stored = value.NewNull()
			}
			return stored, exists, referenceSetter{kind: setterMap, mapping: mapping, key: key}, nil
		}
		return value.Value{}, false, referenceSetter{}, fmt.Errorf("Target is not indexable")
	default:
		return value.Value{}, false, referenceSetter{}, fmt.Errorf("invalid reference target")
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
	store.set(updated)
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

// borrowBaseAddr descreve o LUGAR do pai de um empréstimo, para `addr` (issue
// #83). Imprimir o ponteiro de `Container` deixou de ser honesto quando o
// empréstimo passou a denotar um caminho: aquele campo é o retrato do instante
// da criação, e dois roots ainda preguiçosamente compartilhados imprimiam o
// mesmo endereço para lugares diferentes — e, depois de uma escrita, o endereço
// nomeava o objeto que a CÓPIA ficou possuindo.
//
// O caminho é a identidade, então é ele que `addr` mostra:
//
//	addr(ref a[0].x)  ->  <prop x of <index 0 of <global a>>>
func borrowBaseAddr(ref *value.ObjRef) string {
	if ref.Base.Type != value.VAL_REF {
		return fmt.Sprintf("%p", ref.Container.Obj)
	}
	base, ok := ref.Base.Obj.(*value.ObjRef)
	if !ok || base == nil {
		return "<invalid base>"
	}
	switch base.RefType {
	case value.REF_GLOBAL:
		return fmt.Sprintf("<global %s>", base.Name)
	case value.REF_UPVALUE:
		if location, ok := base.Upvalue.LocationAddress(); ok {
			return location
		}
		return "<invalid upvalue>"
	case value.REF_PTR:
		return fmt.Sprintf("%p", base.Ptr)
	case value.REF_PROPERTY:
		return fmt.Sprintf("<prop %s of %s>", base.Name, borrowBaseAddr(base))
	case value.REF_INDEX:
		return fmt.Sprintf("<index %s of %s>", base.Index.String(), borrowBaseAddr(base))
	}
	return "<invalid base>"
}
