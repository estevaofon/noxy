package main

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Ctrl+C / SIGTERM no noxy: sem tratador, o Go sai sem rodar os defers e
// CloseProcesses nunca roda — a arvore de `process.start` ficava orfa
// (revisao do modulo process, achado 2). exitOnSignal limpa e sai com
// 128+sinal, a menos que o programa tenha assumido os sinais com
// sys.signal_notify.
func TestExitOnSignalCleansUpThenExitsWith128PlusSignal(t *testing.T) {
	signals := make(chan os.Signal, 1)
	var cleaned int32
	exited := make(chan int, 2)
	go exitOnSignal(signals, func() bool { return false }, func() { atomic.AddInt32(&cleaned, 1) }, func(code int) { exited <- code })

	signals <- os.Interrupt
	select {
	case code := <-exited:
		if code != 130 {
			t.Fatalf("exit code=%d, want 130", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGINT nao levou a saida")
	}
	if atomic.LoadInt32(&cleaned) != 1 {
		t.Fatalf("cleanup rodou %d vezes, want 1", cleaned)
	}

	signals <- syscall.SIGTERM
	select {
	case code := <-exited:
		if code != 143 {
			t.Fatalf("exit code=%d, want 143", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGTERM nao levou a saida")
	}
}

func TestExitOnSignalDefersToAProgramThatSubscribed(t *testing.T) {
	signals := make(chan os.Signal, 1)
	var cleaned int32
	exited := make(chan int, 1)
	go exitOnSignal(signals, func() bool { return true }, func() { atomic.AddInt32(&cleaned, 1) }, func(code int) { exited <- code })

	signals <- os.Interrupt
	select {
	case code := <-exited:
		t.Fatalf("saiu com %d, mas o programa assinou os sinais", code)
	case <-time.After(200 * time.Millisecond):
	}
	if atomic.LoadInt32(&cleaned) != 0 {
		t.Fatal("cleanup nao deveria rodar quando o programa assinou os sinais")
	}
}
