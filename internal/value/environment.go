package value

type GlobalEnvironment struct {
	local  *bindingStore
	parent *GlobalEnvironment
}

func NewGlobalEnvironment(parent *GlobalEnvironment) *GlobalEnvironment {
	return &GlobalEnvironment{local: newBindingStore(nil), parent: parent}
}

func NewGlobalEnvironmentFrom(values map[string]Value, parent *GlobalEnvironment) *GlobalEnvironment {
	environment := NewGlobalEnvironment(parent)
	for name, item := range values {
		environment.SetLocal(name, item)
	}
	return environment
}

func (environment *GlobalEnvironment) GetLocal(name string) (Value, bool) {
	if environment == nil {
		return Value{}, false
	}
	return environment.local.get(name)
}

func (environment *GlobalEnvironment) Resolve(name string) (Value, bool) {
	for current := environment; current != nil; current = current.parent {
		if item, ok := current.GetLocal(name); ok {
			return item, true
		}
	}
	return Value{}, false
}

func (environment *GlobalEnvironment) SetLocal(name string, item Value) {
	environment.local.set(name, item)
}

func (environment *GlobalEnvironment) DefineLocalIfAbsent(name string, item Value) bool {
	return environment.local.defineIfAbsent(name, item)
}

func (environment *GlobalEnvironment) ResolveOwner(name string) (*GlobalEnvironment, bool) {
	for current := environment; current != nil; current = current.parent {
		if _, ok := current.GetLocal(name); ok {
			return current, true
		}
	}
	return nil, false
}

func (environment *GlobalEnvironment) LocalSnapshot() map[string]Value {
	result := make(map[string]Value)
	for key, item := range environment.local.snapshot() {
		if name, ok := key.(string); ok {
			result[name] = item
		}
	}
	return result
}

func (environment *GlobalEnvironment) ReplaceLocal(values map[string]Value) {
	replacement := make(map[interface{}]Value, len(values))
	for name, item := range values {
		replacement[name] = item
	}
	environment.local.replace(replacement)
}

func (environment *GlobalEnvironment) ExportMap() Value {
	return Value{Type: VAL_OBJ, kind: objKindMap, Obj: &ObjMap{store: environment.local}}
}

// Generation soma as gerações dos stores da cadeia de ambientes. Qualquer
// escrita em qualquer nível (inclusive via ObjMap exportado que compartilha o
// store — ver ExportMap) avança a soma; os contadores só incrementam, então a
// soma é estritamente crescente e serve de token de invalidação de cache.
func (environment *GlobalEnvironment) Generation() uint64 {
	var sum uint64
	for current := environment; current != nil; current = current.parent {
		sum += current.local.gen.Load()
	}
	return sum
}
