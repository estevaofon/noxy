package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/noxylang/noxy/internal/vm"
)

// installExitSignalHandler faz Ctrl+C / SIGTERM no noxy passarem pela
// limpeza dos recursos que sobreviveriam ao processo — a arvore de
// `process.start` (spec do modulo process §4.3) e os plugins por processo
// (spec de extensoes §4.5) — antes de sair com 128+sinal, o codigo que o
// shell atribui a um processo morto pelo sinal. Sem tratador o Go sai na
// hora, sem rodar os defers de runWithConfig, e o filho ficava orfao: com
// Setpgid ele nem recebe o Ctrl+C do terminal (grupo proprio). Um programa
// que assinou os sinais com sys.signal_notify e dono deles: o tratador
// nao interfere e o programa decide quando chamar sys.exit. So no modo
// script — o REPL tem o proprio Ctrl+C no editor de linha.
func installExitSignalHandler(machine *vm.VM) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go exitOnSignal(signals, machine.SignalSubscribed, func() {
		machine.CloseProcesses()
		machine.CloseExtensions()
	}, os.Exit)
	return func() { signal.Stop(signals) }
}

// exitOnSignal e o laco do tratador, separado para teste: cada sinal que
// o programa nao assinou roda cleanup e exit(128+numero do sinal).
func exitOnSignal(signals <-chan os.Signal, subscribed func() bool, cleanup func(), exit func(int)) {
	for sig := range signals {
		if subscribed() {
			continue
		}
		cleanup()
		exit(128 + signalNumber(sig))
	}
}

func signalNumber(sig os.Signal) int {
	if number, ok := sig.(syscall.Signal); ok {
		return int(number)
	}
	return 2 // os.Interrupt em qualquer plataforma
}
