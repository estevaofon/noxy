package vm

import (
	"fmt"
	"reflect"

	"noxy-vm/internal/value"
)

func referenceMapKey(index value.Value) (interface{}, error) {
	switch index.Type {
	case value.VAL_INT:
		return index.AsInt, nil
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

func (vm *VM) referenceStorage(ref *value.ObjRef) (stored value.Value, exists bool, store referenceSetter, err error) {
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
		instance, ok := ref.Container.Obj.(*value.ObjInstance)
		if ref.Container.Type != value.VAL_OBJ || !ok || instance == nil {
			return value.Value{}, false, nil, fmt.Errorf("Target is not an instance")
		}
		stored, ok := instance.Fields[ref.Name]
		if !ok {
			return value.Value{}, false, nil, fmt.Errorf("undefined property '%s'", ref.Name)
		}
		return stored, true, func(updated value.Value) { instance.Fields[ref.Name] = updated }, nil
	case value.REF_INDEX:
		if array, ok := ref.Container.Obj.(*value.ObjArray); ref.Container.Type == value.VAL_OBJ && ok && array != nil {
			if ref.Index.Type != value.VAL_INT {
				return value.Value{}, false, nil, fmt.Errorf("array reference index must be integer")
			}
			index := int(ref.Index.AsInt)
			if index < 0 || index >= len(array.Elements) {
				return value.Value{}, false, nil, fmt.Errorf("Index out of bounds")
			}
			return array.Elements[index], true, func(updated value.Value) { array.Elements[index] = updated }, nil
		}
		if mapping, ok := ref.Container.Obj.(*value.ObjMap); ref.Container.Type == value.VAL_OBJ && ok && mapping != nil {
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
func (vm *VM) retargetOwnedSlot(ref *value.ObjRef, updated value.Value) bool {
	if ref == nil || (ref.RefType != value.REF_UPVALUE && ref.RefType != value.REF_PTR) {
		return false
	}
	for i := 0; i < vm.frameCount; i++ {
		frame := vm.frames[i]
		if frame == nil {
			continue
		}
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
// custo no caso comum (nenhuma captura aberta).
func (vm *VM) retargetOwnedSlotForUpvalue(upv *value.ObjUpvalue, updated value.Value) bool {
	if upv == nil || vm.openUpvalues == nil {
		return false
	}
	for i := 0; i < vm.frameCount; i++ {
		frame := vm.frames[i]
		if frame == nil {
			continue
		}
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

func (vm *VM) lookupGlobalReferenceValue(ref *value.ObjRef) (value.Value, error) {
	stored, _, _, err := vm.referenceStorage(ref)
	return stored, err
}

func (vm *VM) storeGlobalReferenceValue(ref *value.ObjRef, updated value.Value) error {
	_, _, store, err := vm.referenceStorage(ref)
	if err != nil {
		return err
	}
	store(updated)
	return nil
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
	stored, _, store, err := vm.referenceStorage(ref)
	if err != nil {
		return err
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
