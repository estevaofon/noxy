# Módulo `process` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `process.start(cmd)` devolve um handle de processo filho cuja saída se lê aos pedaços sem bloquear e cuja árvore inteira se para com `terminate`/`kill`/`close` — e nada sobrevive à saída do Noxy.

**Architecture:** um `ProcessResource` por processo no registry `SharedState.Processes` (handle inteiro no struct Noxy `Process`), com o líder subindo por `shellCommand` (o mesmo de `sys.exec`), stdout+stderr num único `os.Pipe` drenado por goroutine para um buffer, `cmd.Wait` memoizado numa segunda goroutine (`done`), grupo de processos (`Setpgid`) em Unix e job object no Windows para alcançar a árvore. `terminate`/`kill` sinalizam a árvore; `close` e `CloseProcesses` (saída do Noxy) matam o que resta.

**Tech Stack:** Go 1.25, `os/exec`, `syscall` (Unix), `golang.org/x/sys/windows` (já dependência). Sem dependências novas.

**Spec:** `docs/superpowers/specs/2026-09-26-process-module-design.md` — o plano argumenta a partir dela; leia as duas.

## Global Constraints

- Verificação obrigatória após cada tarefa (AGENTS.md): `go build ./... && go vet ./...`, `GOOS=windows go vet ./internal/vm`, `go test ./internal/... -count=1`, `go test ./cmd/... -count=1`, `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`.
- Guardas de arquitetura: `defineProcessBuiltins()` em `builtins_process.go` (tabela de `TestBuiltinSourceLayout`); registry `Processes` no `SharedState` (`TestResourceRegistriesAndModuleCacheHaveSharedOwners`); nenhum map global cru.
- Nativos de módulo são `process_<nome>` e só aparecem pelos wrappers de `internal/stdlib/process.nx`; snapshot ordenado de `builtins_registry_test.go` atualizado.
- Handle inválido em `read`/`poll`/`wait`/`wait_for`/`terminate`/`kill` é erro de runtime com o texto literal `process: handle N is not an open process`; `close` é idempotente.
- `read` nunca bloqueia e nunca corta uma sequência UTF-8 incompleta antes do EOF.
- Diagnóstico nunca em stdout; nenhum `fmt.Print*` no runtime.
- Nada de `time.Sleep` como sincronização nos testes além do necessário para "ainda rodando" (300 ms contra um `sleep 1`); os fins são esperados por `wait`/`wait_for`.

## Review Focus

1. Um neto que herda o pipe (`sh -c 'servidor &'`) mantém o EOF longe: `poll`/`wait` devem reportar o fim do líder mesmo assim. **Revisão final:** o teste que este plano citava (`TestProcessCloseKillsTheWholeGroup`, com `; wait`) mantém o líder vivo e não cobre o caso; a cobertura veio no passo de correção com `TestProcessCloseKillsAGrandchildAfterTheLeaderExited`, `TestProcessTerminateReachesAGrandchildAfterTheLeaderExited` e `TestExitStatusPaysTheDrainGraceOnlyOnce` (`builtins_process_unix_test.go`).
2. Saída que chega logo antes do fim: depois de `wait` devolver, tudo o que o líder escreveu está em `read` (Task 2: `TestProcessStartWaitCapturesOutput` lê **depois** do `wait`).
3. Pedaço partindo um caractere UTF-8 (Task 1: `TestIncompleteUTF8TailIsHeldBack`, `TestProcessTakeHoldsIncompleteSequenceUntilEOF`).
4. `terminate` num processo que já saiu sozinho: `false`, sem erro, e `close` continua idempotente (Task 2: `TestProcessTerminateAfterExitIsFalse`).
5. `wait_for` com prazo negativo é erro de runtime, não espera infinita (Task 2: `TestProcessWaitForRejectsNegativeTimeout`).

---

## File map

| Arquivo | Estado | Responsabilidade |
|---|---|---|
| `internal/vm/process_tree_unix.go` (`unix`) | novo | `processTree` por pgid: `signal(force)`, `release()`; `configureProcessGroupUnguarded` |
| `internal/vm/process_tree_linux.go` (`linux`) | novo | `configureProcessGroup` com `Pdeathsig`; `deathGuardRefused` |
| `internal/vm/process_tree_unix_other.go` (`unix && !linux`) | novo | `configureProcessGroup` sem guarda; `deathGuardRefused` = false |
| `internal/vm/process_tree_windows.go` (`windows`) | novo | job object `KILL_ON_JOB_CLOSE`; `signal` = `TerminateJobObject` |
| `internal/vm/builtins_process.go` | novo | `ProcessResource`, `startChildProcess`, `drain`/`reap`/`take`/`shutdown`, `incompleteUTF8Tail`, `defineProcessBuiltins`, `CloseProcesses` |
| `internal/vm/vm.go`, `internal/vm/builtins.go` | modificar | campo e construção de `Processes`; `defineProcessBuiltins` na lista |
| `internal/vm/builtins_sys.go` | modificar | `sys_exit` chama `CloseProcesses` |
| `cmd/noxy/main.go` | modificar | `defer machine.CloseProcesses()` em `runREPL` e `runWithConfig` |
| `internal/stdlib/process.nx` | novo | wrappers tipados |
| `internal/vm/builtins_process_test.go`, `builtins_process_unix_test.go` | novo | testes |
| `internal/vm/builtins_registry_test.go`, `architecture_test.go` | modificar | snapshot; tabelas |
| `docs/NOXY_LANGUAGE_SPEC.md`, `CHANGELOG.md`, `noxy_examples/process_supervise.nx` | modificar/novo | docs e exemplo |

---

### Task 1: recurso, árvore por plataforma e buffer de saída

**Files:**
- Create: `internal/vm/process_tree_unix.go`, `internal/vm/process_tree_linux.go`, `internal/vm/process_tree_unix_other.go`, `internal/vm/process_tree_windows.go`, `internal/vm/builtins_process.go` (sem os nativos ainda)
- Modify: `internal/vm/vm.go` (struct `SharedState`), `internal/vm/builtins.go:14` (construção do registry)
- Test: `internal/vm/builtins_process_test.go`

**Interfaces:**
- Produces: `type ProcessResource struct`, `func startChildProcess(command string) (*ProcessResource, error)`, `func (p *ProcessResource) take() []byte`, `func (p *ProcessResource) exited() bool`, `func (p *ProcessResource) awaitDrained(limit time.Duration)`, `func (p *ProcessResource) shutdown()`, `func incompleteUTF8Tail(b []byte) int`, `type processTree` com `signal(force bool) bool` e `release()`, `SharedState.Processes *handleRegistry[*ProcessResource]`, `func (s *SharedState) CloseProcesses()`.

- [ ] **Step 1: testes de unidade do buffer (falham: símbolos inexistentes)**

```go
// internal/vm/builtins_process_test.go
package vm

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

// Comandos portateis: `sleep N` em Unix; no cmd, `ping -n N+1` contra o
// loopback dorme ~N segundos (nao ha sleep no Windows sem PowerShell).
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
		{"ol\xc3", 1},           // 'á' pela metade: 1 de 2 bytes
		{"x\xe2\x82", 2},        // '€' pela metade: 2 de 3
		{"\xf0\x9f\x98", 3},     // emoji: 3 de 4
		{"ol\xc3\xa1", 0},       // completo
		{"a\xff", 0},            // lead invalido: entrega como esta
		{"\x80", 0},             // continuacao solta: nada a reter
		{"", 0},
	}
	for _, tc := range cases {
		if got := incompleteUTF8Tail([]byte(tc.in)); got != tc.want {
			t.Errorf("incompleteUTF8Tail(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestProcessTakeHoldsIncompleteSequenceUntilEOF(t *testing.T) {
	resource := &ProcessResource{done: make(chan struct{})}
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
	if !contains(out, "first") || !contains(out, "second") {
		t.Fatalf("saida = %q", out)
	}
	if !resource.exited() || resource.exitCode != 0 {
		t.Fatalf("exited=%v code=%d", resource.exited(), resource.exitCode)
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

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

(Use `strings.Contains` no lugar de `contains`/`indexOf` se o arquivo já importa `strings`; o esboço evita colidir com helpers homônimos do pacote de teste.)

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/vm -run 'TestIncompleteUTF8Tail|TestProcessTake|TestStartChildProcess|TestShutdownKills' -count=1`
Expected: erro de compilação (`undefined: incompleteUTF8Tail`, `ProcessResource`).

- [ ] **Step 3: árvore por plataforma**

```go
// internal/vm/process_tree_unix.go
//go:build unix

package vm

import (
	"os"
	"os/exec"
	"syscall"
)

// processTree e o grupo de processos do lider (pgid == pid, Setpgid):
// terminate/kill alcancam a arvore inteira, nao so o shell (spec §4.2).
type processTree struct{ leader *os.Process }

func newProcessTree(leader *os.Process) processTree { return processTree{leader: leader} }

// signal envia SIGTERM (force=false) ou SIGKILL ao grupo; se o grupo ja
// nao existe, ao lider. false quando nada foi sinalizado.
func (t processTree) signal(force bool) bool {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(-t.leader.Pid, sig); err == nil {
		return true
	}
	return t.leader.Signal(sig) == nil
}

func (t processTree) release() {}

func configureProcessGroupUnguarded(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
```

```go
// internal/vm/process_tree_linux.go
//go:build linux

package vm

import (
	"errors"
	"os/exec"
	"syscall"
)

// configureProcessGroup: grupo proprio (Setpgid) e guarda de morte
// (Pdeathsig): se o Noxy morrer sem passar por CloseProcesses, o kernel
// mata o lider (spec §4.3, best effort — netos sobrevivem).
func configureProcessGroup(cmd *exec.Cmd) bool {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	return true
}

// deathGuardRefused: repetir o Start sem a guarda so quando ela foi
// aplicada e o erro e EPERM (sandbox como o AWS Lambda recusa o prctl) —
// a mesma regra de internal/ext/process_spawn.go.
func deathGuardRefused(guarded bool, err error) bool {
	return guarded && errors.Is(err, syscall.EPERM)
}
```

```go
// internal/vm/process_tree_unix_other.go
//go:build unix && !linux

package vm

import "os/exec"

// Sem Pdeathsig fora do Linux: so o grupo (spec §4.3).
func configureProcessGroup(cmd *exec.Cmd) bool {
	configureProcessGroupUnguarded(cmd)
	return false
}

func deathGuardRefused(bool, error) bool { return false }
```

```go
// internal/vm/process_tree_windows.go
//go:build windows

package vm

import (
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// shellCommand ja preencheu SysProcAttr.CmdLine; nao ha grupo nem guarda
// no Start — o job object entra depois (newProcessTree).
func configureProcessGroup(*exec.Cmd) bool  { return false }
func configureProcessGroupUnguarded(*exec.Cmd) {}
func deathGuardRefused(bool, error) bool    { return false }

// processTree e um job object com KILL_ON_JOB_CLOSE: tudo o que o lider
// criar entra no job, terminate/kill terminam o job e fechar o handle
// (release, ou a morte do Noxy) mata o que restou. Sem job (falha de API)
// sobra Process.Kill no lider.
type processTree struct {
	leader *os.Process
	job    windows.Handle
}

func newProcessTree(leader *os.Process) processTree {
	tree := processTree{leader: leader}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return tree
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return tree
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(leader.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return tree
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return tree
	}
	tree.job = job
	return tree
}

// signal: nao ha SIGTERM no Windows — terminate e kill terminam o job
// (exit code 1 no lider).
func (t processTree) signal(bool) bool {
	if t.job != 0 {
		return windows.TerminateJobObject(t.job, 1) == nil
	}
	return t.leader.Kill() == nil
}

func (t processTree) release() {
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
	}
}
```

- [ ] **Step 4: recurso e buffer (`builtins_process.go`, sem nativos)**

```go
// internal/vm/builtins_process.go
package vm

import (
	"os"
	"os/exec"
	"sync"
	"time"
)

// ProcessResource e um processo filho supervisionado (spec
// 2026-09-26-process-module-design.md): o lider (shell da plataforma) e
// sua arvore, a saida drenada para um buffer e o Wait memoizado numa
// goroutine. Vive em SharedState.Processes; o struct Noxy `Process` guarda
// o handle.
type ProcessResource struct {
	cmd  *exec.Cmd
	tree processTree

	mu       sync.Mutex
	pending  []byte // saida chegada e ainda nao lida
	eof      bool   // o pipe fechou: todo detentor (lider e netos) saiu
	exitCode int
	closed   bool

	done    chan struct{} // fechado quando cmd.Wait devolveu
	drained chan struct{} // fechado no EOF do pipe
}

// startChildProcess sobe `command` pelo shell da plataforma com stdout e
// stderr num unico pipe e stdin no dispositivo nulo. A guarda de morte e
// best effort: em EPERM (Lambda) repete sem ela, como as extensoes.
func startChildProcess(command string) (*ProcessResource, error) {
	cmd, reader, guarded, err := launchChild(command, true)
	if err != nil && deathGuardRefused(guarded, err) {
		cmd, reader, _, err = launchChild(command, false)
	}
	if err != nil {
		return nil, err
	}
	resource := &ProcessResource{
		cmd:     cmd,
		tree:    newProcessTree(cmd.Process),
		done:    make(chan struct{}),
		drained: make(chan struct{}),
	}
	go resource.drain(reader)
	go resource.reap()
	return resource, nil
}

// launchChild monta e inicia o comando; o pipe pertence a tentativa, entao
// repetir sem a guarda exige comando e pipe novos.
func launchChild(command string, guard bool) (cmd *exec.Cmd, reader *os.File, guarded bool, err error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, false, err
	}
	cmd = shellCommand(command)
	cmd.Stdin = nil // dispositivo nulo: sem stdin (spec §1)
	cmd.Stdout = writer
	cmd.Stderr = writer
	if guard {
		guarded = configureProcessGroup(cmd)
	} else {
		configureProcessGroupUnguarded(cmd)
	}
	err = cmd.Start()
	// O filho tem a copia dele; o EOF do nosso lado depende de fechar esta.
	_ = writer.Close()
	if err != nil {
		_ = reader.Close()
		return nil, nil, guarded, err
	}
	return cmd, reader, guarded, nil
}

func (p *ProcessResource) drain(reader *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			p.mu.Lock()
			p.pending = append(p.pending, buf[:n]...)
			p.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	_ = reader.Close()
	p.mu.Lock()
	p.eof = true
	p.mu.Unlock()
	close(p.drained)
}

func (p *ProcessResource) reap() {
	err := p.cmd.Wait()
	code := 0
	if p.cmd.ProcessState != nil {
		code = p.cmd.ProcessState.ExitCode()
	} else if err != nil {
		code = -1
	}
	p.mu.Lock()
	p.exitCode = code
	p.mu.Unlock()
	close(p.done)
}

// take entrega a saida acumulada e esvazia o buffer. Antes do EOF um
// pedaco nunca termina no meio de uma sequencia UTF-8: os bytes de uma
// sequencia incompleta ficam para a proxima leitura (spec §4.1).
func (p *ProcessResource) take() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	cut := len(p.pending)
	if !p.eof {
		cut -= incompleteUTF8Tail(p.pending)
	}
	out := append([]byte(nil), p.pending[:cut]...)
	p.pending = append([]byte(nil), p.pending[cut:]...)
	return out
}

// incompleteUTF8Tail conta os bytes finais que sao o comeco de uma
// sequencia UTF-8 ainda sem todos os bytes (1 a 3); 0 quando a cauda esta
// completa ou e invalida (um lead byte invalido nao vai ficar completo).
func incompleteUTF8Tail(b []byte) int {
	n := len(b)
	for i := 1; i <= 3 && i <= n; i++ {
		c := b[n-i]
		if c&0xC0 == 0x80 { // continuacao: procurar o lead byte mais atras
			continue
		}
		var need int
		switch {
		case c&0x80 == 0:
			need = 1
		case c&0xE0 == 0xC0:
			need = 2
		case c&0xF0 == 0xE0:
			need = 3
		case c&0xF8 == 0xF0:
			need = 4
		default:
			return 0
		}
		if need > i {
			return i
		}
		return 0
	}
	return 0
}

func (p *ProcessResource) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// awaitDrained espera o EOF do pipe ate `limit`: depois que o lider saiu,
// o que ele escreveu esta no pipe e a goroutine de drain so precisa de um
// instante para busca-lo; um neto que herdou o pipe segura o EOF, e ai o
// limite devolve o controle (spec §4.1).
func (p *ProcessResource) awaitDrained(limit time.Duration) {
	select {
	case <-p.drained:
	case <-time.After(limit):
	}
}

// exitStatus e (running, exit_code) para poll/wait_for: ao observar o
// fim, garante que a saida escrita ate ele esta no buffer.
func (p *ProcessResource) exitStatus() (running bool, code int) {
	if !p.exited() {
		return true, -1
	}
	p.awaitDrained(100 * time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	return false, p.exitCode
}

// shutdown mata o que ainda roda e libera a arvore; idempotente. Chamado
// por process_close e por CloseProcesses (saida do Noxy).
func (p *ProcessResource) shutdown() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	eof := p.eof
	p.mu.Unlock()
	if !p.exited() {
		p.tree.signal(true)
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
		}
	} else if !eof {
		// Lider ja reaped, pipe aberto: um neto ainda vive. E a unica
		// condicao em que o grupo e sinalizado depois do lider (spec §4.2),
		// para nao sinalizar um pgid reutilizado.
		p.tree.signal(true)
	}
	p.tree.release()
}

// CloseProcesses fecha (mata) todo processo ainda registrado. Chamado em
// sys_exit e por defer na CLI e no REPL — ao lado de CloseExtensions.
func (s *SharedState) CloseProcesses() {
	if s.Processes == nil {
		return
	}
	for handle := range s.Processes.snapshot() {
		if resource, ok := s.Processes.remove(handle); ok {
			resource.shutdown()
		}
	}
}

func (vm *VM) CloseProcesses() { vm.shared.CloseProcesses() }
```

- [ ] **Step 5: registry no `SharedState`**

Em `internal/vm/vm.go`, após `Statements   *handleRegistry[*StatementResource]`, adicionar `Processes    *handleRegistry[*ProcessResource]`. Em `internal/vm/builtins.go`, após `shared.Statements = newHandleRegistry[*StatementResource]()`, adicionar `shared.Processes = newHandleRegistry[*ProcessResource]()`.

- [ ] **Step 6: rodar os testes**

Run: `go build ./... && GOOS=windows go vet ./internal/vm && go test ./internal/vm -run 'TestIncompleteUTF8Tail|TestProcessTake|TestStartChildProcess|TestShutdownKills' -count=1`
Expected: PASS.

---

### Task 2: nativos, wrapper `process.nx` e testes de linguagem

**Files:**
- Modify: `internal/vm/builtins_process.go` (adicionar `defineProcessBuiltins`), `internal/vm/builtins.go:36-49` (chamar), `internal/vm/builtins_registry_test.go` (snapshot), `internal/vm/architecture_test.go:60-77` e `:1061-1070` (tabelas)
- Create: `internal/stdlib/process.nx`, `internal/vm/builtins_process_unix_test.go`
- Test: `internal/vm/builtins_process_test.go` (acrescentar)

**Interfaces:**
- Consumes: Task 1.
- Produces: nativos `process_start(cmd, Process)`, `process_read(p)`, `process_poll(p, ProcessStatus)`, `process_wait(p)`, `process_wait_for(p, ms, ProcessStatus)`, `process_terminate(p)`, `process_kill(p)`, `process_close(p)`; wrappers `process.start/read/poll/wait/wait_for/terminate/kill/close`.

- [ ] **Step 1: testes de linguagem (falham: módulo inexistente)**

```go
// acrescentar a internal/vm/builtins_process_test.go
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
sys.sleep(300)
let first: bytes = process.read(p)
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
```

```go
// internal/vm/builtins_process_unix_test.go
//go:build unix

package vm

import (
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// close mata a arvore, nao so o shell: o pid de um `sleep` em background
// lido da saida do lider deixa de existir depois do close.
func TestProcessCloseKillsTheWholeGroup(t *testing.T) {
	cells := processCells(t, `
use process
use sys
let p: process.Process = process.start("sleep 30 & echo $!; wait")
sys.sleep(300)
let out: bytes = process.read(p)
process.close(p)
test_report([to_str(out)])`)
	pid, err := strconv.Atoi(strings.TrimSpace(cells[0]))
	if err != nil {
		t.Fatalf("pid do neto ilegivel em %q: %v", cells[0], err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return // ESRCH: o neto morreu
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("o neto %d continua vivo depois de close", pid)
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/vm -run 'TestProcess' -count=1`
Expected: FAIL com `module not found: process` / `undefined global`.

- [ ] **Step 3: nativos**

```go
// acrescentar a internal/vm/builtins_process.go (imports: "fmt", "github.com/estevaofon/noxy/internal/value")
func (vm *VM) defineProcessBuiltins() {
	vm.DefineContextualNative("process_start", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 2 {
			return value.NewNull(), fmt.Errorf("process_start expects a command and the Process struct")
		}
		definition, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), fmt.Errorf("process_start: argument 2 must be the Process struct")
		}
		resource, startErr := startChildProcess(args[0].String())
		if startErr != nil {
			return processValue(definition, -1, -1, false, startErr.Error()), nil
		}
		handle := machine.shared.Processes.add(resource)
		return processValue(definition, handle, resource.cmd.Process.Pid, true, ""), nil
	})
	vm.DefineContextualNative("process_read", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		resource, err := openProcess(context, args, 1)
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewBytes(string(resource.take())), nil
	})
	vm.DefineContextualNative("process_poll", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		resource, err := openProcess(context, args, 2)
		if err != nil {
			return value.NewNull(), err
		}
		definition, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), fmt.Errorf("process_poll: argument 2 must be the ProcessStatus struct")
		}
		running, code := resource.exitStatus()
		return statusValue(definition, running, code), nil
	})
	vm.DefineContextualNative("process_wait", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		resource, err := openProcess(context, args, 1)
		if err != nil {
			return value.NewNull(), err
		}
		<-resource.done
		_, code := resource.exitStatus()
		return value.NewInt(int64(code)), nil
	})
	vm.DefineContextualNative("process_wait_for", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		resource, err := openProcess(context, args, 3)
		if err != nil {
			return value.NewNull(), err
		}
		timeout := args[1].Int()
		if args[1].Type != value.VAL_INT || timeout < 0 {
			return value.NewNull(), fmt.Errorf("process: wait_for: timeout must be >= 0")
		}
		definition, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), fmt.Errorf("process_wait_for: argument 3 must be the ProcessStatus struct")
		}
		select {
		case <-resource.done:
		case <-time.After(time.Duration(timeout) * time.Millisecond):
		}
		running, code := resource.exitStatus()
		return statusValue(definition, running, code), nil
	})
	vm.DefineContextualNative("process_terminate", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		resource, err := openProcess(context, args, 1)
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewBool(!resource.exited() && resource.tree.signal(false)), nil
	})
	vm.DefineContextualNative("process_kill", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		resource, err := openProcess(context, args, 1)
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewBool(!resource.exited() && resource.tree.signal(true)), nil
	})
	vm.DefineContextualNative("process_close", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		machine, err := nativeVM(context)
		if err != nil {
			return value.NewNull(), err
		}
		if len(args) < 1 {
			return value.NewNull(), nil
		}
		if resource, ok := machine.shared.Processes.remove(processHandle(args[0])); ok {
			resource.shutdown()
		}
		return value.NewNull(), nil
	})
}

// openProcess resolve o Process do argumento 1 no registry; handle
// fechado ou `ok=false` e erro de runtime (misuse, spec §4.4).
func openProcess(context value.NativeContext, args []value.Value, arity int) (*ProcessResource, error) {
	machine, err := nativeVM(context)
	if err != nil {
		return nil, err
	}
	if len(args) < arity {
		return nil, fmt.Errorf("process: expected %d arguments, got %d", arity, len(args))
	}
	handle := processHandle(args[0])
	resource, ok := machine.shared.Processes.get(handle)
	if !ok {
		return nil, fmt.Errorf("process: handle %d is not an open process", handle)
	}
	return resource, nil
}

func processHandle(arg value.Value) int {
	instance, ok := arg.Obj.(*value.ObjInstance)
	if arg.Type != value.VAL_OBJ || !ok {
		return -1
	}
	return int(instance.Field("handle").Int())
}

func processValue(definition *value.ObjStruct, handle, pid int, ok bool, errorText string) value.Value {
	return value.NewInstanceWith(definition, map[string]value.Value{
		"handle": value.NewInt(int64(handle)),
		"pid":    value.NewInt(int64(pid)),
		"ok":     value.NewBool(ok),
		"error":  value.NewString(errorText),
	})
}

func statusValue(definition *value.ObjStruct, running bool, code int) value.Value {
	return value.NewInstanceWith(definition, map[string]value.Value{
		"running":   value.NewBool(running),
		"exit_code": value.NewInt(int64(code)),
	})
}
```

Em `internal/vm/builtins.go`, adicionar `vm.defineProcessBuiltins()` após `vm.defineSystemBuiltins()`.

- [ ] **Step 4: wrapper**

```noxy
// stdlib/process.nx — processo filho supervisionado (spec de design
// 2026-09-26-process-module-design.md): sobe pelo shell da plataforma,
// saida lida aos pedacos sem bloquear, parada pela arvore inteira. O
// handle e o dono: close (ou a saida do Noxy) encerra o que ainda roda.

struct Process
    handle: int,     // chave no registry; -1 quando ok=false
    pid: int,        // do lider (o shell); -1 quando ok=false
    ok: bool,        // subiu
    error: string    // por que nao subiu
end

struct ProcessStatus
    running: bool,
    exit_code: int   // -1 enquanto roda ou morto por sinal (Unix); 1 se o job foi terminado (Windows)
end

// Sobe `cmd` pelo shell (sh -c / cmd /S /C), stdout+stderr capturados
// num unico fluxo, stdin no dispositivo nulo.
func start(cmd: string) -> Process
    return process_start(cmd, Process)
end

// O que chegou desde a ultima leitura (b"" se nada). Nunca bloqueia; um
// pedaco nunca parte uma sequencia UTF-8 antes do fim do fluxo.
func read(p: Process) -> bytes
    return process_read(p)
end

// Estado do lider, sem bloquear.
func poll(p: Process) -> ProcessStatus
    return process_poll(p, ProcessStatus)
end

// Bloqueia ate o lider sair; devolve o exit code.
func wait(p: Process) -> int
    return process_wait(p)
end

// Espera ate timeout_ms; running=true se o prazo venceu.
func wait_for(p: Process, timeout_ms: int) -> ProcessStatus
    return process_wait_for(p, timeout_ms, ProcessStatus)
end

// Pede parada a arvore: SIGTERM ao grupo (Unix); termina o job (Windows).
// false se nada foi sinalizado (ja saiu).
func terminate(p: Process) -> bool
    return process_terminate(p)
end

// Forca: SIGKILL ao grupo (Unix); termina o job (Windows).
func kill(p: Process) -> bool
    return process_kill(p)
end

// Mata o que ainda roda e libera o handle. Idempotente.
func close(p: Process) -> void
    process_close(p)
end
```

- [ ] **Step 5: snapshot e tabelas de arquitetura**

`builtins_registry_test.go`: entre `"print",` e `"range",` inserir `"process_close", "process_kill", "process_poll", "process_read", "process_start", "process_terminate", "process_wait", "process_wait_for",`. `architecture_test.go`: `"builtins_process.go": {"defineProcessBuiltins"},` na tabela de `TestBuiltinSourceLayout`; `"Processes": "*handleRegistry[*ProcessResource]",` em `wantRegistries`.

- [ ] **Step 6: rodar**

Run: `go build ./... && go vet ./internal/vm && GOOS=windows go vet ./internal/vm && go test ./internal/vm -run 'TestProcess|TestBuiltinRegistrySnapshot|TestBuiltinSourceLayout|TestResourceRegistries' -count=1`
Expected: PASS.

---

### Task 3: limpeza na saída do Noxy

**Files:**
- Modify: `internal/vm/builtins_sys.go:429-439` (`sys_exit`), `cmd/noxy/main.go:252` e `:428` (defers)
- Test: `internal/vm/builtins_process_test.go`

- [ ] **Step 1: teste (falha: `CloseProcesses` não mata nada… existe desde a Task 1, então este teste passa direto — ele fixa o contrato)**

```go
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
```

- [ ] **Step 2: `sys_exit` e CLI**

Em `sys_exit`, antes de `vm.shared.CloseExtensions()`: `vm.shared.CloseProcesses()` com o comentário `// os.Exit nao roda defers: mata os processos filhos registrados (spec process §4.3) e fecha os plugins por processo aqui`. Em `cmd/noxy/main.go`, logo após cada `defer machine.CloseExtensions()`: `defer machine.CloseProcesses()` com o comentário `// Processos filhos de `process.start` morrem com o Noxy (spec process §4.3).`

- [ ] **Step 3: rodar**

Run: `go build ./... && go test ./internal/vm -run 'TestCloseProcesses' -count=1 && go test ./cmd/... -count=1`
Expected: PASS.

---

### Task 4: docs, exemplo e verificação completa

**Files:**
- Modify: `docs/NOXY_LANGUAGE_SPEC.md` (§12: tabela de módulos + seção `### Processes (`process`)` antes de `### JSON`), `CHANGELOG.md` (0.26.0: parágrafo de abertura e entrada em *Added*)
- Create: `noxy_examples/process_supervise.nx`

- [ ] **Step 1: exemplo**

```noxy
// process_supervise.nx — um programa rodando outro: saída aos pedaços
// enquanto ele roda, estado sem bloquear, parada da árvore inteira.
use process
use sys
use strings

func assert(cond: bool, msg: string) -> void
    if !cond then
        print("FALHOU: " + msg)
        sys.exit(1)
    end
end

let windows: bool = sys.os() == "windows"

// 1. Saída incremental: o programa escreve, dorme um segundo, escreve de novo.
let cmd: string = "echo primeira; sleep 1; echo segunda"
if windows then
    cmd = "echo primeira&ping -n 2 127.0.0.1 >nul&echo segunda"
end
let p: process.Process = process.start(cmd)
assert(p.ok, "start: " + p.error)
let saida: bytes = b""
let leituras: int = 0
while process.poll(p).running do
    let pedaco: bytes = process.read(p)
    if length(pedaco) > 0 then
        leituras = leituras + 1
    end
    saida = saida + pedaco
    sys.sleep(50)
end
saida = saida + process.read(p)
let texto: string = to_str(saida)
iprint(texto)
assert(strings.contains(texto, "primeira") && strings.contains(texto, "segunda"), "saída completa")
assert(leituras >= 1, "houve leitura enquanto rodava")
let fim: process.ProcessStatus = process.poll(p)
assert(!fim.running && fim.exit_code == 0, "exit code 0")
process.close(p)

// 2. Parar um processo que não termina sozinho.
let longo: process.Process = process.start("sleep 30")
if windows then
    longo = process.start("ping -n 31 127.0.0.1 >nul")
end
assert(process.poll(longo).running, "longo está rodando")
assert(process.terminate(longo), "terminate sinalizou")
let parado: process.ProcessStatus = process.wait_for(longo, 5000)
assert(!parado.running, "parou dentro do prazo")
process.close(longo)

print("process_supervise: ok")
```

- [ ] **Step 2: spec §12**

Linha na tabela de módulos: `| \`process\` | Child processes: start, read output without blocking, poll/wait, terminate/kill the whole tree |`. Seção `### Processes (\`process\`)` com: a API (tabela), stdin nulo e saída combinada, regra da fronteira UTF-8, EOF × fim do líder, `terminate`/`kill` por plataforma, `close` e a saída do Noxy (best effort na morte dura), erros de handle, e o exemplo curto do laço `poll`/`read`.

- [ ] **Step 3: CHANGELOG**

Trocar, no parágrafo de abertura da 0.26.0, "o achado 1 (...) é um subsistema novo e fica para uma spec de design" por "o achado 1 é o módulo `process`, desenhado em `docs/superpowers/specs/2026-09-26-process-module-design.md`". Entrada em *Added*: `**Módulo \`process\`** (achado 1): \`process.start(cmd) -> Process\` ...` descrevendo as oito funções, o pipe único, a fronteira UTF-8, grupo/job, `close` e `CloseProcesses`.

- [ ] **Step 4: verificação completa**

Run: `go build ./... && go vet ./... && GOOS=windows go vet ./internal/vm && go test ./internal/... ./cmd/... -count=1 && go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx && gofmt -l internal/ cmd/`
Expected: tudo verde, `gofmt -l` vazio.

---

## Self-review

- **Cobertura da spec:** §3 API → Task 2; §4.1 saída/UTF-8 → Task 1 (`take`, `incompleteUTF8Tail`) e Task 2 (leitura incremental); §4.2 árvore/`close` → Task 1 (`shutdown`, `processTree`) e Task 2 (`TestProcessCloseKillsTheWholeGroup`); §4.3 saída do Noxy → Task 3; §4.4 erros → Task 2 (handle fechado, timeout negativo; `start` que não sobe devolve `ok=false` em `process_start`, sem teste automático: exige shell ausente); §5 arquivos → file map; §7 docs → Task 4.
- **Tipos:** `processTree.signal(force bool) bool` e `release()` iguais nos quatro arquivos de plataforma; `exitStatus() (running bool, code int)` usado por `process_poll`, `process_wait`, `process_wait_for`.
- **Review Focus:** 1 → `TestProcessCloseKillsTheWholeGroup` (o líder `wait`a o neto; `close` mata os dois); 2 → `TestProcessStartWaitCapturesOutput`; 3 → testes de unidade da Task 1; 4 → `TestProcessTerminateAfterExitIsFalse`; 5 → `TestProcessWaitForRejectsNegativeTimeout`.

---

## Passo de correção após a revisão final (executado)

Cinco Important e cinco Minor regradeados pelo efeito entraram (ver ledger no relatório da sessão): carência de drain paga uma vez (`poll` nunca bloqueia); tratador de Ctrl+C/SIGTERM na CLI (`cmd/noxy/signals.go`, `VM.SignalSubscribed`); `terminate`/`kill` alcançam o grupo com líder reaped e pipe aberto; testes do ramo "líder reaped, pipe aberto"; laço limitado de leitura em vez de `sleep(300)`; `shutdown` fecha o lado de leitura do pipe; guarda `closed` em `signal`; saturação de `wait_for`; docs (CHANGELOG, README, site, spec §12); teste tolerante a zumbi. Diferido: teste do retry sem `Pdeathsig` (EPERM).
