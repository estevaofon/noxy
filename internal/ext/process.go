// internal/ext/process.go
package ext

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"noxy-vm/internal/value"
)

// Carencias do host (spec §2.7, §4.3). Variaveis para os testes encurtarem.
var (
	cancelGrace   = time.Second
	shutdownGrace = 2 * time.Second
)

// procConn e o que um plugin em execucao parece ao host: os dois pipes e
// wait/kill. execConn (process_spawn.go) e o real; os testes usam io.Pipe.
type procConn interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Wait() error
	Kill() error
}

type spawnFunc func(ctx context.Context) (procConn, error)

type ProcessConfig struct {
	// Path e o binario a executar, absoluto (spec §2.1).
	Path        string
	NoxyVersion string
	Log         io.Writer // destino dos LOG; nil → os.Stderr
	Limits      Limits    // zero → DefaultLimits()
}

// reply e o que uma chamada em voo recebe: RESULT (body), ERROR (failed +
// msg) ou a morte do processo (err).
type reply struct {
	body   []byte
	failed bool
	msg    string
	err    error
}

// hostKill e a causa de uma morte decidida pelo host (timeout sem cancel).
type hostKill struct{ reason string }

func (e *hostKill) Error() string { return e.reason }

// Process e o backend de kind = "process" (spec 2026-08-29): um processo por
// extensao, multiplexado por id, subido na primeira chamada.
type Process struct {
	Manifest *Manifest

	cfg     ProcessConfig
	logOut  io.Writer
	limits  Limits
	exports []string
	spawn   spawnFunc

	// callMu serializa a troca CALL/resposta em concurrency = "single"
	// (spec §5) — nao o start.
	callMu sync.Mutex

	// mu guarda o estado abaixo; writeMu serializa escritas no stdin do
	// plugin (um quadro inteiro por vez).
	mu       sync.Mutex
	writeMu  sync.Mutex
	conn     procConn
	alive    bool
	poisoned bool
	closed   bool
	pending  map[uint32]chan reply
	nextID   uint32
	deathErr error
	// dying e a primeira causa registrada para a conexao atual: o kill do
	// host (timeout sem cancel, violacao) precede o EOF que ele mesmo
	// provoca no leitor, e e a causa que deve aparecer no erro.
	dying error
}

var _ Backend = (*Process)(nil)

func NewProcess(manifest *Manifest, cfg ProcessConfig) *Process {
	return newProcess(manifest, cfg, execSpawner(cfg.Path))
}

func newProcess(manifest *Manifest, cfg ProcessConfig, spawn spawnFunc) *Process {
	logOut := cfg.Log
	if logOut == nil {
		logOut = os.Stderr
	}
	limits := cfg.Limits
	if limits.MaxBytes == 0 {
		limits = DefaultLimits()
	}
	exports := make([]string, len(manifest.Exports))
	for i, exp := range manifest.Exports {
		exports[i] = exp.Name
	}
	return &Process{
		Manifest: manifest,
		cfg:      cfg,
		logOut:   logOut,
		limits:   limits,
		exports:  exports,
		spawn:    spawn,
		pending:  map[uint32]chan reply{},
	}
}

// ensureStarted sobe o processo e faz o handshake na primeira chamada
// (spec §4.1, §4.2). Chamadas concorrentes esperam o mesmo start sob mu.
func (p *Process) ensureStarted(ctx context.Context) error {
	name := p.Manifest.Name
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("extension '%s' was closed at exit", name)
	}
	if p.alive {
		return nil
	}
	if p.poisoned {
		return fmt.Errorf("extension '%s' is poisoned by an earlier trap", name)
	}
	conn, err := p.spawn(ctx)
	if err != nil {
		p.poisoned = !p.Manifest.Restart
		return fmt.Errorf("extension '%s' trapped: start: %v", name, err)
	}
	reader := bufio.NewReaderSize(conn.Stdout(), 64<<10)
	if err := p.handshake(conn, reader); err != nil {
		_ = conn.Kill()
		_ = conn.Wait()
		p.poisoned = !p.Manifest.Restart
		return fmt.Errorf("extension '%s' trapped: handshake: %v", name, err)
	}
	p.conn = conn
	p.alive = true
	p.deathErr = nil
	go p.readLoop(conn, reader)
	return nil
}

func (p *Process) handshake(conn procConn, reader *bufio.Reader) error {
	body, err := helloBody(p.cfg.NoxyVersion, p.Manifest.Name, p.exports, p.limits)
	if err != nil {
		return err
	}
	if err := WriteFrame(conn.Stdin(), Frame{Kind: FrameHello, Body: body}); err != nil {
		return fmt.Errorf("write HELLO: %v", err)
	}
	type outcome struct {
		frame Frame
		err   error
	}
	replies := make(chan outcome, 1)
	go func() {
		for {
			f, err := ReadFrame(reader, p.limits.MaxBytes)
			if err != nil {
				replies <- outcome{err: err}
				return
			}
			// LOG antes do HELLO do plugin e permitido (spec §2.4).
			if f.Kind == FrameLog {
				if err := p.printLog(f); err != nil {
					replies <- outcome{err: err}
					return
				}
				continue
			}
			replies <- outcome{frame: f}
			return
		}
	}()
	var deadline <-chan time.Time
	if d := p.Manifest.HandshakeTimeout(); d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case out := <-replies:
		if out.err != nil {
			if errors.Is(out.err, io.EOF) || errors.Is(out.err, io.ErrUnexpectedEOF) {
				return fmt.Errorf("process exited before replying")
			}
			return out.err
		}
		return p.checkHello(out.frame)
	case <-deadline:
		// ensureStarted mata o processo; o leitor acima ve EOF e termina.
		return fmt.Errorf("no reply within %v", p.Manifest.HandshakeTimeout())
	}
}

func (p *Process) checkHello(f Frame) error {
	switch f.Kind {
	case FrameError:
		msg, err := p.errorMessage(f)
		if err != nil {
			return fmt.Errorf("plugin refused with a malformed ERROR: %v", err)
		}
		return fmt.Errorf("plugin refused: %s", msg)
	case FrameHello:
	default:
		return fmt.Errorf("unexpected frame kind 0x%02x before HELLO", f.Kind)
	}
	if f.ID != 0 {
		return fmt.Errorf("HELLO carries call id %d", f.ID)
	}
	m, err := decodeBodyMap(f.Body, p.limits)
	if err != nil {
		return fmt.Errorf("HELLO body: %v", err)
	}
	proto, ok := mapString(m, "protocol")
	if !ok {
		return fmt.Errorf("HELLO without a protocol field")
	}
	if proto != ProtocolVersion {
		return fmt.Errorf("plugin speaks %q, host speaks %q", proto, ProtocolVersion)
	}
	return nil
}

func (p *Process) errorMessage(f Frame) (string, error) {
	m, err := decodeBodyMap(f.Body, p.limits)
	if err != nil {
		return "", err
	}
	msg, ok := mapString(m, "message")
	if !ok {
		return "", fmt.Errorf("missing message")
	}
	return msg, nil
}

func (p *Process) printLog(f Frame) error {
	m, err := decodeBodyMap(f.Body, p.limits)
	if err != nil {
		return &ProtocolError{Detail: "LOG body: " + err.Error()}
	}
	msg, ok := mapString(m, "message")
	if !ok {
		return &ProtocolError{Detail: "LOG without a message field"}
	}
	fmt.Fprintf(p.logOut, "[ext %s] %s\n", p.Manifest.Name, msg)
	return nil
}

// readLoop demultiplexa as respostas por id (spec §5). Qualquer erro de
// leitura ou quadro fora de lugar encerra o processo (§4.4, §6).
func (p *Process) readLoop(conn procConn, reader *bufio.Reader) {
	for {
		f, err := ReadFrame(reader, p.limits.MaxBytes)
		if err != nil {
			p.die(conn, err)
			return
		}
		switch f.Kind {
		case FrameLog:
			if err := p.printLog(f); err != nil {
				p.die(conn, err)
				return
			}
		case FrameResult, FrameError:
			if err := p.deliver(f); err != nil {
				p.die(conn, err)
				return
			}
		default:
			p.die(conn, &ProtocolError{Detail: fmt.Sprintf("unexpected frame kind 0x%02x from the plugin", f.Kind)})
			return
		}
	}
}

func (p *Process) deliver(f Frame) error {
	r := reply{}
	switch f.Kind {
	case FrameResult:
		r.body = f.Body
	case FrameError:
		msg, err := p.errorMessage(f)
		if err != nil {
			return &ProtocolError{Detail: fmt.Sprintf("ERROR frame for call %d: %v", f.ID, err)}
		}
		r.failed, r.msg = true, msg
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ch, ok := p.pending[f.ID]
	if !ok {
		return &ProtocolError{Detail: fmt.Sprintf("reply for unknown call id %d", f.ID)}
	}
	delete(p.pending, f.ID)
	ch <- r
	return nil
}

// register reserva um id em voo. Devolve a conexao para que o chamador
// escreva na mesma conexao que registrou — apos a morte, conn e nil.
func (p *Process) register() (uint32, chan reply, procConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive {
		if p.deathErr != nil {
			return 0, nil, nil, p.deathErr
		}
		return 0, nil, nil, fmt.Errorf("extension '%s' is not running", p.Manifest.Name)
	}
	for {
		p.nextID++
		if p.nextID == 0 {
			continue
		}
		if _, busy := p.pending[p.nextID]; !busy {
			break
		}
	}
	ch := make(chan reply, 1)
	p.pending[p.nextID] = ch
	return p.nextID, ch, p.conn, nil
}

func (p *Process) writeFrame(conn procConn, f Frame) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return WriteFrame(conn.Stdin(), f)
}

func (p *Process) Call(ctx context.Context, fnIndex int, args []value.Value) (value.Value, error) {
	name := p.Manifest.Name
	if fnIndex < 0 || fnIndex >= len(p.exports) {
		return value.NewNull(), fmt.Errorf("extension '%s': export index %d out of range", name, fnIndex)
	}
	encoded, err := EncodeArgs(args, p.limits)
	if err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': %w", name, err)
	}
	if p.Manifest.Concurrency == "single" {
		p.callMu.Lock()
		defer p.callMu.Unlock()
	}
	if err := p.ensureStarted(ctx); err != nil {
		return value.NewNull(), err
	}
	id, ch, conn, err := p.register()
	if err != nil {
		return value.NewNull(), err
	}
	if err := p.writeFrame(conn, Frame{Kind: FrameCall, ID: id, Fn: uint32(fnIndex), Body: encoded}); err != nil {
		p.die(conn, err)
		// resposta que empatou com a falha de escrita: nunca vira sucesso silencioso
		r := <-ch
		if r.err != nil {
			return value.NewNull(), r.err
		}
		return p.finish(conn, r, fnIndex)
	}
	var deadline <-chan time.Time
	if d := p.Manifest.CallTimeout(fnIndex); d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case r := <-ch:
		return p.finish(conn, r, fnIndex)
	case <-deadline:
		return p.expire(conn, id, ch, fnIndex)
	}
}

func (p *Process) finish(conn procConn, r reply, fnIndex int) (value.Value, error) {
	name := p.Manifest.Name
	if r.err != nil {
		return value.NewNull(), r.err
	}
	if r.failed {
		return value.NewNull(), fmt.Errorf("extension '%s' failed: %s", name, r.msg)
	}
	result, err := DecodeValue(r.body, p.limits)
	if err != nil {
		// NXB invalido no RESULT e violacao de protocolo (spec §6): o fluxo
		// nao e mais confiavel.
		p.die(conn, &ProtocolError{Detail: "RESULT body: " + err.Error()})
		return value.NewNull(), p.currentDeathErr()
	}
	if err := checkDeclaredReturn(result, p.Manifest.Exports[fnIndex].Returns); err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': %w", name, err)
	}
	return result, nil
}

func (p *Process) currentDeathErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deathErr != nil {
		return p.deathErr
	}
	return fmt.Errorf("extension '%s' trapped: process exited", p.Manifest.Name)
}

// expire trata o prazo vencido (spec §4.3): pede CANCEL, devolve "timed out"
// e, se o plugin nao responder na carencia, mata o processo. Em "single" o
// chamador espera a resposta ao CANCEL (o mutex fica com ele ate la); nos
// outros modos a espera vai para uma goroutine.
func (p *Process) expire(conn procConn, id uint32, ch chan reply, fnIndex int) (value.Value, error) {
	name := p.Manifest.Name
	export := p.exports[fnIndex]
	limit := p.Manifest.CallTimeout(fnIndex).Milliseconds()
	timedOut := fmt.Errorf("extension '%s' timed out: %s exceeded %d ms", name, export, limit)
	if err := p.writeFrame(conn, Frame{Kind: FrameCancel, ID: id}); err != nil {
		p.die(conn, err)
		// resposta que empatou com a falha de escrita: nunca vira sucesso silencioso
		r := <-ch
		if r.err != nil {
			return value.NewNull(), r.err
		}
		return value.NewNull(), timedOut
	}
	await := func() error {
		select {
		case r := <-ch:
			return r.err // nil: cancel honrado, resposta descartada
		case <-time.After(cancelGrace):
			p.die(conn, &hostKill{reason: fmt.Sprintf("%s exceeded %d ms and did not cancel; process killed", export, limit)})
			return (<-ch).err
		}
	}
	if p.Manifest.Concurrency == "single" {
		if err := await(); err != nil {
			return value.NewNull(), err
		}
		return value.NewNull(), timedOut
	}
	go await()
	return value.NewNull(), timedOut
}

// die encerra a conexao (mata, espera) e entrega a causa a toda chamada em
// voo (spec §4.4). A PRIMEIRA causa registrada vence: expire chama die com
// hostKill e so depois o leitor ve o EOF do kill — as duas chamadas
// convergem, e a segunda a chegar ao fim ve p.conn != conn e volta.
func (p *Process) die(conn procConn, cause error) {
	p.mu.Lock()
	if p.conn != conn {
		p.mu.Unlock()
		return
	}
	if p.dying == nil {
		p.dying = cause
	}
	p.mu.Unlock()

	_ = conn.Kill()
	waitErr := conn.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != conn {
		return
	}
	p.alive = false
	p.conn = nil
	p.poisoned = !p.Manifest.Restart
	p.deathErr = p.deathError(p.dying, waitErr)
	p.dying = nil
	for id, ch := range p.pending {
		delete(p.pending, id)
		ch <- reply{err: p.deathErr}
	}
}

func (p *Process) deathError(cause, waitErr error) error {
	name := p.Manifest.Name
	var violation *ProtocolError
	var kill *hostKill
	switch {
	case errors.As(cause, &violation):
		return fmt.Errorf("extension '%s' trapped: %v", name, violation)
	case errors.As(cause, &kill):
		return fmt.Errorf("extension '%s' trapped: %s", name, kill.reason)
	default:
		return fmt.Errorf("extension '%s' trapped: process exited (%s)", name, exitStatus(waitErr))
	}
}

func exitStatus(waitErr error) string {
	var exitErr *exec.ExitError
	switch {
	case errors.As(waitErr, &exitErr):
		return fmt.Sprintf("status %d", exitErr.ExitCode())
	case waitErr != nil:
		return waitErr.Error()
	default:
		return "status 0"
	}
}

// Close fecha o stdin do plugin (EOF = shutdown, spec §2.7), espera a
// carencia e mata o que sobrar. Chamadas depois de Close nao sobem processo.
func (p *Process) Close(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	conn, alive := p.conn, p.alive
	p.mu.Unlock()
	if !alive || conn == nil {
		return nil
	}
	_ = conn.Stdin().Close()
	done := make(chan error, 1)
	go func() { done <- conn.Wait() }()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		_ = conn.Kill()
		<-done
	}
	return nil
}
