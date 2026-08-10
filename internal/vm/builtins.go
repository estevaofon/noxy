package vm

func (vm *VM) defineBuiltins() {
	vm.defineCoreBuiltins()
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
	vm.defineTerminalBuiltins()
}
