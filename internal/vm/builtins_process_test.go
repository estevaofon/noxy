package vm

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Modulo `process` (spec de design 2026-09-26-process-module-design.md,
// achado 1 do Noxy-Editor). Comandos portateis: `sleep N` em Unix; no cmd,
// `ping -n N+1` contra o loopback dorme ~N segundos (nao ha sleep no
// Windows sem PowerShell).
func shellSleep(seconds int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("ping -n %d 127.0.0.1 >nul", seconds+1)
	}
	return fmt.Sprintf("sleep %d", seconds)
}

func TestIncompleteUTF8TailIsHeldBack(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"abc", 0},
		{"ol\xc3", 1},       // 'á' pela metade: 1 de 2 bytes
		{"x\xe2\x82", 2},    // '€' pela metade: 2 de 3
		{"\xf0\x9f\x98", 3}, // emoji: 3 de 4
		{"ol\xc3\xa1", 0},   // completo
		{"a\xff", 0},        // lead invalido: entrega como esta
		{"\x80", 0},         // continuacao solta: nada a reter
		{"", 0},
	}
	for _, tc := range cases {
		if got := incompleteUTF8Tail([]byte(tc.in)); got != tc.want {
			t.Errorf("incompleteUTF8Tail(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestProcessTakeHoldsIncompleteSequenceUntilEOF(t *testing.T) {
	resource := &ProcessResource{done: make(chan struct{}), drained: make(chan struct{})}
	resource.pending = []byte("ol\xc3")
	if got := string(resource.take()); got != "ol" {
		t.Fatalf("take = %q, want %q (sequencia incompleta retida)", got, "ol")
	}
	resource.pending = append(resource.pending, "\xa1!"...)
	if got := string(resource.take()); got != "\xc3\xa1!" {
		t.Fatalf("take = %q, want %q", got, "\xc3\xa1!")
	}
	resource.pending = []byte("\xc3")
	resource.eof = true
	if got := string(resource.take()); got != "\xc3" {
		t.Fatalf("take no EOF = %q, want os bytes retidos entregues", got)
	}
	if got := string(resource.take()); got != "" {
		t.Fatalf("take vazio = %q, want \"\"", got)
	}
}

func TestStartChildProcessDrainsOutputAndReapsExit(t *testing.T) {
	resource, err := startChildProcess("echo first&echo second")
	if err != nil {
		t.Fatal(err)
	}
	defer resource.shutdown()
	select {
	case <-resource.done:
	case <-time.After(10 * time.Second):
		t.Fatal("o lider nao saiu")
	}
	resource.awaitDrained(time.Second)
	out := string(resource.take())
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Fatalf("saida = %q", out)
	}
	running, code := resource.exitStatus()
	if running || code != 0 {
		t.Fatalf("running=%v code=%d", running, code)
	}
}

func TestShutdownKillsARunningLeader(t *testing.T) {
	resource, err := startChildProcess(shellSleep(30))
	if err != nil {
		t.Fatal(err)
	}
	if resource.exited() {
		t.Fatal("saiu cedo demais")
	}
	started := time.Now()
	resource.shutdown()
	if !resource.exited() {
		t.Fatal("shutdown voltou com o lider vivo")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatalf("shutdown demorou %v", time.Since(started))
	}
	resource.shutdown() // idempotente
}

func shellEchoSleepEcho() string {
	if runtime.GOOS == "windows" {
		return "echo first&" + shellSleep(1) + "&echo second"
	}
	return "echo first; sleep 1; echo second"
}

func processCells(t *testing.T, program string) []string {
	t.Helper()
	cells := semArray(t, captureVMSource(t, program))
	out := make([]string, len(cells))
	for i, cell := range cells {
		out[i] = cell.String()
	}
	return out
}

func TestProcessStartWaitCapturesOutput(t *testing.T) {
	cells := processCells(t, `
use process
let p: process.Process = process.start("echo first&echo second")
let code: int = process.wait(p)
let out: bytes = process.read(p)
process.close(p)
test_report([to_str(p.ok), to_str(p.pid > 0), to_str(code), to_str(out)])`)
	if cells[0] != "true" || cells[1] != "true" || cells[2] != "0" {
		t.Fatalf("ok/pid/code = %v", cells[:3])
	}
	if !strings.Contains(cells[3], "first") || !strings.Contains(cells[3], "second") {
		t.Fatalf("saida = %q", cells[3])
	}
}

func TestProcessReadIsNonBlockingAndIncremental(t *testing.T) {
	cells := processCells(t, `
use process
use sys
let p: process.Process = process.start(`+strconv.Quote(shellEchoSleepEcho())+`)
let first: bytes = b""
let tries: int = 0
while length(first) == 0 && tries < 200 do
    first = first + process.read(p)
    tries = tries + 1
    sys.sleep(25)
end
let running: bool = process.poll(p).running
let code: int = process.wait(p)
let second: bytes = process.read(p)
process.close(p)
test_report([to_str(first), to_str(running), to_str(code), to_str(second)])`)
	if !strings.Contains(cells[0], "first") || strings.Contains(cells[0], "second") {
		t.Fatalf("primeira leitura = %q, want so 'first'", cells[0])
	}
	if cells[1] != "true" || cells[2] != "0" {
		t.Fatalf("running/code = %v", cells[1:3])
	}
	if !strings.Contains(cells[3], "second") {
		t.Fatalf("segunda leitura = %q", cells[3])
	}
}

func TestProcessPollTerminateAndWaitFor(t *testing.T) {
	cells := processCells(t, `
use process
let p: process.Process = process.start(`+strconv.Quote(shellSleep(30))+`)
let before: process.ProcessStatus = process.poll(p)
let asked: bool = process.terminate(p)
let after: process.ProcessStatus = process.wait_for(p, 5000)
process.close(p)
test_report([to_str(before.running), to_str(before.exit_code), to_str(asked), to_str(after.running), to_str(after.exit_code)])`)
	wantCode := "-1" // morto por sinal
	if runtime.GOOS == "windows" {
		wantCode = "1" // TerminateJobObject(job, 1)
	}
	want := []string{"true", "-1", "true", "false", wantCode}
	for i := range want {
		if cells[i] != want[i] {
			t.Fatalf("cell %d = %q, want %q (all: %v)", i, cells[i], want[i], cells)
		}
	}
}

func TestProcessWaitForTimesOutWhileRunning(t *testing.T) {
	cells := processCells(t, `
use process
let p: process.Process = process.start(`+strconv.Quote(shellSleep(30))+`)
let s: process.ProcessStatus = process.wait_for(p, 100)
let killed: bool = process.kill(p)
let code: int = process.wait(p)
process.close(p)
test_report([to_str(s.running), to_str(killed), to_str(code != 0)])`)
	if cells[0] != "true" || cells[1] != "true" || cells[2] != "true" {
		t.Fatalf("got %v", cells)
	}
}

func TestProcessTerminateAfterExitIsFalse(t *testing.T) {
	cells := processCells(t, `
use process
let p: process.Process = process.start("echo done")
let code: int = process.wait(p)
let asked: bool = process.terminate(p)
process.close(p)
process.close(p)
test_report([to_str(code), to_str(asked)])`)
	if cells[0] != "0" || cells[1] != "false" {
		t.Fatalf("got %v, want [0 false]", cells)
	}
}

func TestProcessOperationsOnClosedHandleAreRuntimeErrors(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
use process
let p: process.Process = process.start("echo done")
process.wait(p)
process.close(p)
process.read(p)`)
	if err == nil || !strings.Contains(err.Error(), "is not an open process") {
		t.Fatalf("error=%v", err)
	}
}

func TestProcessWaitForRejectsNegativeTimeout(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
use process
let p: process.Process = process.start("echo done")
process.wait_for(p, -1)`)
	if err == nil || !strings.Contains(err.Error(), "process: wait_for: timeout must be >= 0") {
		t.Fatalf("error=%v", err)
	}
}

func TestCloseProcessesKillsEverythingStillRegistered(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `
use process
let a: process.Process = process.start(`+strconv.Quote(shellSleep(30))+`)
let b: process.Process = process.start(`+strconv.Quote(shellSleep(30))+`)`); err != nil {
		t.Fatal(err)
	}
	live := machine.shared.Processes.snapshot()
	if len(live) != 2 {
		t.Fatalf("registrados=%d, want 2", len(live))
	}
	machine.CloseProcesses()
	if remaining := len(machine.shared.Processes.snapshot()); remaining != 0 {
		t.Fatalf("registry ainda tem %d processos", remaining)
	}
	for handle, resource := range live {
		if !resource.exited() {
			t.Fatalf("processo %d continua vivo depois de CloseProcesses", handle)
		}
	}
	machine.CloseProcesses() // idempotente
}

// wait_for com um prazo enorme nao pode virar poll por overflow da
// multiplicacao por time.Millisecond: o prazo e saturado.
func TestProcessWaitForClampsHugeTimeouts(t *testing.T) {
	cells := processCells(t, `
use process
let p: process.Process = process.start(`+strconv.Quote(shellSleep(1))+`)
let s: process.ProcessStatus = process.wait_for(p, 9223372036854775807)
process.close(p)
test_report([to_str(s.running)])`)
	if cells[0] != "false" {
		t.Fatalf("wait_for(max int) devolveu running=%s: o prazo estourou e virou poll", cells[0])
	}
}

func TestSignalSubscribedReflectsSignalNotify(t *testing.T) {
	machine := New()
	if machine.SignalSubscribed() {
		t.Fatal("VM nova nao deveria ter assinatura de sinal")
	}
	if err := interpretVMSource(t, machine, `
use sys
let ch: any = make_chan(1)
sys.signal_notify(ch)`); err != nil {
		t.Fatal(err)
	}
	if !machine.SignalSubscribed() {
		t.Fatal("signal_notify deveria registrar a assinatura")
	}
	if err := interpretVMSource(t, machine, `
use sys
sys.signal_stop()`); err != nil {
		t.Fatal(err)
	}
	if machine.SignalSubscribed() {
		t.Fatal("signal_stop deveria remover a assinatura")
	}
}
