package vm

import (
	"database/sql"
	"fmt"
	"net"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
	"os"
	"sync"
)

const StackMax = 2048
const FramesMax = 64

func (vm *VM) runtimeError(c *chunk.Chunk, ip int, format string, args ...interface{}) error {
	line := 0
	file := "?"
	if c != nil {
		file = c.FileName
		if ip > 0 && ip <= len(c.Lines) {
			line = c.Lines[ip-1]
		}
	}
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("[%s:line %d] %s", file, line, msg)
}

type CallFrame struct {
	Closure     *value.ObjClosure
	IP          int
	Slots       int // Offset in stack where this frame's locals start
	Environment *value.GlobalEnvironment
}

type SharedState struct {
	Root    *value.GlobalEnvironment
	Modules *moduleCache

	// Shared Network Resources
	NetListeners map[int]net.Listener
	NetConns     map[int]net.Conn
	NextNetID    int
	NetLock      sync.Mutex

	// Shared Database Resources
	DbHandles   map[int]*sql.DB
	StmtHandles map[int]*sql.Stmt
	StmtParams  map[int]map[int]interface{}
	NextDbID    int
	NextStmtID  int
	DbLock      sync.Mutex
}

type VM struct {
	frames       [FramesMax]*CallFrame
	frameCount   int
	currentFrame *CallFrame

	chunk *chunk.Chunk // Removed, accessed via frame
	ip    int          // Removed, accessed via frame (or cached)

	stack    [StackMax]value.Value
	stackTop int

	shared *SharedState
	Config VMConfig

	moduleLoadStack []moduleKey

	// IO Management
	openFiles map[int64]*os.File
	nextFD    int64

	// Net Management (Moved to SharedState)
	netBufferedData  map[int][]byte   // For peeked data during select (Local to thread/VM?)
	netBufferedConns map[int]net.Conn // For peeked accepts (Local to thread/VM?)
	// netListeners, netConns, nextNetID removed from VM

	LastPopped value.Value

	openUpvalues *value.ObjUpvalue // Head of linked list of open upvalues
}

func (*VM) IsNativeContext() {}

func nativeVM(context value.NativeContext) (*VM, error) {
	machine, ok := context.(*VM)
	if !ok || machine == nil {
		return nil, fmt.Errorf("invalid VM native context")
	}
	return machine, nil
}

type VMConfig struct {
	RootPath string
}

func New() *VM {
	return NewWithConfig(VMConfig{RootPath: "."})
}

func NewWithConfig(cfg VMConfig) *VM {
	shared := &SharedState{
		Root:         value.NewGlobalEnvironment(nil),
		Modules:      newModuleCache(),
		NetListeners: make(map[int]net.Listener),
		NetConns:     make(map[int]net.Conn),
		NextNetID:    1,
		DbHandles:    make(map[int]*sql.DB),
		StmtHandles:  make(map[int]*sql.Stmt),
		StmtParams:   make(map[int]map[int]interface{}),
		NextDbID:     1,
		NextStmtID:   1,
	}
	return NewWithShared(shared, cfg)
}

func NewWithShared(shared *SharedState, cfg VMConfig) *VM {
	if shared.Root == nil {
		shared.Root = value.NewGlobalEnvironment(nil)
	}
	if shared.Modules == nil {
		shared.Modules = newModuleCache()
	}
	vm := &VM{
		shared:    shared,
		Config:    cfg,
		openFiles: make(map[int64]*os.File),
		nextFD:    1,

		netBufferedData:  make(map[int][]byte),
		netBufferedConns: make(map[int]net.Conn),
	}

	vm.defineBuiltins()
	return vm
}

func (vm *VM) DefineNative(name string, fn value.NativeFunc) {
	vm.shared.Root.DefineLocalIfAbsent(name, value.NewNative(name, fn))
}

func (vm *VM) DefineNativeWithSignature(name string, signature value.NativeSignature, fn value.NativeFunc) {
	vm.shared.Root.DefineLocalIfAbsent(name, value.NewNativeWithSignature(name, signature, fn))
}

func (vm *VM) DefineContextualNative(name string, fn value.ContextualNativeFunc) {
	vm.SetGlobal(name, value.NewContextualNative(name, fn))
}

func (vm *VM) DefineContextualNativeWithSignature(name string, signature value.NativeSignature, fn value.ContextualNativeFunc) {
	vm.shared.Root.DefineLocalIfAbsent(name, value.NewContextualNativeWithSignature(name, signature, fn))
}

func (vm *VM) SetGlobal(name string, val value.Value) {
	vm.shared.Root.SetLocal(name, val)
}

func (vm *VM) GetGlobal(name string) (value.Value, bool) {
	return vm.shared.Root.GetLocal(name)
}

func (vm *VM) SetModule(name string, val value.Value) {
	source, err := vm.resolveModule(name)
	if err == nil {
		vm.shared.Modules.store(source.Key, val)
	}
}

func (vm *VM) GetModule(name string) (value.Value, bool) {
	source, err := vm.resolveModule(name)
	if err != nil {
		return value.NewNull(), false
	}
	return vm.shared.Modules.get(source.Key)
}
