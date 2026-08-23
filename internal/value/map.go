package value

import (
	"sync"
	"sync/atomic"
)

type bindingStore struct {
	mu     sync.RWMutex
	gen    atomic.Uint64
	values map[interface{}]Value
}

// Regra do gen: cada funil de mutação deste store precisa terminar com
// store.gen.Add(1), sempre DEPOIS de aplicar a mutação — nunca antes. Um
// bump visível antes da escrita deixaria um leitor concorrente observar a
// nova geração com o valor velho ainda no map e cachear esse valor como se
// fosse atual; um quinto funil que esqueça o bump reintroduz leitura
// obsoleta em silêncio, sem nenhum teste acusando.
func newBindingStore(values map[interface{}]Value) *bindingStore {
	if values == nil {
		values = make(map[interface{}]Value)
	}
	return &bindingStore{values: values}
}

func (store *bindingStore) get(key interface{}) (Value, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	result, ok := store.values[key]
	return result, ok
}

func (store *bindingStore) set(key interface{}, item Value) {
	store.mu.Lock()
	store.values[key] = item
	store.mu.Unlock()
	store.gen.Add(1)
}

// swap grava item em key e devolve o ocupante anterior, tudo numa secao
// critica — e o `m[k] = v` do VM (setIndexGeneric), que antes fazia get + set
// (dois ciclos de lock, dois hashes) so para saber se havia velho a liberar
// (issue #66, item 4). Mesma regra do gen: bump DEPOIS da escrita.
func (store *bindingStore) swap(key interface{}, item Value) (old Value, existed bool) {
	store.mu.Lock()
	old, existed = store.values[key]
	store.values[key] = item
	store.mu.Unlock()
	store.gen.Add(1)
	return old, existed
}

// count e len(values) sob RLock — Len() fazia len(snapshot()), copiando o map
// inteiro para contar (issue #66, item 4).
func (store *bindingStore) count() int {
	store.mu.RLock()
	n := len(store.values)
	store.mu.RUnlock()
	return n
}

func (store *bindingStore) defineIfAbsent(key interface{}, item Value) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.values[key]; exists {
		return false
	}
	store.values[key] = item
	store.gen.Add(1)
	return true
}

func (store *bindingStore) delete(key interface{}) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.values[key]; !exists {
		return false
	}
	delete(store.values, key)
	store.gen.Add(1)
	return true
}

func (store *bindingStore) snapshot() map[interface{}]Value {
	store.mu.RLock()
	defer store.mu.RUnlock()
	result := make(map[interface{}]Value, len(store.values))
	for key, item := range store.values {
		result[key] = item
	}
	return result
}

func (store *bindingStore) replace(values map[interface{}]Value) {
	store.mu.Lock()
	defer store.mu.Unlock()

	replacement := make(map[interface{}]Value, len(values))
	for key, item := range values {
		replacement[key] = item
	}
	for key := range store.values {
		delete(store.values, key)
	}
	for key, item := range replacement {
		store.values[key] = item
	}
	store.gen.Add(1)
}

func (mapping *ObjMap) ensureStore() *bindingStore {
	mapping.storeOnce.Do(func() {
		if mapping.store == nil {
			mapping.store = newBindingStore(nil)
		}
	})
	return mapping.store
}

func (mapping *ObjMap) Get(key interface{}) (Value, bool) {
	return mapping.ensureStore().get(key)
}

func (mapping *ObjMap) Set(key interface{}, item Value) {
	mapping.ensureStore().set(key, item)
}

func (mapping *ObjMap) Delete(key interface{}) bool {
	return mapping.ensureStore().delete(key)
}

// Swap grava item em key e devolve o ocupante anterior (e se existia) numa
// unica secao critica — ver bindingStore.swap.
func (mapping *ObjMap) Swap(key interface{}, item Value) (Value, bool) {
	return mapping.ensureStore().swap(key, item)
}

func (mapping *ObjMap) Len() int {
	return mapping.ensureStore().count()
}

func (mapping *ObjMap) Snapshot() map[interface{}]Value {
	return mapping.ensureStore().snapshot()
}

func (mapping *ObjMap) Replace(values map[interface{}]Value) {
	mapping.ensureStore().replace(values)
}
