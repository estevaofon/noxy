package vm

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/estevaofon/noxy/internal/value"
)

// ProcessResource e um processo filho supervisionado (spec de design
// docs/superpowers/specs/2026-09-26-process-module-design.md, achado 1 do
// Noxy-Editor): o lider (shell da plataforma) e sua arvore, a saida
// drenada para um buffer e o Wait memoizado numa goroutine. Vive em
// SharedState.Processes; o struct Noxy `Process` guarda o handle.
type ProcessResource struct {
	cmd  *exec.Cmd
	tree processTree

	mu       sync.Mutex
	reader   *os.File // lado de leitura do pipe; shutdown o fecha para a drain terminar
	pending  []byte   // saida chegada e ainda nao lida
	eof      bool     // o pipe fechou: todo detentor (lider e netos) saiu
	exitCode int
	settled  bool // a carencia de drain apos o fim do lider ja foi paga
	closed   bool

	done    chan struct{} // fechado quando cmd.Wait devolveu
	drained chan struct{} // fechado no EOF do pipe
}

// startChildProcess sobe `command` pelo shell da plataforma (shellCommand,
// o mesmo de sys.exec) com stdout e stderr num unico pipe e stdin no
// dispositivo nulo. A guarda de morte e best effort: em EPERM (Lambda)
// repete sem ela, como as extensoes por processo.
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
		reader:  reader,
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
	cmd.Stdin = nil // dispositivo nulo: sem stdin (spec §1, fora de escopo)
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
// completa ou e invalida (um lead byte invalido nunca vai ficar completo).
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

// exitStatus e (running, exit_code) para poll/wait/wait_for: na PRIMEIRA
// observacao do fim, espera a carencia de drain para que a saida escrita
// ate ele esteja no buffer; as seguintes voltam na hora — com um neto
// segurando o pipe, pagar a carencia a cada poll fazia "nunca bloqueia"
// custar 100 ms por chamada (revisao, achado 1).
func (p *ProcessResource) exitStatus() (running bool, code int) {
	if !p.exited() {
		return true, -1
	}
	p.mu.Lock()
	settled := p.settled
	p.mu.Unlock()
	if !settled {
		p.awaitDrained(100 * time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.settled = true
	return false, p.exitCode
}

// signal e o terminate/kill do recurso: alcanca a arvore enquanto o lider
// roda OU o pipe continua aberto (um neto vive e, com ele, o grupo — um
// pgid nao e reutilizado enquanto tem membro). Depois de close, ou com o
// lider reaped e o fluxo encerrado, nada a sinalizar: false. A guarda de
// `closed` sob o mutex e o que impede sinalizar um job do Windows cujo
// handle o close ja liberou.
func (p *ProcessResource) signal(force bool) bool {
	p.mu.Lock()
	closed, eof := p.closed, p.eof
	p.mu.Unlock()
	if closed || (p.exited() && eof) {
		return false
	}
	return p.tree.signal(force)
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
	// Um escritor que saiu do grupo (setsid) sobrevive ao kill e seguraria
	// a drain, o fd e um buffer sem leitor pelo resto do programa: fechar o
	// lado de leitura faz o Read pendente voltar e a drain terminar; o
	// escritor recebe EPIPE (revisao, achado 6).
	if p.reader != nil {
		_ = p.reader.Close()
	}
}

// CloseProcesses fecha (mata) todo processo ainda registrado. Chamado em
// sys_exit e por defer na CLI e no REPL — ao lado de CloseExtensions
// (spec §4.3).
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
		if args[1].Type != value.VAL_INT || args[1].Int() < 0 {
			return value.NewNull(), fmt.Errorf("process: wait_for: timeout must be >= 0")
		}
		definition, ok := args[2].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull(), fmt.Errorf("process_wait_for: argument 3 must be the ProcessStatus struct")
		}
		select {
		case <-resource.done:
		case <-time.After(millisecondsDuration(args[1].Int())):
		}
		running, code := resource.exitStatus()
		return statusValue(definition, running, code), nil
	})
	vm.DefineContextualNative("process_terminate", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		resource, err := openProcess(context, args, 1)
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewBool(resource.signal(false)), nil
	})
	vm.DefineContextualNative("process_kill", func(context value.NativeContext, args []value.Value) (value.Value, error) {
		resource, err := openProcess(context, args, 1)
		if err != nil {
			return value.NewNull(), err
		}
		return value.NewBool(resource.signal(true)), nil
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

// millisecondsDuration satura em vez de estourar: um prazo acima de
// ~292 anos virava negativo na multiplicacao e wait_for voltava na hora.
func millisecondsDuration(ms int64) time.Duration {
	if ms > int64(math.MaxInt64)/int64(time.Millisecond) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ms) * time.Millisecond
}
