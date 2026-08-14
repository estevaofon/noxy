# Runtime Context Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace VM-capturing natives, raw global/module maps, and split resource ownership with a concurrency-safe runtime foundation that preserves existing Noxy programs and legacy Go native APIs.

**Architecture:** Native values carry either the legacy handler or a contextual handler invoked with the active VM. `value.GlobalEnvironment` and synchronized `ObjMap` storage provide stable global identity and live module exports, while `SharedState` owns coordinated module loading and per-resource registries. Registry locks protect lookup only; each resource entry owns operation and close synchronization.

**Tech Stack:** Go 1.24.0, toolchain Go 1.24.11, Noxy bytecode VM, Go `sync` primitives, `go test -race`.

## Global Constraints

- Do not change Noxy syntax or any existing Noxy builtin signature in this subproject.
- Preserve the Go signatures of `value.NewNative`, `value.NewNativeWithSignature`, `VM.DefineNative`, and `VM.DefineNativeWithSignature`.
- Keep current `net_select`, blocking socket, builtin result, and detached `spawn` semantics; only move ownership and synchronization.
- Do not implement cross-VM value validation, supervised tasks, `defer`, signals, substring changes, or HTTP changes here.
- Use test-first red-green-refactor for every behavior.
- Never hold an environment, module-cache, or registry lock while executing Noxy/native code or blocking I/O.
- Individual map operations are synchronized; compound Noxy operations are not made atomic.
- Keep the worktree's unrelated user changes untouched.

---

## Planned File Structure

### New files

- `internal/value/native.go` — dual native ABI and invocation.
- `internal/value/native_test.go` — native ABI compatibility and invalid-state tests.
- `internal/value/map.go` — synchronized map storage and `ObjMap` API.
- `internal/value/map_test.go` — map operation and race tests.
- `internal/value/environment.go` — hierarchical `GlobalEnvironment`.
- `internal/value/environment_test.go` — resolution, shadowing, owner, and live-export tests.
- `internal/vm/module_cache.go` — single-flight module cache and dependency graph.
- `internal/vm/module_cache_test.go` — concurrent load and cross-flight cycle tests.
- `internal/vm/resources.go` — handle registries and resource entry types.
- `internal/vm/resources_test.go` — registry/lifecycle tests.
- `internal/vm/runtime_context_test.go` — active-VM dispatch and initialization tests.

### Modified files

- `internal/value/value.go` — use environments, new map/native types, and global-reference owners.
- `internal/plugin/plugin.go` — consume map snapshots.
- `internal/compiler/compiler.go` — construct unbound functions with environment metadata.
- `internal/vm/vm.go` — new shared state, root environment, initialization, and native registration.
- `internal/vm/calls.go` — invoke natives contextually and copy synchronized maps.
- `internal/vm/call_validation.go` — callable validation through `ObjNative.IsCallable`.
- `internal/vm/executor.go` — environment-based opcodes, map API, imports, and copy-out execution.
- `internal/vm/references.go` — environment owners and synchronized map references.
- `internal/vm/modules.go` — resolve sources, use live environments, and call module cache.
- `internal/vm/runtime_type_validation.go` — inspect map snapshots.
- `internal/vm/json_population.go` — snapshot/replace map population.
- `internal/vm/json_strict.go` — snapshot map serialization.
- `internal/vm/builtins_collections.go` — synchronized maps and contextual reference operations.
- `internal/vm/builtins_json.go` — synchronized maps and contextual `json_loads`.
- `internal/vm/builtins_io.go` — shared file resources.
- `internal/vm/builtins_net.go` — shared synchronized listener/socket resources.
- `internal/vm/builtins_sqlite.go` — shared database/statement resources.
- `internal/vm/builtins_concurrency.go` — active VM context without changing spawn semantics.
- `internal/vm/builtins_sys.go` — active VM for dynamic plugin registration.
- `internal/vm/builtins.go` — one-time builtin initialization.
- `internal/vm/vm_test_helpers_test.go` — invoke builtin through active VM.
- Every `internal/vm/*_test.go` returned by `rg -l '\.Data\b|\.Globals\b|openFiles|NetListeners|NetConns|DbHandles|StmtHandles|StmtParams' internal/vm -g '*_test.go'` — replace direct fields with supported APIs in the task that removes each field.
- `internal/vm/architecture_test.go` — enforce the new boundaries.
- `docs/CONCURRENCY.md`, `docs/NOXY_LANGUAGE_SPEC.md`, `CHANGELOG.md` — document guarantees and compatibility.

---

### Task 1: Add the dual native ABI and active-context invocation

**Files:**
- Create: `internal/value/native.go`
- Create: `internal/value/native_test.go`
- Create: `internal/vm/runtime_context_test.go`
- Modify: `internal/value/value.go`
- Modify: `internal/vm/calls.go`
- Modify: `internal/vm/call_validation.go`
- Modify: `internal/vm/runtime_type_validation.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/vm_test_helpers_test.go`
- Modify: `internal/vm/builtins_net_test.go`

**Interfaces:**
- Produces: `value.NativeContext`, `value.ContextualNativeFunc`, `(*ObjNative).Invoke`, `(*ObjNative).IsCallable`, contextual constructors, and contextual VM registration methods.
- Preserves: all existing legacy native constructors and registration methods.

- [ ] **Step 1: Write failing value-level ABI tests**

```go
package value

import (
	"errors"
	"testing"
)

type testNativeContext struct{}

func (*testNativeContext) IsNativeContext() {}

func TestObjNativeInvokeSupportsLegacyAndContextualHandlers(t *testing.T) {
	ctx := &testNativeContext{}
	legacy := NewNative("legacy", func(args []Value) Value { return args[0] })
	got, err := legacy.Obj.(*ObjNative).Invoke(ctx, []Value{NewInt(7)})
	if err != nil || got.AsInt != 7 {
		t.Fatalf("legacy invoke=(%v, %v), want (7, nil)", got, err)
	}

	contextual := NewContextualNative("contextual", func(actual NativeContext, args []Value) (Value, error) {
		if actual != ctx {
			return NewNull(), errors.New("wrong context")
		}
		return args[0], nil
	})
	got, err = contextual.Obj.(*ObjNative).Invoke(ctx, []Value{NewInt(9)})
	if err != nil || got.AsInt != 9 {
		t.Fatalf("contextual invoke=(%v, %v), want (9, nil)", got, err)
	}
}

func TestObjNativeInvokeRejectsInvalidHandlerConfigurations(t *testing.T) {
	ctx := &testNativeContext{}
	tests := []*ObjNative{
		{Name: "missing"},
		{Name: "both", Fn: func([]Value) Value { return NewNull() }, Contextual: func(NativeContext, []Value) (Value, error) { return NewNull(), nil }},
	}
	for _, native := range tests {
		if native.IsCallable() {
			t.Fatalf("%s unexpectedly callable", native.Name)
		}
		if _, err := native.Invoke(ctx, nil); err == nil {
			t.Fatalf("%s invoke succeeded", native.Name)
		}
	}
}
```

- [ ] **Step 2: Run the tests and verify RED**

Run: `go test ./internal/value -run 'TestObjNativeInvoke' -v`
Expected: build failure because contextual native types and `Invoke` do not exist.

- [ ] **Step 3: Implement the dual ABI**

Move the native definitions from `value.go` to `native.go` and implement:

```go
package value

import "fmt"

type NativeContext interface {
	IsNativeContext()
}

type NativeFunc func(args []Value) Value
type ContextualNativeFunc func(context NativeContext, args []Value) (Value, error)

type ObjNative struct {
	Name       string
	Fn         NativeFunc
	Contextual ContextualNativeFunc
	Signature  *NativeSignature
}

func (native *ObjNative) IsCallable() bool {
	return native != nil && (native.Fn == nil) != (native.Contextual == nil)
}

func (native *ObjNative) Invoke(context NativeContext, args []Value) (Value, error) {
	if native == nil {
		return NewNull(), fmt.Errorf("invalid native function")
	}
	if !native.IsCallable() {
		return NewNull(), fmt.Errorf("native '%s' must define exactly one handler", native.Name)
	}
	if native.Contextual != nil {
		if context == nil {
			return NewNull(), fmt.Errorf("native '%s' requires a runtime context", native.Name)
		}
		return native.Contextual(context, args)
	}
	return native.Fn(args), nil
}

func NewNative(name string, fn NativeFunc) Value {
	return Value{Type: VAL_NATIVE, Obj: &ObjNative{Name: name, Fn: fn}}
}

func NewNativeWithSignature(name string, signature NativeSignature, fn NativeFunc) Value {
	return Value{Type: VAL_NATIVE, Obj: &ObjNative{Name: name, Fn: fn, Signature: &signature}}
}

func NewContextualNative(name string, fn ContextualNativeFunc) Value {
	return Value{Type: VAL_NATIVE, Obj: &ObjNative{Name: name, Contextual: fn}}
}

func NewContextualNativeWithSignature(name string, signature NativeSignature, fn ContextualNativeFunc) Value {
	return Value{Type: VAL_NATIVE, Obj: &ObjNative{Name: name, Contextual: fn, Signature: &signature}}
}
```

- [ ] **Step 4: Route VM calls through `Invoke`**

Add to `VM`:

```go
func (*VM) IsNativeContext() {}

func nativeVM(context value.NativeContext) (*VM, error) {
	machine, ok := context.(*VM)
	if !ok || machine == nil {
		return nil, fmt.Errorf("invalid VM native context")
	}
	return machine, nil
}
```

Add `DefineContextualNative` and `DefineContextualNativeWithSignature` beside the legacy methods. In `callValue`, keep existing signature validation and argument-copy preparation, then replace direct invocation with:

```go
result, err := native.Invoke(vm, args)
if err != nil {
	return false, vm.runtimeError(c, ip, "%s", err)
}
vm.stackTop -= argCount + 1
vm.push(result)
return true, nil
```

Change every callable check from `native.Fn != nil` to `native.IsCallable()`.

- [ ] **Step 5: Make test helpers invoke with the selected VM**

```go
func callBuiltin(t *testing.T, machine *VM, name string, args ...value.Value) value.Value {
	t.Helper()
	result, err := requireBuiltin(t, machine, name).Invoke(machine, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return result
}
```

Apply the same `Invoke(machine, args)` rule in bounded helpers.

- [ ] **Step 6: Add and run an active-VM regression test**

```go
func TestContextualNativeReceivesCallingVM(t *testing.T) {
	parent := NewWithConfig(VMConfig{RootPath: "parent"})
	child := NewWithShared(parent.shared, VMConfig{RootPath: "child"})
	parent.DefineContextualNative("active_root", func(context value.NativeContext, _ []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewString(machine.Config.RootPath), nil
	})
	assertBuiltinValue(t, callBuiltin(t, child, "active_root"), value.NewString("child"))
}
```

Run: `go test ./internal/value ./internal/vm -run 'TestObjNativeInvoke|TestContextualNativeReceivesCallingVM' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```powershell
git add internal/value/native.go internal/value/native_test.go internal/value/value.go internal/vm/calls.go internal/vm/call_validation.go internal/vm/runtime_type_validation.go internal/vm/vm.go internal/vm/vm_test_helpers_test.go internal/vm/builtins_net_test.go internal/vm/runtime_context_test.go
git commit -m "refactor(vm): add contextual native invocation"
```

---

### Task 2: Add synchronized `ObjMap` storage without breaking the build

**Files:**
- Create: `internal/value/map.go`
- Create: `internal/value/map_test.go`
- Modify: `internal/value/value.go`

**Interfaces:**
- Produces: `ObjMap.Get`, `Set`, `Delete`, `Len`, `Snapshot`, and `Replace`.
- Temporary compatibility: keep `Data` as an alias only until Task 3 migrates every caller.

- [ ] **Step 1: Write failing synchronized-map tests**

```go
func TestObjMapOperationsUseSnapshots(t *testing.T) {
	mapping := NewMap().Obj.(*ObjMap)
	mapping.Set("answer", NewInt(42))
	if got, ok := mapping.Get("answer"); !ok || got.AsInt != 42 {
		t.Fatalf("answer=(%v,%t)", got, ok)
	}
	snapshot := mapping.Snapshot()
	snapshot["answer"] = NewInt(0)
	got, _ := mapping.Get("answer")
	if got.AsInt != 42 {
		t.Fatal("snapshot mutated live map")
	}
	if !mapping.Delete("answer") || mapping.Len() != 0 {
		t.Fatal("delete did not remove key")
	}
}

func TestObjMapConcurrentSetAndSnapshot(t *testing.T) {
	mapping := NewMap().Obj.(*ObjMap)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(2)
		go func(id int) {
			defer workers.Done()
			<-start
			for n := 0; n < 100; n++ {
				mapping.Set(int64(id*100+n), NewInt(int64(n)))
			}
		}(i)
		go func() {
			defer workers.Done()
			<-start
			for n := 0; n < 100; n++ {
				_ = mapping.Snapshot()
			}
		}()
	}
	close(start)
	workers.Wait()
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/value -run 'TestObjMap' -v`
Expected: build failure because the methods do not exist.

- [ ] **Step 3: Implement `bindingStore` and the map methods**

```go
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
	store.values = make(map[interface{}]Value, len(values))
	for key, item := range values {
		store.values[key] = item
	}
	store.mu.Unlock()
}

type ObjMap struct {
	Data        map[interface{}]Value // removed in Task 3
	store       *bindingStore
	storeOnce   sync.Once
	RuntimeType atomic.Pointer[RuntimeTypeInfo]
}

func (mapping *ObjMap) ensureStore() *bindingStore {
	mapping.storeOnce.Do(func() { mapping.store = newBindingStore(mapping.Data) })
	return mapping.store
}

func (mapping *ObjMap) Get(key interface{}) (Value, bool) { return mapping.ensureStore().get(key) }
func (mapping *ObjMap) Set(key interface{}, item Value) { mapping.ensureStore().set(key, item) }
func (mapping *ObjMap) Delete(key interface{}) bool { return mapping.ensureStore().delete(key) }
func (mapping *ObjMap) Len() int { return len(mapping.Snapshot()) }
func (mapping *ObjMap) Snapshot() map[interface{}]Value { return mapping.ensureStore().snapshot() }
func (mapping *ObjMap) Replace(values map[interface{}]Value) { mapping.ensureStore().replace(values) }
```

Constructors must initialize `Data` and `store` with the same initial map during this compatibility task.

- [ ] **Step 4: Verify GREEN under the race detector**

Run: `go test -race ./internal/value -run 'TestObjMap' -v`
Expected: PASS with no race report.

- [ ] **Step 5: Commit**

```powershell
git add internal/value/map.go internal/value/map_test.go internal/value/value.go
git commit -m "refactor(value): add synchronized map storage"
```

---

### Task 3: Migrate all runtime map access and remove `ObjMap.Data`

**Files:**
- Modify: `internal/value/map.go`
- Modify: `internal/value/value.go`
- Modify: `internal/plugin/plugin.go`
- Modify: `internal/vm/builtins_collections.go`
- Modify: `internal/vm/builtins_json.go`
- Modify: `internal/vm/builtins_net.go`
- Modify: `internal/vm/calls.go`
- Modify: `internal/vm/executor.go`
- Modify: `internal/vm/json_population.go`
- Modify: `internal/vm/json_strict.go`
- Modify: `internal/vm/references.go`
- Modify: `internal/vm/runtime_type_validation.go`
- Modify: every test file returned by `rg -l '\.Data\b' internal/vm -g '*_test.go'`.

**Interfaces:**
- Consumes: synchronized `ObjMap` API from Task 2.
- Produces: no mutable raw map field anywhere in `ObjMap` consumers.

- [ ] **Step 1: Add an architectural test that currently fails**

Add to `architecture_test.go`:

```go
func TestRuntimeDoesNotAccessObjMapDataDirectly(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "architecture_test.go" {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(source, []byte(".Data")) {
			t.Errorf("%s accesses ObjMap.Data directly", file)
		}
	}
}
```

Run: `go test ./internal/vm -run TestRuntimeDoesNotAccessObjMapDataDirectly -v`
Expected: FAIL listing current direct-access files.

- [ ] **Step 2: Apply the exact production-code mapping**

Use these replacements in every production occurrence:

| Old operation | New operation |
|---|---|
| `mapping.Data[key]` read | `mapping.Get(key)` |
| `mapping.Data[key] = item` | `mapping.Set(key, item)` |
| `delete(mapping.Data, key)` | `mapping.Delete(key)` |
| `len(mapping.Data)` | `mapping.Len()` |
| `for key, item := range mapping.Data` | `for key, item := range mapping.Snapshot()` |
| replace whole `mapping.Data` | `mapping.Replace(newValues)` |
| shallow-copy `mapping.Data` | use `mapping.Snapshot()` |

For absent indexed reads, preserve `null` behavior:

```go
item, exists := mapping.Get(key)
if !exists {
	item = value.NewNull()
}
```

For map references, return a setter that calls `mapping.Set(key, updated)`. For JSON population, build a new map from `Snapshot`, validate it completely, and call `Replace` only after validation succeeds.

- [ ] **Step 3: Migrate tests to supported helpers**

Add test-only helpers:

```go
func setTestMap(mapping *value.ObjMap, key interface{}, item value.Value) {
	mapping.Set(key, item)
}

func requireTestMapValue(t *testing.T, mapping *value.ObjMap, key interface{}) value.Value {
	t.Helper()
	item, ok := mapping.Get(key)
	if !ok {
		t.Fatalf("map is missing key %v", key)
	}
	return item
}
```

Replace test mutation, lookup, iteration, and length with `Set`, `Get`, `Snapshot`, and `Len`.

- [ ] **Step 4: Remove compatibility storage**

Change `ObjMap` to:

```go
type ObjMap struct {
	store       *bindingStore
	storeOnce   sync.Once
	RuntimeType atomic.Pointer[RuntimeTypeInfo]
}
```

Make `ensureStore` preserve an injected live store:

```go
func (mapping *ObjMap) ensureStore() *bindingStore {
	mapping.storeOnce.Do(func() {
		if mapping.store == nil {
			mapping.store = newBindingStore(nil)
		}
	})
	return mapping.store
}
```

`NewMap` creates `&ObjMap{store: newBindingStore(nil)}` and calls `ensureStore()` before returning. `NewMapWithData` creates an empty map, converts the input keys, calls `Replace`, and returns it. `String` and `Format` iterate over `Snapshot()`.

- [ ] **Step 5: Verify no direct field remains**

Run: `rg -n '\.Data\b' internal -g '*.go'`
Expected: no matches.

Run: `go test -race ./internal/value ./internal/plugin ./internal/vm`
Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/value internal/plugin internal/vm
git commit -m "refactor(value): encapsulate synchronized maps"
```

---

### Task 4: Add hierarchical `GlobalEnvironment`

**Files:**
- Create: `internal/value/environment.go`
- Create: `internal/value/environment_test.go`
- Modify: `internal/value/map.go`

**Interfaces:**
- Produces: environment resolution, ownership, snapshots, replacement, and live export maps.

- [ ] **Step 1: Write failing environment tests**

```go
func TestGlobalEnvironmentResolvesAndShadowsParent(t *testing.T) {
	root := NewGlobalEnvironment(nil)
	root.SetLocal("value", NewInt(1))
	child := NewGlobalEnvironment(root)
	if got, _ := child.Resolve("value"); got.AsInt != 1 {
		t.Fatalf("inherited value=%v", got)
	}
	owner, ok := child.ResolveOwner("value")
	if !ok || owner != root {
		t.Fatal("wrong inherited owner")
	}
	child.SetLocal("value", NewInt(2))
	owner, _ = child.ResolveOwner("value")
	if owner != child {
		t.Fatal("shadow did not become local")
	}
}

func TestGlobalEnvironmentExportsAreLiveAndLocalOnly(t *testing.T) {
	root := NewGlobalEnvironment(nil)
	root.SetLocal("builtin", NewInt(1))
	module := NewGlobalEnvironment(root)
	module.SetLocal("answer", NewInt(41))
	exports := module.ExportMap().Obj.(*ObjMap)
	module.SetLocal("answer", NewInt(42))
	if got, _ := exports.Get("answer"); got.AsInt != 42 {
		t.Fatalf("live export=%v", got)
	}
	if _, inherited := exports.Get("builtin"); inherited {
		t.Fatal("exports leaked parent binding")
	}
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/value -run TestGlobalEnvironment -v`
Expected: build failure because `GlobalEnvironment` does not exist.

- [ ] **Step 3: Implement the environment**

```go
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
	return Value{Type: VAL_OBJ, Obj: &ObjMap{store: environment.local}}
}
```

- [ ] **Step 4: Run environment and map races**

Run: `go test -race ./internal/value -run 'TestGlobalEnvironment|TestObjMap' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/value/environment.go internal/value/environment_test.go internal/value/map.go
git commit -m "feat(value): add synchronized global environments"
```

---

### Task 5: Migrate VM execution, closures, globals, references, and map adapters

**Files:**
- Modify: `internal/value/value.go`
- Modify: `internal/compiler/compiler.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/executor.go`
- Modify: `internal/vm/calls.go`
- Modify: `internal/vm/references.go`
- Modify: `internal/vm/builtins_concurrency.go`
- Modify: `internal/vm/modules.go`
- Modify: `internal/vm/reference_ownership_test.go`
- Modify: `internal/vm/native_signatures_test.go`
- Modify: `internal/vm/module_exports_test.go`
- Modify: `internal/vm/architecture_test.go`

**Interfaces:**
- Consumes: `GlobalEnvironment`.
- Produces: environment-based frames/closures/functions/refs, `InterpretWithEnvironment`, and copy-in/copy-out `InterpretWithGlobals`.

- [ ] **Step 1: Write failing execution tests**

Add tests for an explicitly empty map and owner identity:

```go
func TestInterpretWithGlobalsUsesNonNilEmptyEnvironment(t *testing.T) {
	code := compileVMSource(t, "let answer: int = 42")
	globals := map[string]value.Value{}
	if err := New().InterpretWithGlobals(code, globals); err != nil {
		t.Fatal(err)
	}
	if got := globals["answer"]; got.Type != value.VAL_INT || got.AsInt != 42 {
		t.Fatalf("answer=%v", got)
	}
}

func TestModuleGlobalReferenceRetainsResolvedEnvironment(t *testing.T) {
	root := value.NewGlobalEnvironment(nil)
	root.SetLocal("root", value.NewInt(1))
	module := value.NewGlobalEnvironment(root)
	module.SetLocal("module", value.NewInt(2))
	machine := New()
	machine.shared.Root = root
	ref := &value.ObjRef{RefType: value.REF_GLOBAL, Name: "module", GlobalOwner: module}
	if err := machine.storeGlobalReferenceValue(ref, value.NewInt(3)); err != nil {
		t.Fatal(err)
	}
	got, _ := module.GetLocal("module")
	if got.AsInt != 3 {
		t.Fatalf("module value=%v", got)
	}
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/vm -run 'TestInterpretWithGlobalsUsesNonNilEmptyEnvironment|TestModuleGlobalReferenceRetainsResolvedEnvironment' -v`
Expected: build failure because VM objects still use raw maps.

- [ ] **Step 3: Change value and frame ownership types**

Use these exact fields:

```go
type ObjFunction struct {
	Name string
	Arity int
	UpvalueCount int
	Params []ParamInfo
	Chunk interface{}
	Environment *GlobalEnvironment
	RuntimeType *RuntimeTypeInfo
}

type ObjClosure struct {
	Function *ObjFunction
	Upvalues []*ObjUpvalue
	Environment *GlobalEnvironment
}

type ObjRef struct {
	// existing metadata
	GlobalOwner *GlobalEnvironment
	// existing non-global target fields
}

type CallFrame struct {
	Closure *value.ObjClosure
	IP int
	Slots int
	Environment *value.GlobalEnvironment
}
```

Change `NewFunction`'s final argument from `map[string]Value` to `*GlobalEnvironment`, then update its compiler, executor, module, malformed-value-test, and closure call sites reported by `rg -n 'NewFunction\(' internal -g '*.go'` to pass either the current environment or `nil` for unbound compiler constants.

- [ ] **Step 4: Add root and interpretation entry points**

`SharedState` gains `Root *value.GlobalEnvironment`. Implement:

```go
func (vm *VM) Interpret(c *chunk.Chunk) error {
	return vm.InterpretWithEnvironment(c, vm.shared.Root)
}

func (vm *VM) InterpretWithGlobals(c *chunk.Chunk, globals map[string]value.Value) (err error) {
	if globals == nil {
		return vm.InterpretWithEnvironment(c, vm.shared.Root)
	}
	environment := value.NewGlobalEnvironmentFrom(globals, vm.shared.Root)
	defer func() {
		for name := range globals {
			delete(globals, name)
		}
		for name, item := range environment.LocalSnapshot() {
			globals[name] = item
		}
	}()
	return vm.InterpretWithEnvironment(c, environment)
}

func (vm *VM) InterpretWithEnvironment(c *chunk.Chunk, environment *value.GlobalEnvironment) error {
	if environment == nil {
		return fmt.Errorf("interpret requires a global environment")
	}
	scriptFunction := &value.ObjFunction{Name: "script", Arity: 0, Chunk: c, Environment: environment}
	scriptClosure := &value.ObjClosure{Function: scriptFunction, Upvalues: []*value.ObjUpvalue{}, Environment: environment}
	vm.stackTop = 0
	vm.push(value.Value{Type: value.VAL_FUNCTION, Obj: scriptClosure})
	frame := &CallFrame{Closure: scriptClosure, IP: 0, Slots: 1, Environment: environment}
	vm.frames[0] = frame
	vm.frameCount = 1
	vm.currentFrame = frame
	return vm.run(1)
}
```

`GetGlobal`, `SetGlobal`, and native definition delegate to `shared.Root`; definition uses `DefineLocalIfAbsent`.

- [ ] **Step 5: Migrate global and closure opcodes**

Implement the opcode rules exactly:

```go
// OP_GET_GLOBAL
item, ok := frame.Environment.Resolve(name)

// OP_SET_GLOBAL
frame.Environment.SetLocal(name, vm.peek(0))

// OP_REF_GLOBAL
owner, ok := frame.Environment.ResolveOwner(name)
vm.push(value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{
	RefType: value.REF_GLOBAL,
	Name: name,
	GlobalOwner: owner,
}})
```

Bind function constants and closures to `frame.Environment`. Construct call frames from `closure.Environment`. Change global reference storage to `GlobalOwner.GetLocal`/`SetLocal`.

- [ ] **Step 6: Use live module environments before adding cache coordination**

In every embedded/file module path:

```go
moduleEnvironment := value.NewGlobalEnvironment(vm.shared.Root)
moduleFunction.Environment = moduleEnvironment
moduleClosure.Environment = moduleEnvironment
vm.push(value.Value{Type: value.VAL_FUNCTION, Obj: moduleClosure})
if ok, err := vm.callValue(vm.peek(0), 0, nil, 0); !ok { return value.NewNull(), err }
startFrameCount := vm.frameCount
if err := vm.run(startFrameCount); err != nil { return value.NewNull(), err }
vm.pop()
return moduleEnvironment.ExportMap(), nil
```

Directory modules use a standalone child environment, insert child exports with `SetLocal`, and return `ExportMap`.

- [ ] **Step 7: Add architectural assertions and verify**

Update architecture tests to reject `Globals map` and `GlobalOwner *map`. Run:

```powershell
rg -n '\.Globals\b|Globals:|GlobalOwner \*map' internal/value internal/vm -g '*.go'
go test -race ./internal/value ./internal/vm
```

Expected: `rg` finds no obsolete ownership fields; tests PASS.

- [ ] **Step 8: Commit**

```powershell
git add internal/value/value.go internal/compiler/compiler.go internal/vm
git commit -m "refactor(vm): use synchronized global environments"
```

---

### Task 6: Coordinate module loading and detect concurrent cycles

**Files:**
- Create: `internal/vm/module_cache.go`
- Create: `internal/vm/module_cache_test.go`
- Modify: `internal/vm/modules.go`
- Modify: `internal/vm/executor.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/module_exports_test.go`

**Interfaces:**
- Produces: canonical `moduleKey`, `moduleCache.Do`, per-VM load stack, successful-result caching, failure retry, and dependency-cycle errors.

- [ ] **Step 1: Write deterministic failing cache tests**

Test one initializer with channels:

```go
func TestModuleCacheInitializesKeyOnce(t *testing.T) {
	cache := newModuleCache()
	key := moduleKey{Root: "root", Name: "module"}
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	load := func() (value.Value, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return value.NewInt(42), nil
	}
	results := make(chan value.Value, 2)
	for i := 0; i < 2; i++ {
		go func() {
			got, err := cache.Do(key, nil, load)
			if err != nil { t.Error(err); return }
			results <- got
		}()
	}
	<-entered
	close(release)
	<-results
	<-results
	if calls.Load() != 1 { t.Fatalf("loads=%d", calls.Load()) }
}
```

Add a cross-flight graph test that establishes `A -> B`, then attempts `B -> A`, and requires an error containing `A -> B -> A` without waiting.

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/vm -run TestModuleCache -v`
Expected: build failure because the cache does not exist.

- [ ] **Step 3: Implement cache state and graph**

Use these types:

```go
type moduleKey struct { Root, Name string }
type moduleEntry struct {
	done chan struct{}
	result value.Value
	err error
}
type moduleCache struct {
	mu sync.Mutex
	entries map[moduleKey]*moduleEntry
	dependencies map[moduleKey]map[moduleKey]struct{}
}
```

`Do(key, parent, load)` must:

1. return ready cached results immediately;
2. install `loading` for the first caller;
3. add `parent -> key` before waiting or loading;
4. run DFS from `key` to `parent` before adding the edge and return a formatted cycle if reachable;
5. wait on `entry.done` outside the lock;
6. run `load` outside the lock for the owner;
7. publish result/error, close `done`, and remove failed entries;
8. remove the dependency edge on every return path.

- [ ] **Step 4: Split module resolution from cached initialization**

Refactor `modules.go` into:

```go
func (vm *VM) resolveModule(name string) (resolvedModule, error)
func (vm *VM) loadResolvedModule(source resolvedModule) (value.Value, error)
func (vm *VM) loadModule(name string) (value.Value, error)
```

`resolvedModule` contains canonical identity, kind (embedded/file/directory), and path/content. The cache key includes canonical `RootPath`, current `NOXY_PATH`, and resolved module name. `loadModule` passes the top of the per-VM module-load stack as parent, pushes the owner key only while executing `loadResolvedModule`, and pops it with `defer`.

Change `OP_IMPORT` to call only `loadModule`; remove separate `GetModule`/`SetModule` calls.

- [ ] **Step 5: Add integration tests for concurrent import and direct cycles**

Create `counter.nx` under `t.TempDir()` with `test_module_init()`. Compile `use counter` once, create two VMs with the same `SharedState` and root, and register:

```go
var initializations atomic.Int32
parent.DefineNative("test_module_init", func([]value.Value) value.Value {
	initializations.Add(1)
	return value.NewNull()
})
```

Run the compiled import simultaneously in both VMs behind a start channel, collect two errors, and assert both nil and `initializations.Load() == 1`.

For direct cycles, write `a.nx` containing `use b` and `b.nx` containing `use a`; interpret `use a` and assert the error contains `a -> b -> a`. The lower-level cache test from Step 1 remains the deterministic cross-flight `A/B` cycle test.

Run: `go test -race ./internal/vm -run 'TestModuleCache|TestConcurrentModule|Test.*ImportCycle' -v`
Expected: PASS with no deadlock or race.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm/module_cache.go internal/vm/module_cache_test.go internal/vm/modules.go internal/vm/executor.go internal/vm/vm.go internal/vm/module_exports_test.go
git commit -m "fix(vm): coordinate concurrent module loading"
```

---

### Task 7: Introduce shared handle registries and resource entries

**Files:**
- Create: `internal/vm/resources.go`
- Create: `internal/vm/resources_test.go`
- Modify: `internal/vm/vm.go`

**Interfaces:**
- Produces: `handleRegistry[T]`, `FileResource`, `ListenerResource`, `SocketResource`, `DatabaseResource`, and `StatementResource`.

- [ ] **Step 1: Write failing registry lifecycle tests**

```go
func TestHandleRegistryUsesMonotonicNonReusableHandles(t *testing.T) {
	registry := newHandleRegistry[string]()
	first := registry.add("first")
	if _, ok := registry.remove(first); !ok { t.Fatal("remove failed") }
	second := registry.add("second")
	if second <= first { t.Fatalf("handles %d then %d", first, second) }
	if _, ok := registry.get(first); ok { t.Fatal("removed handle resolved") }
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/vm -run TestHandleRegistry -v`
Expected: build failure.

- [ ] **Step 3: Implement registry and resource types**

```go
type handleRegistry[T any] struct {
	mu sync.RWMutex
	next int
	items map[int]T
}

func newHandleRegistry[T any]() *handleRegistry[T] {
	return &handleRegistry[T]{next: 1, items: make(map[int]T)}
}

func (registry *handleRegistry[T]) add(item T) int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	handle := registry.next
	registry.next++
	registry.items[handle] = item
	return handle
}

func (registry *handleRegistry[T]) get(handle int) (T, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	item, ok := registry.items[handle]
	return item, ok
}

func (registry *handleRegistry[T]) remove(handle int) (T, bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	item, ok := registry.items[handle]
	if ok { delete(registry.items, handle) }
	return item, ok
}
```

Define these entries:

```go
type FileResource struct {
	stateMu sync.Mutex
	operationMu sync.Mutex
	file *os.File
	closed bool
}

type ListenerResource struct {
	stateMu sync.Mutex
	acceptMu sync.Mutex
	listener net.Listener
	bufferedAccept net.Conn
	closed bool
}

type SocketResource struct {
	stateMu sync.Mutex
	readMu sync.Mutex
	writeMu sync.Mutex
	connection net.Conn
	bufferedRead []byte
	closed bool
}

type DatabaseResource struct {
	stateMu sync.Mutex
	database *sql.DB
	closed bool
}

type StatementResource struct {
	mu sync.Mutex
	statement *sql.Stmt
	parameters map[int]interface{}
	closed bool
}
```

- [ ] **Step 4: Replace `SharedState` resource fields**

```go
type SharedState struct {
	Root *value.GlobalEnvironment
	Modules *moduleCache
	Files *handleRegistry[*FileResource]
	Listeners *handleRegistry[*ListenerResource]
	Sockets *handleRegistry[*SocketResource]
	Databases *handleRegistry[*DatabaseResource]
	Statements *handleRegistry[*StatementResource]
	initOnce sync.Once
}
```

At this checkpoint, add the new registry fields alongside the old file/network/SQLite fields. Do not redirect existing builtins yet. Task 8 removes only `openFiles`/`nextFD`; Task 9 removes only old network fields; Task 10 removes only old SQLite fields. This keeps each commit buildable without an adapter that has two owners for the same resource.

- [ ] **Step 5: Run and commit**

Run: `go test -race ./internal/vm -run TestHandleRegistry -v`
Expected: PASS.

```powershell
git add internal/vm/resources.go internal/vm/resources_test.go internal/vm/vm.go
git commit -m "refactor(vm): add shared resource registries"
```

---

### Task 8: Move file handles into shared contextual resources

**Files:**
- Modify: `internal/vm/builtins_io.go`
- Modify: `internal/vm/builtins_io_test.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/resources.go`

**Interfaces:**
- Consumes: active contextual native and `SharedState.Files`.
- Produces: cross-VM file handle visibility and synchronized close/operation lifecycle.

- [ ] **Step 1: Write the failing cross-VM test**

```go
func TestFileHandleIsUsableAcrossSharedVMs(t *testing.T) {
	parent := New()
	child := NewWithShared(parent.shared, parent.Config)
	path := filepath.Join(t.TempDir(), "shared.txt")
	fileType := testFileDefinition()
	handle := callBuiltin(t, parent, "io_open", value.NewString(path), value.NewString("w"), fileType)
	callBuiltin(t, child, "io_write", handle, value.NewString("shared"))
	callBuiltin(t, child, "io_close", handle)
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "shared" {
		t.Fatalf("contents=%q err=%v", contents, err)
	}
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/vm -run TestFileHandleIsUsableAcrossSharedVMs -v`
Expected: FAIL because the child resolves the native captured by the parent but file state is VM-local.

- [ ] **Step 3: Add file resource operations**

Add:

```go
func (resource *FileResource) use(operation func(*os.File) value.Value) (value.Value, bool) {
	resource.operationMu.Lock()
	defer resource.operationMu.Unlock()
	resource.stateMu.Lock()
	if resource.closed || resource.file == nil {
		resource.stateMu.Unlock()
		return value.NewNull(), false
	}
	file := resource.file
	resource.stateMu.Unlock()
	return operation(file), true
}

func (resource *FileResource) close() error {
	resource.stateMu.Lock()
	if resource.closed || resource.file == nil {
		resource.stateMu.Unlock()
		return os.ErrClosed
	}
	resource.closed = true
	file := resource.file
	resource.stateMu.Unlock()
	return file.Close()
}
```

Builtin handlers obtain the resource with `machine.shared.Files.get(handle)` before calling `use`; close obtains it with `remove` before calling `close`. Registry locks are therefore released before `Read`, `Write`, `Stat`, or `Close`.

Close must remove the handle first, set `closed`, and call `file.Close()` without waiting for `operationMu`.

- [ ] **Step 4: Convert stateful IO natives**

Register `io_open`, both close variants, both write variants, both read variants, and `io_read_lines` with `DefineContextualNative`. Start each handler with:

```go
machine, err := nativeVM(context)
if err != nil { return value.NewNull(), err }
```

Return `(existingResult, nil)` for all existing operational outcomes. Leave path-only pure functions legacy. Remove `VM.openFiles` and `VM.nextFD`.

- [ ] **Step 5: Update cleanup and add a close/read lifecycle test**

Add `handleRegistry.snapshot()` for test cleanup. Replace each `machine.openFiles` cleanup/assertion with `machine.shared.Files.snapshot()` or `get`.

Add this deterministic underlying-close test:

```go
func TestFileCloseInterruptsBlockedReadWithoutRegistryDeadlock(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil { t.Fatal(err) }
	defer writer.Close()
	machine := New()
	resource := &FileResource{file: reader}
	handle := machine.shared.Files.add(resource)
	done := make(chan error, 1)
	go func() {
		resource.operationMu.Lock()
		defer resource.operationMu.Unlock()
		buffer := make([]byte, 1)
		_, readErr := resource.file.Read(buffer)
		done <- readErr
	}()
	removed, ok := machine.shared.Files.remove(handle)
	if !ok { t.Fatal("file handle disappeared") }
	removed.stateMu.Lock()
	removed.closed = true
	removed.stateMu.Unlock()
	if err := removed.file.Close(); err != nil { t.Fatal(err) }
	select {
	case readErr := <-done:
		if readErr == nil { t.Fatal("blocked read succeeded after close") }
	case <-time.After(statefulBuiltinTimeout):
		t.Fatal("close did not interrupt blocked read")
	}
}
```

Run: `go test -race ./internal/vm -run 'TestIO|TestFileHandle' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm/builtins_io.go internal/vm/builtins_io_test.go internal/vm/resources.go internal/vm/vm.go
git commit -m "fix(io): share file resources across VMs"
```

---

### Task 9: Move network state and select buffers into synchronized resources

**Files:**
- Modify: `internal/vm/builtins_net.go`
- Modify: `internal/vm/builtins_net_test.go`
- Modify: `internal/vm/resources.go`
- Modify: `internal/vm/vm.go`

**Interfaces:**
- Consumes: listener/socket registries and contextual native ABI.
- Produces: shared per-listener accepted-connection buffer and per-socket read buffer.
- Preserves: current sequential, read-only, consuming `net_select` behavior until the network subproject.

- [ ] **Step 1: Write failing cross-VM buffer and concurrent-select tests**

Use the existing `TestNetworkBuiltinsLoopbackLifecycle` setup through accept, then create `child := NewWithShared(machine.shared, machine.Config)`. Send `"x"` from the client, call `net_select` on the server socket through `machine`, and call `net_recv(server, 1)` through `child`; assert `ok=true`, `count=1`, and `data=b"x"`.

Add a second test with the same accepted server socket:

```go
start := make(chan struct{})
done := make(chan value.Value, 2)
for _, current := range []*VM{machine, child} {
	current := current
	go func() {
		<-start
		done <- callBuiltinWithinBound(t, current, "net_select",
			value.NewArray([]value.Value{server}), value.NewArray(nil), value.NewArray(nil), value.NewInt(5))
	}()
}
close(start)
for i := 0; i < 2; i++ {
	select {
	case <-done:
	case <-time.After(statefulBuiltinTimeout):
		t.Fatal("concurrent net_select did not complete")
	}
}
```

Send one byte before releasing `start`. The readiness counts may differ because readiness remains advisory; the assertions are bounded completion, valid `SelectResult`, and no race/panic.

- [ ] **Step 2: Verify RED under `-race`**

Run: `go test -race ./internal/vm -run 'TestNetSelectBufferIsSharedAcrossVMs|TestConcurrentNetSelect' -v`
Expected: race report or incorrect cross-VM receive with the current local buffers.

- [ ] **Step 3: Convert listener and socket registration**

`net_listen` stores `*ListenerResource`; `net_connect` and successful accept store `*SocketResource`. Every handler resolves the active VM context and uses its `SharedState` registries.

Use this lock protocol:

- accept/select-listener: `acceptMu`, then briefly `stateMu`, never registry lock;
- recv/select-connection: `readMu`, then briefly `stateMu`;
- send: `writeMu`, then briefly `stateMu`;
- close: registry removal, mark closed under `stateMu`, close underlying object without read/write/accept lock.

The current peeked byte is stored in `SocketResource.bufferedRead`; the accepted connection is stored in `ListenerResource.bufferedAccept`.

- [ ] **Step 4: Preserve socket result shapes**

Continue returning the existing socket and `NetResult` maps and existing error strings. Do not change timeout coercion, write/error sets, or `setblocking` behavior in this task.

Remove `NetListeners`, `NetConns`, `NetLock`, `NextNetID`, `netBufferedData`, and `netBufferedConns` from old locations.

- [ ] **Step 5: Verify targeted and full network tests**

Run: `go test -race ./internal/vm -run 'TestNetwork|TestNetSelect|TestConcurrentNetSelect' -v`
Expected: PASS without race reports.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm/builtins_net.go internal/vm/builtins_net_test.go internal/vm/resources.go internal/vm/vm.go
git commit -m "fix(net): synchronize shared socket state"
```

---

### Task 10: Move SQLite handles and statement parameters into resources

**Files:**
- Modify: `internal/vm/builtins_sqlite.go`
- Modify: `internal/vm/builtins_sqlite_test.go`
- Modify: `internal/vm/resources.go`
- Modify: `internal/vm/vm.go`

**Interfaces:**
- Produces: shared database/statement resources and per-statement parameter synchronization.

- [ ] **Step 1: Write failing cross-VM SQLite tests**

Use `newSQLiteTestDefinitions()` and `sqliteTemplate()`. Open `filepath.Join(t.TempDir(), "shared.sqlite")` with `parent`, create `entries(id INTEGER, name TEXT)` with `sqlite_exec`, and prepare `INSERT INTO entries VALUES (?, ?)` with `parent`. Bind index 1 through `parent` and index 2 through `child := NewWithShared(parent.shared, parent.Config)`, execute through `child`, then query through `parent` and assert one row containing `1, "shared"`.

For the parameter race, prepare one statement and release 16 goroutines through `start := make(chan struct{})`; even goroutines repeatedly call `sqlite_bind_int(statement, 1, i)` and odd goroutines repeatedly call `sqlite_bind_text(statement, 2, to_str(i))`, each 100 times. Wait with `sync.WaitGroup`; the assertion is completion and no `-race` report. Reset and finalize the statement after the workers finish.

- [ ] **Step 2: Verify RED**

Run: `go test -race ./internal/vm -run 'TestSQLiteHandlesAreSharedAcrossVMs|TestSQLiteStatementParametersConcurrent' -v`
Expected: race report for statement parameters or failure caused by old coarse state.

- [ ] **Step 3: Convert database natives**

Register all SQLite builtins contextually. `sqlite_open` adds `DatabaseResource`; database operations resolve it without holding registry locks. `sqlite_close` removes, marks closed, and closes outside the registry lock.

- [ ] **Step 4: Convert statement natives**

`sqlite_prepare` creates `StatementResource{statement: stmt, parameters: make(map[int]interface{})}`. Bind and reset serialize through the statement mutex. `sqlite_step_exec` locks the statement, copies parameters in ascending index order, clears the parameter map, unlocks, and then calls the underlying statement with that immutable slice. Finalize removes the entry, locks it, marks it closed, detaches the statement pointer, unlocks, and closes the detached statement. A bind/step that obtained the entry before finalize rechecks `closed` while holding the statement mutex and becomes the existing invalid/no-op result if closed.

Remove `DbHandles`, `StmtHandles`, `StmtParams`, counters, and `DbLock`.

- [ ] **Step 5: Verify**

Run: `go test -race ./internal/vm -run 'TestSQLite' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm/builtins_sqlite.go internal/vm/builtins_sqlite_test.go internal/vm/resources.go internal/vm/vm.go
git commit -m "fix(sqlite): synchronize shared database resources"
```

---

### Task 11: Convert remaining VM-dependent natives and initialize builtins once

**Files:**
- Modify: `internal/vm/builtins.go`
- Modify: `internal/vm/builtins_collections.go`
- Modify: `internal/vm/builtins_concurrency.go`
- Modify: `internal/vm/builtins_json.go`
- Modify: `internal/vm/builtins_sys.go`
- Modify: `internal/vm/builtins_registry_test.go`
- Modify: `internal/vm/builtins_concurrency_test.go`
- Modify: `internal/vm/native_signatures_test.go`
- Modify: `internal/vm/vm.go`
- Modify: `internal/vm/runtime_context_test.go`

**Interfaces:**
- Produces: no standard native that captures a bootstrap VM while accessing VM state; one-time shared builtin registration.
- Preserves: current detached spawn execution behavior, including its existing argument/global semantics for the later spawn subproject.

- [ ] **Step 1: Write failing one-time initialization tests**

```go
func TestSharedVMsReuseBuiltinValuesButInvokeActiveContext(t *testing.T) {
	parent := NewWithConfig(VMConfig{RootPath: "parent"})
	child := NewWithShared(parent.shared, VMConfig{RootPath: "child"})
	parentSpawn, _ := parent.GetGlobal("spawn")
	childSpawn, _ := child.GetGlobal("spawn")
	if parentSpawn.Obj != childSpawn.Obj {
		t.Fatal("shared VM registered a second spawn native")
	}
}
```

Add this classification test, which fails until each listed builtin is contextual:

```go
func TestStatefulBuiltinsUseContextualHandlers(t *testing.T) {
	machine := New()
	for _, name := range []string{
		"spawn", "delete", "append", "pop", "json_loads", "sys_load_plugin",
		"io_open", "net_listen", "sqlite_open",
	} {
		native := requireBuiltin(t, machine, name)
		if native.Contextual == nil || native.Fn != nil || !native.IsCallable() {
			t.Errorf("%s is not exclusively contextual", name)
		}
	}
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/vm -run 'TestSharedVMsReuseBuiltinValues|TestContextualCollection|TestContextualJSON' -v`
Expected: FAIL because builtins are still registered per VM or capture bootstrap state.

- [ ] **Step 3: Convert VM-dependent handlers**

Use contextual handlers for:

- `spawn`;
- `delete`, `append`, and `pop`;
- `json_loads`;
- `sys_load_plugin`.

Each obtains `machine := nativeVM(context)` and uses that machine for reference resolution, child VM creation, current-frame metadata, or dynamic registration. Plugin request closures remain legacy because they capture only the synchronized `PluginClient`.

Do not change spawn argument copying or its selection of the function environment in this task.

- [ ] **Step 4: Add one-time shared initialization**

Implement:

```go
func (shared *SharedState) initializeState() {
	shared.stateOnce.Do(func() {
		shared.Root = value.NewGlobalEnvironment(nil)
		shared.Modules = newModuleCache()
		shared.Files = newHandleRegistry[*FileResource]()
		shared.Listeners = newHandleRegistry[*ListenerResource]()
		shared.Sockets = newHandleRegistry[*SocketResource]()
		shared.Databases = newHandleRegistry[*DatabaseResource]()
		shared.Statements = newHandleRegistry[*StatementResource]()
	})
}
```

`SharedState` also owns `builtinsOnce sync.Once`. `NewWithShared` calls `shared.initializeState()`, constructs the executor-local `VM`, and then calls `shared.builtinsOnce.Do(vm.defineBuiltins)`. Every handler that reads VM state must already be contextual at this point; legacy handlers registered during this call may capture only immutable package data or an explicitly concurrency-safe external client. Therefore, the stored builtin values do not retain the bootstrap VM for runtime state access.

- [ ] **Step 5: Verify registry identity and behavior**

Run: `go test -race ./internal/vm -run 'TestSharedVMsReuseBuiltinValues|TestSpawn|Test.*JSON|Test.*Collection' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/vm/builtins.go internal/vm/builtins_collections.go internal/vm/builtins_concurrency.go internal/vm/builtins_json.go internal/vm/builtins_sys.go internal/vm/builtins_registry_test.go internal/vm/builtins_concurrency_test.go internal/vm/native_signatures_test.go internal/vm/vm.go internal/vm/runtime_context_test.go
git commit -m "refactor(vm): bind stateful natives to active context"
```

---

### Task 12: Enforce architecture, document guarantees, and run full verification

**Files:**
- Modify: `internal/vm/architecture_test.go`
- Modify: `docs/CONCURRENCY.md`
- Modify: `docs/NOXY_LANGUAGE_SPEC.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Produces: permanent regression guards and user-facing concurrency documentation.

- [ ] **Step 1: Strengthen architectural tests**

Assert source structure contains:

- contextual invocation through `Invoke`;
- `GlobalEnvironment` on frames, functions, closures, and global references;
- resource registries only in `SharedState`;
- module cache in its own file.

Assert source structure excludes:

```text
ObjMap.Data
Globals map[string]value.Value
GlobalOwner *map[string]value.Value
openFiles on VM
netBufferedData on VM
netBufferedConns on VM
DbHandles/StmtHandles/StmtParams raw maps
native.Fn(args) direct calls
```

- [ ] **Step 2: Document exact guarantees and limitations**

In `docs/CONCURRENCY.md`, document that shared VMs share globals, module cache, and handles; individual binding/map operations avoid Go map crashes; compound operations and nested composite mutation still require channels.

In `docs/NOXY_LANGUAGE_SPEC.md`, retain shallow-copy semantics and add the memory-model note beside composite values. State that no public Noxy API changed in this foundation.

In `CHANGELOG.md`, record fixes for active native context, cross-VM handle ownership, module single initialization, and concurrent map crashes. Explicitly state that `net_select` semantics and supervised spawn remain follow-up work.

- [ ] **Step 3: Run formatting and focused verification**

Run:

```powershell
gofmt -w internal/value internal/plugin internal/vm internal/compiler
go test -race ./internal/value ./internal/plugin ./internal/vm
```

Expected: PASS with no race reports.

- [ ] **Step 4: Run the repository-required verification**

Run each command independently:

```powershell
go test ./internal/...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
go build ./...
go vet ./...
```

Expected: every command exits 0; the Noxy concurrent runner reports no unexpected failures.

- [ ] **Step 5: Confirm architectural scans are empty**

```powershell
rg -n '\.Data\b|Globals map\[string\]|GlobalOwner \*map|openFiles|netBuffered(Data|Conns)|DbHandles|StmtHandles|StmtParams|\.Fn\(args\)' internal -g '*.go'
```

Expected: no production-code matches; any allowed compatibility assertion in tests must be explicit and narrowly scoped.

- [ ] **Step 6: Review the diff against the specification**

Read `docs/superpowers/specs/2026-08-13-runtime-context-foundation-design.md` section by section and confirm every goal and non-goal. Verify no public Noxy signature changed and no follow-up feature leaked into this implementation.

- [ ] **Step 7: Commit final guards and documentation**

```powershell
git add internal/vm/architecture_test.go docs/CONCURRENCY.md docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md
git commit -m "docs: describe concurrency-safe runtime foundation"
```
