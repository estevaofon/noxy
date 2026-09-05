package vm

import (
	"bufio"
	"os"

	"github.com/estevaofon/noxy/internal/value"
)

func (shared *SharedState) initializeState() {
	shared.stateOnce.Do(func() {
		shared.Root = value.NewGlobalEnvironment(nil)
		shared.Modules = newModuleCache()
		shared.Files = newHandleRegistry[*FileResource]()
		shared.Listeners = newSequencedHandleRegistry[*ListenerResource](1, 2)
		shared.Sockets = newSequencedHandleRegistry[*SocketResource](2, 2)
		shared.Databases = newHandleRegistry[*DatabaseResource]()
		shared.Statements = newHandleRegistry[*StatementResource]()
	})
}

func (shared *SharedState) stdin() *bufio.Reader {
	shared.stdinOnce.Do(func() { shared.stdinReader = bufio.NewReader(os.Stdin) })
	return shared.stdinReader
}

// stdinHandle registra (uma vez) o FileResource de os.Stdin — mesmo leitor de
// input(), marcado stdin (close nao fecha, write recusa) — e devolve o fd.
func (shared *SharedState) stdinHandle() int {
	shared.stdinHandleOnce.Do(func() {
		shared.stdinFD = shared.Files.add(&FileResource{file: os.Stdin, reader: shared.stdin(), stdin: true})
	})
	return shared.stdinFD
}

func (vm *VM) defineBuiltins() {
	vm.defineCoreBuiltins()
	vm.defineConvertBuiltins()
	vm.defineConcurrencyBuiltins()
	vm.defineTimeBuiltins()
	vm.defineIOBuiltins()
	vm.defineStringBuiltins()
	vm.defineMathBuiltins()
	vm.defineCryptoBuiltins()
	vm.defineSystemBuiltins()
	vm.defineCollectionBuiltins()
	vm.defineNetworkBuiltins()
	vm.defineSQLiteBuiltins()
	vm.defineJSONBuiltins()
}
