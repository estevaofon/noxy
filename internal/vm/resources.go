package vm

import (
	"bufio"
	"database/sql"
	"errors"
	"io"
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
	// reader e o leitor bufferizado de read_line, criado sob demanda; para o
	// recurso de stdin (Task 11) e o MESMO leitor de input(). Acesso so
	// dentro de use() (operationMu). Uma leitura do arquivo INTEIRO
	// (readAll em arquivo comum) DESCARTA o leitor de linha: ela reposiciona
	// o offset do SO (Seek + read ate o fim) e o que estava no buffer deixa de
	// corresponder a esse offset — o read_line seguinte abre leitor novo e ve
	// EOF.
	reader *bufio.Reader
	// stdin marca o recurso de os.Stdin: close() nao fecha o descritor e
	// read/read_lines leem "o restante" pelo reader (pipe nao tem Stat/Seek).
	stdin bool
}

// lineReader devolve o leitor bufferizado do recurso (cria na primeira
// chamada). Chamar dentro de use().
func (resource *FileResource) lineReader(file *os.File) *bufio.Reader {
	if resource.reader == nil {
		resource.reader = bufio.NewReader(file)
	}
	return resource.reader
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

// readAll devolve o conteudo "inteiro": do inicio em arquivo comum
// (readFileContents), o que ainda nao foi consumido em stdin. Chamar dentro de
// use().
func (resource *FileResource) readAll(file *os.File) ([]byte, bool, string) {
	if !resource.stdin {
		content, ok, errorText := readFileContents(file)
		// O offset do SO terminou no fim do arquivo e o buffer pendente ficou
		// dessincronizado: o leitor de linha e refeito na proxima chamada.
		resource.reader = nil
		return content, ok, errorText
	}
	content, err := io.ReadAll(resource.lineReader(file))
	if err != nil {
		return nil, false, err.Error()
	}
	return content, true, ""
}

func (resource *FileResource) close() error {
	resource.stateMu.Lock()
	if resource.closed || resource.file == nil {
		resource.stateMu.Unlock()
		return os.ErrClosed
	}
	if resource.stdin {
		// os.Stdin e do processo, nao do programa Noxy: fechar o descritor
		// deixaria qualquer leitura seguinte (inclusive de outro VM do mesmo
		// estado) sem entrada.
		resource.stateMu.Unlock()
		return nil
	}
	resource.closed = true
	file := resource.file
	resource.stateMu.Unlock()
	return file.Close()
}

type ListenerResource struct {
	stateMu            sync.Mutex
	deadlineMu         sync.Mutex
	acceptMu           sync.Mutex
	listener           deadlineListener
	ioTimeout          time.Duration
	lastAcceptDeadline time.Time
	deadlineGeneration uint64
	closed             bool
	pollWaiters        map[*networkWake]struct{}
}

type SocketResource struct {
	stateMu            sync.Mutex
	deadlineMu         sync.Mutex
	readMu             sync.Mutex
	writeMu            sync.Mutex
	connection         net.Conn
	ioTimeout          time.Duration
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
		waiters:  takeNetworkWaiters(resource.pollWaiters),
	}
	resource.listener = nil
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
	errs := make([]error, 0, len(detached.waiters)+1)
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
