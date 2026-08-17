package vm

import (
	"fmt"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
	"os"
	"sync"
)

const StackMax = 2048
const FramesMax = 64

func (vm *VM) runtimeError(c *chunk.Chunk, ip int, format string, args ...interface{}) error {
	return vm.runtimeErrorCause(c, ip, nil, format, args...)
}

type CallFrame struct {
	Closure     *value.ObjClosure
	IP          int
	StackBase   int // First stack slot owned by this frame
	LocalBase   int // Offset in stack where this frame's locals start
	Deferred    []PreparedCall
	Environment *value.GlobalEnvironment

	// Owned: slots absolutos de vm.stack retidos por este frame
	// (parametros e lets). Liberados em finalizeCurrentFrame.
	Owned []int
}

type SharedState struct {
	Root         *value.GlobalEnvironment
	Modules      *moduleCache
	Files        *handleRegistry[*FileResource]
	fileMetaMu   sync.RWMutex
	Listeners    *handleRegistry[*ListenerResource]
	Sockets      *handleRegistry[*SocketResource]
	Databases    *handleRegistry[*DatabaseResource]
	Statements   *handleRegistry[*StatementResource]
	stateOnce    sync.Once
	builtinsOnce sync.Once

	SignalSubMu      sync.Mutex
	ActiveSignalChan chan os.Signal
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
	return NewWithShared(&SharedState{}, cfg)
}

func NewWithShared(shared *SharedState, cfg VMConfig) *VM {
	shared.initializeState()
	vm := &VM{
		shared: shared,
		Config: cfg,
	}

	shared.builtinsOnce.Do(vm.defineBuiltins)
	return vm
}

func (vm *VM) DefineNative(name string, fn value.NativeFunc) {
	vm.shared.Root.DefineLocalIfAbsent(name, stampReadonlyArgs(value.NewNative(name, fn)))
}

func (vm *VM) DefineNativeWithSignature(name string, signature value.NativeSignature, fn value.NativeFunc) {
	vm.shared.Root.DefineLocalIfAbsent(name, value.NewNativeWithSignature(name, signature, fn))
}

func (vm *VM) DefineContextualNative(name string, fn value.ContextualNativeFunc) {
	vm.SetGlobal(name, stampReadonlyArgs(value.NewContextualNative(name, fn)))
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
