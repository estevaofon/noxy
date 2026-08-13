package value

import "sync"

type bindingStore struct {
	mu     sync.RWMutex
	values map[interface{}]Value
}

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
}

func (store *bindingStore) defineIfAbsent(key interface{}, item Value) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.values[key]; exists {
		return false
	}
	store.values[key] = item
	return true
}

func (store *bindingStore) delete(key interface{}) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.values[key]; !exists {
		return false
	}
	delete(store.values, key)
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
}

func (mapping *ObjMap) ensureStore() *bindingStore {
	mapping.storeOnce.Do(func() {
		mapping.store = newBindingStore(mapping.Data)
		mapping.Data = mapping.store.values
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

func (mapping *ObjMap) Len() int {
	return len(mapping.Snapshot())
}

func (mapping *ObjMap) Snapshot() map[interface{}]Value {
	return mapping.ensureStore().snapshot()
}

func (mapping *ObjMap) Replace(values map[interface{}]Value) {
	mapping.ensureStore().replace(values)
}
