package vm

import (
	"fmt"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
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
	Root       *value.GlobalEnvironment
	Modules    *moduleCache
	Files      *handleRegistry[*FileResource]
	fileMetaMu sync.RWMutex
	Listeners  *handleRegistry[*ListenerResource]
	Sockets    *handleRegistry[*SocketResource]
	Databases  *handleRegistry[*DatabaseResource]
	Statements *handleRegistry[*StatementResource]
	initOnce   sync.Once
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
	shared := &SharedState{
		Root:       value.NewGlobalEnvironment(nil),
		Modules:    newModuleCache(),
		Files:      newHandleRegistry[*FileResource](),
		Listeners:  newSequencedHandleRegistry[*ListenerResource](1, 2),
		Sockets:    newSequencedHandleRegistry[*SocketResource](2, 2),
		Databases:  newHandleRegistry[*DatabaseResource](),
		Statements: newHandleRegistry[*StatementResource](),
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
	if shared.Listeners == nil {
		shared.Listeners = newSequencedHandleRegistry[*ListenerResource](1, 2)
	}
	if shared.Sockets == nil {
		shared.Sockets = newSequencedHandleRegistry[*SocketResource](2, 2)
	}
	vm := &VM{
		shared: shared,
		Config: cfg,
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
