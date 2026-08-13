# Runtime Context Foundation Design

**Date:** 2026-08-13  
**Status:** Approved design, awaiting written-spec review  
**Scope:** First subproject in the NoxyDB-driven runtime improvements

## 1. Purpose

This subproject establishes the concurrency-safe runtime foundation required by
the later fixes for `spawn`, network polling, supervised tasks, `defer`, signals,
strings, and the HTTP server.

The immediate defects are architectural:

- native functions stored in shared globals capture the first `VM` that
  registered them;
- files remain local to a VM while sockets and SQLite handles are only partly
  shared;
- network look-ahead buffers remain local to a VM even though connections are
  shared;
- module and frame globals are raw Go maps;
- concurrent module loads can initialize the same module more than once.

This design removes those inconsistencies without changing Noxy source syntax
or the public behavior of existing builtins.

## 2. Goals

1. Invoke stateful natives with the VM that is actually executing the call.
2. Preserve source compatibility for the existing Go native APIs.
3. Replace raw global maps with synchronized, identity-stable environments.
4. Give module exports a synchronized live view of their module environment.
5. Coordinate concurrent module loading and detect import cycles.
6. Make files, sockets, listeners, databases, and statements safely reachable
   from every VM sharing the same `SharedState`.
7. Eliminate concurrent Go map access in the migrated tables and stores.
8. Preserve all existing Noxy builtin signatures and observable success/error
   result shapes.

## 3. Non-goals

This subproject does not implement:

- corrected `spawn` argument, closure, or module-environment semantics;
- the recursive cross-VM value-transfer policy;
- a new network poller or new `net_select` semantics;
- socket nonblocking mode or read/write timeouts;
- supervised `Task` values;
- unwind, `defer`, or exception handling;
- signal subscriptions;
- string substring corrections;
- the incremental strict HTTP server.

The existing `net_select` buffering behavior is retained temporarily, but its
buffers move into synchronized shared resources so that polling and receiving
through different VMs use the same state.

## 4. Native ABI

### 4.1 Dual handler model

The existing native ABI remains unchanged:

```go
type NativeFunc func([]Value) Value
```

A contextual ABI is added:

```go
type NativeContext interface {
	IsNativeContext()
}

type ContextualNativeFunc func(NativeContext, []Value) (Value, error)
```

`VM` implements `NativeContext`. The interface is deliberately opaque; it is a
type-safe carrier for the active runtime, not a growing abstraction of VM
operations. Builtin handlers implemented in package `vm` validate and convert
the context to `*VM`.

`ObjNative` holds exactly one handler:

```go
type ObjNative struct {
	Name       string
	Fn         NativeFunc
	Contextual ContextualNativeFunc
	Signature  *NativeSignature
}
```

All native invocation goes through:

```go
func (native *ObjNative) Invoke(
	context NativeContext,
	args []Value,
) (Value, error)
```

`Invoke` has these rules:

- only `Fn` is set: call it and return `(result, nil)`;
- only `Contextual` is set: require a non-nil valid context and call it;
- neither or both are set: return an ordinary invocation error;
- never panic because the handler or context is absent or malformed.

Runtime callability checks use an `IsCallable`-style predicate rather than
testing `native.Fn != nil`.

### 4.2 Constructors and registration

The following existing APIs retain their signatures and behavior:

```go
value.NewNative(...)
value.NewNativeWithSignature(...)
vm.DefineNative(...)
vm.DefineNativeWithSignature(...)
```

The following APIs are added:

```go
value.NewContextualNative(...)
value.NewContextualNativeWithSignature(...)
vm.DefineContextualNative(...)
vm.DefineContextualNativeWithSignature(...)
```

Legacy extension code remains source-compatible. Go binaries are rebuilt as
usual after the internal package changes. Existing RPC-based Noxy plugins keep
their public protocol unchanged.

Pure natives may remain legacy handlers. Natives that access VM or shared
runtime state become contextual. A legacy plugin closure that captures only its
own concurrency-safe client may remain legacy; it must not capture a VM.

### 4.3 Registration lifecycle

`SharedState` owns an initialization `sync.Once`. Both `New()` and
`NewWithShared()` ensure that the root environment, registries, module cache,
and standard builtins are initialized exactly once.

Builtin handler values do not capture the VM used during initialization.
`NewWithShared()` then initializes only per-executor state such as stack,
frames, and configuration.

Dynamic `DefineNative` operations use an atomic define-if-absent operation on
the root environment. The first definition retains the name, preserving the
current behavior while avoiding check-then-set races.

## 5. Synchronized values and global environments

### 5.1 Shared binding store

Package `value` contains a private synchronized store:

```go
type bindingStore struct {
	mu     sync.RWMutex
	values map[interface{}]Value
}
```

It provides individual `Get`, `Set`, `DefineIfAbsent`, `Delete`, `Len`, and
`Snapshot` operations. Locks are held only while accessing the map. Returned
`Value` objects are not recursively locked or copied by the store.

`ObjMap` uses this store and exposes methods instead of a mutable `Data` field.
All VM, JSON, collection, reference, and test code migrates to those methods.
This prevents internal code from bypassing the store mutex.

Individual map operations become safe against Go's concurrent-map crash. A
multi-operation transaction remains non-atomic and requires program-level
coordination.

### 5.2 Global environment

`GlobalEnvironment` contains a local `bindingStore` and an immutable optional
parent pointer. It provides:

```go
GetLocal(name string) (Value, bool)
Resolve(name string) (Value, bool)
SetLocal(name string, value Value)
DefineLocalIfAbsent(name string, value Value) bool
ResolveOwner(name string) (*GlobalEnvironment, bool)
LocalSnapshot() map[string]Value
```

Lookup locks one environment at a time and releases the lock before walking to
the parent. No environment lock remains held while Noxy or native code runs.

Every `CallFrame`, `ObjFunction`, and `ObjClosure` stores a non-nil
`*GlobalEnvironment`. `ObjRef` global references store the owner environment
and binding name instead of `*map[string]Value`.

The environment rules are:

- the root environment contains builtins and script globals;
- a module environment has the root environment as its parent;
- reads resolve local bindings before parent bindings;
- assignments always write to the current frame's local environment;
- a module assignment to an inherited name creates or updates a local binding;
- a global reference uses `ResolveOwner` and retains that exact environment;
- module exports include local bindings only, never inherited builtins.

Accordingly:

- `OP_GET_GLOBAL` calls `Resolve`;
- `OP_SET_GLOBAL` calls `SetLocal`;
- `OP_REF_GLOBAL` calls `ResolveOwner`;
- wildcard and selected imports write with `SetLocal`.

When a function constant is materialized, the resulting function and closure
are bound to the current frame environment. Calls use the environment retained
by the closure.

### 5.3 Live module exports

The module export object and its `GlobalEnvironment` local bindings share the
same `bindingStore` and therefore the same identity and mutex:

```text
module GlobalEnvironment.local -> bindingStore <- module export object
```

Consequences:

- `module.member` sees later assignments made by module functions;
- module functions see writes performed through the module export object;
- there is no snapshot staleness or duplicate mutex for the same bindings;
- wildcard import takes a local snapshot, releases its source lock, and only
  then writes to the target environment.

The mutable `ObjMap.Data` field is an internal compatibility break accepted by
this design. The source-compatibility guarantee covers the native construction
and registration APIs, not direct mutation of internal value fields.

### 5.4 `InterpretWithGlobals`

`InterpretWithGlobals(chunk, globals)` keeps its Go signature but uses
copy-in/copy-out semantics:

1. If `globals` is non-nil, copy its bindings into a new child environment of
   the root, even when the map is empty.
2. Execute the script in that environment.
3. Take a synchronized local snapshot on every return path, including runtime
   error.
4. Replace the caller map contents with that snapshot before returning.

The caller must not mutate its map concurrently with the method call. Spawned
work that outlives the call continues to use the internal environment; its
later writes are not reflected into the caller map.

`nil` remains the explicit request to execute directly in the root environment.
A new `InterpretWithEnvironment` entry point supports callers that require a
persistent synchronized environment rather than a synchronous map adapter.

## 6. Concurrent module loading

### 6.1 Cache identity and states

The module cache key contains the canonical module root and resolved module
name so VMs with different `RootPath` configurations cannot collide merely
because they share a `SharedState`.

Each entry has one of these states:

```text
loading -> ready
        -> failed
```

A loading entry owns a completion channel. Cache locks protect entry and
dependency-graph bookkeeping only; parsing, compilation, module execution, and
waiting occur without the cache lock.

### 6.2 Single initialization

For a missing key, the first caller installs `loading` and initializes the
module. Other callers obtain the same entry and wait on its completion channel.
The module export object is published as `ready` only after parsing,
compilation, and execution succeed.

On failure, the entry records the error and closes its completion channel so
all callers already attached to that attempt observe the same error. The cache
then removes that failed entry; a later request may begin a new attempt.

Partially initialized environments and exports are never published.

### 6.3 Cycle detection

Cycle detection covers both ordinary recursion and cycles split across
concurrent loading attempts.

Each active module load records directed dependency edges. Before a loading
module waits for another loading module, the coordinator adds the proposed edge
and checks whether it closes a cycle. If it would, no wait occurs and the
runtime returns an import error containing the complete module chain.

Dependency edges are removed when their load attempt completes. This avoids the
cross-flight deadlock in which one goroutine loads `A -> B` while another loads
`B -> A`.

## 7. Shared resources

### 7.1 Registry rules

`SharedState` owns separate registries for:

- files;
- listeners and connections;
- databases;
- statements.

Each registry has its own mutex and monotonic handle counter. Handles are not
reused during the lifetime of a `SharedState`.

A registry lock is used only to locate, insert, or remove an entry. It is never
held during resource I/O, environment access, module execution, native code, or
while another registry is locked.

### 7.2 Resource entries

Entries own their lifecycle and operation synchronization:

```text
FileResource
  state mutex, operation mutex, file, closed

ListenerResource
  state mutex, accept mutex, listener, buffered accepted connection, closed

SocketResource
  state mutex, read mutex, write mutex, connection, buffered read data, closed

DatabaseResource
  state mutex, database, closed

StatementResource
  mutex, statement, pending parameters, closed
```

The temporary accept and read buffers used by the current `net_select` move
into their corresponding listener or socket entry. A poll in one VM and an
accept or receive in another therefore observe the same buffered state.

SQLite statement parameters move into `StatementResource` and are protected by
the statement mutex. Database and statement handles no longer rely on one
coarse database lock during operations.

### 7.3 Operation and close protocol

An operation:

1. obtains the entry pointer under the registry lock;
2. releases the registry lock;
3. checks and operates through the entry's locks;
4. never reacquires a registry lock while holding an operation lock.

Close:

1. atomically removes the handle from its registry;
2. marks the entry closed under its state lock;
3. closes the underlying resource outside the registry lock;
4. does not wait for a read or accept operation mutex before closing.

Closing the underlying resource is allowed to interrupt pending operations.
An operation that already holds the entry may finish or receive the underlying
close error. A new operation observes an invalid handle. Repeated close calls
retain each builtin's current public result behavior.

Explicit builtin close remains the resource-lifetime mechanism. A VM does not
automatically close shared resources when its own interpretation ends, because
another VM may still use them.

## 8. Invocation and error flow

Native calls follow this sequence:

```text
OP_CALL
  -> validate arity, type contract, and reference modes
  -> shallow-copy ordinary parameters
  -> ObjNative.Invoke(active VM, prepared arguments)
  -> convert invocation error to runtimeError with source location
  -> replace callee and arguments with the result
```

Expected operational failures such as a missing file or invalid socket retain
their current Noxy result representation. The `error` result of a contextual
handler is reserved for invocation failures that terminate the current Noxy
execution.

The foundation introduces ordinary VM errors, rather than panics, for:

- a native with zero or two handlers;
- absent or incompatible `NativeContext`;
- a global reference without a valid owner environment;
- an inconsistent module-cache entry;
- an import cycle, including its dependency chain.

Existing builtin-specific invalid/closed-handle results remain compatible.
Unexpected Go panics continue to identify internal implementation defects and
are not silently translated into operational results.

## 9. Concurrency guarantees and limits

This foundation guarantees:

- synchronized access to global bindings, module exports, generic map storage,
  resource registries, per-resource mutable state, and module-cache metadata;
- no Go concurrent-map panic from the migrated storage;
- consistent resource visibility between VMs sharing a `SharedState`;
- exactly one successful initialization for a concurrently requested module
  cache key;
- no cache-lock retention while arbitrary Noxy or native code executes.

It does not make compound language operations atomic. For example:

```noxy
counter = counter + 1
```

is still a read followed by a write. Concurrent mutation of arrays, structs,
or a sequence of map operations still requires channels or another explicit
coordination mechanism. Synchronizing a global binding does not recursively
synchronize the object stored in that binding.

## 10. Migration strategy

The implementation proceeds in this order, with tests remaining buildable at
each step and the whole subproject delivered as one consistent unit:

1. Add the contextual ABI, constructors, `Invoke`, and legacy adapters.
2. Route every native call and callability check through the new abstraction.
3. Add synchronized map storage and `GlobalEnvironment`.
4. Migrate frames, functions, closures, globals, references, and imports.
5. Make module exports a live view and add coordinated module loading.
6. Add shared resource entries and migrate file, network, and SQLite state.
7. Convert every state-dependent builtin to a contextual handler.
8. Remove obsolete local state, raw-map access, and direct handler calls.
9. Update architecture, concurrency, compatibility, and language documentation.

Although intermediate commits may introduce adapters, no stage is considered
complete while contextual dispatch and shared resource ownership disagree.

## 11. Testing strategy

All production behavior is introduced test-first. Each regression test must be
observed failing for the intended reason before its implementation is added.
Concurrent tests use channels and barriers instead of timing sleeps.

Required coverage includes:

- legacy native invocation;
- contextual native invocation with the active VM;
- invalid zero-handler and dual-handler natives;
- two shared VMs invoking the same native without context capture;
- local-before-parent global resolution;
- local assignment shadowing an inherited binding;
- global references retaining their owner environment;
- non-nil empty `InterpretWithGlobals` maps and copy-out on success and error;
- live module exports after a module function updates a binding;
- wildcard imports excluding inherited bindings;
- one initialization under concurrent imports;
- direct and cross-flight import cycles with the complete chain;
- cross-VM file-handle use;
- cross-VM visibility of network buffers;
- concurrent close without panic or raw-map access;
- synchronized SQLite statement parameters;
- preservation of existing native Go API call sites;
- preservation of existing Noxy builtin signatures and result shapes.

Architecture tests prevent reintroduction of:

- mutable raw `ObjMap` storage access;
- global `map` fields in frames, functions, closures, or references;
- VM-local file or network-buffer registries;
- direct invocation of `ObjNative.Fn`;
- registry locks held during blocking I/O or Noxy execution.

## 12. Acceptance criteria

The implementation is accepted only after fresh successful runs of:

```powershell
go test ./internal/...
go test -race ./internal/vm
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
go build ./...
go vet ./...
```

In addition:

- no existing Noxy program requires a source change;
- the existing Go native constructors and VM registration methods compile
  unchanged;
- the race-enabled tests cover the new concurrent paths;
- the later `spawn` and network projects can build on these abstractions without
  moving ownership again.
