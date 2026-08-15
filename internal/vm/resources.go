package vm

import (
	"database/sql"
	"errors"
	"net"
	"os"
	"sync"
	"time"

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
	stateMu             sync.Mutex
	deadlineMu          sync.Mutex
	acceptMu            sync.Mutex
	listener            deadlineListener
	bufferedAccept      net.Conn
	ioTimeout           time.Duration
	acceptProbeDeadline time.Time
	lastAcceptDeadline  time.Time
	deadlineGeneration  uint64
	closed              bool
	pollWaiters         map[*networkWake]struct{}
}

type SocketResource struct {
	stateMu            sync.Mutex
	deadlineMu         sync.Mutex
	readMu             sync.Mutex
	writeMu            sync.Mutex
	connection         net.Conn
	bufferedRead       []byte
	ioTimeout          time.Duration
	readProbeDeadline  time.Time
	lastReadDeadline   time.Time
	lastWriteDeadline  time.Time
	deadlineGeneration uint64
	closed             bool
	pollWaiters        map[*networkWake]struct{}
}

type detachedSocket struct {
	connection net.Conn
	waiters    []*networkWake
}

type detachedListener struct {
	listener deadlineListener
	buffered net.Conn
	waiters  []*networkWake
}

func takeNetworkWaiters(waiters map[*networkWake]struct{}) []*networkWake {
	result := make([]*networkWake, 0, len(waiters))
	for waiter := range waiters {
		result = append(result, waiter)
	}
	return result
}

func detachSocketLocked(resource *SocketResource) (detachedSocket, bool) {
	if resource.closed {
		return detachedSocket{}, false
	}
	resource.closed = true
	resource.deadlineGeneration++
	detached := detachedSocket{
		connection: resource.connection,
		waiters:    takeNetworkWaiters(resource.pollWaiters),
	}
	resource.connection = nil
	resource.bufferedRead = nil
	resource.readProbeDeadline = time.Time{}
	resource.pollWaiters = nil
	return detached, true
}

func detachListenerLocked(resource *ListenerResource) (detachedListener, bool) {
	if resource.closed {
		return detachedListener{}, false
	}
	resource.closed = true
	resource.deadlineGeneration++
	detached := detachedListener{
		listener: resource.listener,
		buffered: resource.bufferedAccept,
		waiters:  takeNetworkWaiters(resource.pollWaiters),
	}
	resource.listener = nil
	resource.bufferedAccept = nil
	resource.acceptProbeDeadline = time.Time{}
	resource.pollWaiters = nil
	return detached, true
}

func finishSocketDetach(detached detachedSocket) error {
	errs := make([]error, 0, len(detached.waiters)+1)
	for _, waiter := range detached.waiters {
		if err := waiter.Signal(); err != nil {
			errs = append(errs, err)
		}
	}
	if detached.connection != nil {
		if err := detached.connection.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func finishListenerDetach(detached detachedListener) error {
	errs := make([]error, 0, len(detached.waiters)+2)
	for _, waiter := range detached.waiters {
		if err := waiter.Signal(); err != nil {
			errs = append(errs, err)
		}
	}
	if detached.listener != nil {
		if err := detached.listener.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if detached.buffered != nil {
		if err := detached.buffered.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
