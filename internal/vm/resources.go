package vm

import (
	"database/sql"
	"net"
	"os"
	"sync"

	"noxy-vm/internal/value"
)

type handleRegistry[T any] struct {
	mu    sync.RWMutex
	next  int
	step  int
	items map[int]T
}

func newHandleRegistry[T any]() *handleRegistry[T] {
	return newSequencedHandleRegistry[T](1, 1)
}

func newSequencedHandleRegistry[T any](next int, step int) *handleRegistry[T] {
	return &handleRegistry[T]{next: next, step: step, items: make(map[int]T)}
}

func (registry *handleRegistry[T]) add(item T) int {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	handle := registry.next
	registry.next += registry.step
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
	if ok {
		delete(registry.items, handle)
	}
	return item, ok
}

func (registry *handleRegistry[T]) snapshot() map[int]T {
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	items := make(map[int]T, len(registry.items))
	for handle, item := range registry.items {
		items[handle] = item
	}
	return items
}

type FileResource struct {
	stateMu     sync.Mutex
	operationMu sync.Mutex
	file        *os.File
	closed      bool
}

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

type ListenerResource struct {
	stateMu        sync.Mutex
	acceptMu       sync.Mutex
	listener       net.Listener
	bufferedAccept net.Conn
	closed         bool
}

type SocketResource struct {
	stateMu      sync.Mutex
	readMu       sync.Mutex
	writeMu      sync.Mutex
	connection   net.Conn
	bufferedRead []byte
	closed       bool
}

type DatabaseResource struct {
	stateMu  sync.Mutex
	database *sql.DB
	closed   bool
}

type StatementResource struct {
	mu         sync.Mutex
	statement  *sql.Stmt
	parameters map[int]interface{}
	closed     bool
}
