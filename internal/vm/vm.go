package vm

import (
	"bufio"
	"fmt"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
	"os"
	"regexp"
	"sync"
)

// Tetos por VM (spec #56 §1). As pilhas NASCEM pequenas (framesInitial /
// stackInitial) e dobram sob demanda ate estes valores; no teto o erro e
// sempre o runtime error `stack overflow: ...`, nunca um panic Go.
const StackMax = 1 << 20 // slots da pilha de operandos
const FramesMax = 100_000

const framesInitial = 64
const stackInitial = 4096

// stackReserve e a folga de operandos que ensureCallCapacity garante na
// ENTRADA de cada frame. push() NAO cresce a pilha (precisa caber no orcamento
// de inline de run(); ver stack.go): todo o crescimento acontece aqui e em
// ensureStackHeadroom. Com 2048, qualquer frame tem, em qualquer profundidade,
// a mesma folga de temporarios que a pilha fixa de 2048 slots dava antes desta
// task; um unico frame que precise de mais bate no sentinela de push() e vira
// runtime error limpo.
const stackReserve = 2048

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

	// Owned: vinculos duraveis (slot, objeto) retidos por este frame
	// (parametros e lets). Liberados em finalizeCurrentFrame.
	Owned []ownedEntry
}

// ownedEntry registra um vínculo durável do frame: o slot e o OBJETO retido
// naquele momento. O release do fim do frame libera o objeto gravado — nunca
// o ocupante atual do slot, que após reuso de slot pode ser um temporário
// jamais retido (liberá-lo seria dec a menos, proibido pela spec).
type ownedEntry struct {
	slot int
	obj  value.Value
}

type SharedState struct {
	Root              *value.GlobalEnvironment
	Modules           *moduleCache
	Files             *handleRegistry[*FileResource]
	fileMetaMu        sync.RWMutex
	Listeners         *handleRegistry[*ListenerResource]
	Sockets           *handleRegistry[*SocketResource]
	Databases         *handleRegistry[*DatabaseResource]
	Statements        *handleRegistry[*StatementResource]
	Regexes           *handleRegistry[*regexp.Regexp]
	RegexPatternCache sync.Map // pattern string -> *regexp.Regexp (atalhos regex.matches/search)
	stateOnce         sync.Once
	builtinsOnce      sync.Once

	// stdin e o leitor UNICO de os.Stdin para todos os VMs deste estado:
	// input() e io.stdin() leem do mesmo buffer (senao a primeira chamada
	// engoliria ate 4 KB das linhas seguintes). Criado na primeira leitura —
	// depois de qualquer troca de os.Stdin feita por quem embute a VM.
	stdinOnce       sync.Once
	stdinReader     *bufio.Reader
	stdinHandleOnce sync.Once
	stdinFD         int

	SignalSubMu      sync.Mutex
	ActiveSignalChan chan os.Signal
}

type VM struct {
	// frames e um slice de VALORES reusado por indice a cada chamada:
	// callPreparedClosure escreve em &frames[frameCount] em vez de heap-alocar
	// um *CallFrame novo. As capacidades de Owned/Deferred de cada slot sao
	// load-bearing para isso — finalizeCurrentFrame (unwind.go) trunca as duas
	// com `[:0]`, nunca com `= nil` (ver BenchmarkNoxyCallOverhead). O slice
	// CRESCE (growFrames, dobro ate FramesMax) apenas em ensureCallCapacity,
	// que reaponta vm.currentFrame; qualquer *CallFrame segurado em variavel
	// Go atraves de uma chamada reentrante tem de ser reobtido por indice
	// (finalizeCurrentFrame faz isso; o loop de run() recarrega apos OP_CALL).
	frames       []CallFrame
	frameCount   int
	currentFrame *CallFrame

	// stackCaptureFloor e o indice do frame mais baixo que captureNoxyStack
	// pode incluir. Zero (padrao) = pilha inteira, o comportamento historico
	// de todo erro fatal de topo. So a fronteira de call_result o levanta,
	// pelo tempo da chamada capturada, para cumprir a promessa da spec de que
	// Failure.stack vai do ponto de falha ate — excluindo — o frame que
	// chamou call_result. Por-VM (tasks rodam em VM proprio), salvo e
	// restaurado pela fronteira, entao fronteiras aninhadas empilham pisos.
	stackCaptureFloor int

	chunk *chunk.Chunk // Removed, accessed via frame
	ip    int          // Removed, accessed via frame (or cached)

	// stack cresce em growStack (dobro ate StackMax); os unicos ponteiros para
	// dentro dela que sobrevivem a uma instrucao sao os upvalues ABERTOS
	// (vm.openUpvalues), migrados por RelocateOpenUpvalues na realocacao.
	// Trocar a pilha e SEMPRE por installStack, nunca por atribuicao direta —
	// stackLimit tem de acompanhar.
	stack    []value.Value
	stackTop int

	// stackLimit == len(stack). Campo proprio porque push() so cabe no
	// orcamento de inline de run() (20, por run() ser "big function")
	// comparando com um campo; comparar com len(vm.stack) custa 1 a mais e
	// desinlina push nos 117 call sites de executor.go.
	stackLimit int

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
		frames: make([]CallFrame, framesInitial),
	}
	vm.installStack(make([]value.Value, stackInitial))

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
