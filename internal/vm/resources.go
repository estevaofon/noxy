package vm

import (
	"database/sql"
	"net"
	"os"
	"sync"
)

type handleRegistry[T any] struct {
	mu    sync.RWMutex
	next  int
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
	if ok {
		delete(registry.items, handle)
	}
	return item, ok
}

type FileResource struct {
	stateMu     sync.Mutex
	operationMu sync.Mutex
	file        *os.File
	closed      bool
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
