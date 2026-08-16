package vm

import "noxy-vm/internal/value"

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

func (vm *VM) defineBuiltins() {
	vm.defineCoreBuiltins()
	vm.defineConvertBuiltins()
	vm.defineConcurrencyBuiltins()
	vm.defineTimeBuiltins()
	vm.defineIOBuiltins()
	vm.defineStringBuiltins()
	vm.defineCryptoBuiltins()
	vm.defineSystemBuiltins()
	vm.defineCollectionBuiltins()
	vm.defineNetworkBuiltins()
	vm.defineSQLiteBuiltins()
	vm.defineJSONBuiltins()
}
