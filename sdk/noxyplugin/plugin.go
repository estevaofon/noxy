// sdk/noxyplugin/plugin.go
package noxyplugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"
)

// Version e a versao do SDK anunciada no HELLO ("noxyplugin-go/<Version>").
const Version = "0.1.0"

const (
	maxBody      = 64 << 20
	shutdownWait = time.Second
)

// Level e o nivel de um LOG (spec §2.6).
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Handler atende um export: args ja decodificados, resultado codificado
// pelo SDK. Um erro vira ERROR (`extension 'x' failed: <msg>` no Noxy); um
// panic e recuperado como ERROR "panic: <v>" e o processo continua.
type Handler func(ctx context.Context, args Args) (any, error)

// ExitError e devolvido por Serve quando o host violou o protocolo ou a
// escrita falhou; Main traduz em os.Exit(Code).
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }

type Plugin struct {
	handlers map[string]Handler
	table    []Handler

	out      io.Writer
	writeMu  sync.Mutex
	writeErr error

	calls   map[uint32]context.CancelFunc
	callsMu sync.Mutex
	wg      sync.WaitGroup
	base    context.Context
	stop    context.CancelFunc
}

// current e o plugin em Serve, para Logf.
var current atomic.Pointer[Plugin]

func New() *Plugin {
	return &Plugin{handlers: map[string]Handler{}, calls: map[uint32]context.CancelFunc{}}
}

// Handle registra o handler do export `name` (o nome do manifesto, com o
// prefixo da extensao). Handlers extras sao permitidos; um export do
// manifesto sem handler recusa o handshake.
func (p *Plugin) Handle(name string, h Handler) { p.handlers[name] = h }

// Serve fala noxy-plugin/1 em r/w ate o EOF de r (nil) ou uma violacao do
// host (*ExitError). Uma goroutine por CALL; o host serializa em "single".
func (p *Plugin) Serve(r io.Reader, w io.Writer) error {
	p.out = w
	p.base, p.stop = context.WithCancel(context.Background())
	defer p.stop()
	current.Store(p)
	defer current.CompareAndSwap(p, nil)

	in := bufio.NewReaderSize(r, 64<<10)
	if err := p.handshake(in); err != nil {
		return err
	}
	for {
		f, err := readFrame(in, maxBody)
		if err != nil {
			p.shutdown()
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return &ExitError{Code: 2, Msg: "noxyplugin: " + err.Error()}
		}
		switch f.Kind {
		case kindCall:
			p.dispatch(f)
		case kindCancel:
			p.cancel(f.ID)
		default:
			p.shutdown()
			return &ExitError{Code: 2, Msg: fmt.Sprintf("noxyplugin: unexpected frame kind 0x%02x from the host", f.Kind)}
		}
		if err := p.lastWriteError(); err != nil {
			p.shutdown()
			return &ExitError{Code: 2, Msg: "noxyplugin: write: " + err.Error()}
		}
	}
}

func (p *Plugin) handshake(in *bufio.Reader) error {
	f, err := readFrame(in, maxBody)
	if err != nil {
		return &ExitError{Code: 2, Msg: "noxyplugin: read HELLO: " + err.Error()}
	}
	if f.Kind != kindHello {
		return &ExitError{Code: 2, Msg: fmt.Sprintf("noxyplugin: first frame is 0x%02x, not HELLO", f.Kind)}
	}
	hello, err := decodeStringMap(f.Body)
	if err != nil {
		return &ExitError{Code: 2, Msg: "noxyplugin: HELLO body: " + err.Error()}
	}
	proto, _ := hello["protocol"].(string)
	if proto != protocolVersion {
		p.sendError(0, fmt.Sprintf("unsupported protocol %q (plugin speaks %s)", proto, protocolVersion))
		return &ExitError{Code: 2, Msg: "noxyplugin: unsupported protocol " + proto}
	}
	// Binding por nome (spec §2.4): a tabela por indice nasce da lista do
	// host, e um export sem handler recusa o handshake com o nome.
	rawExports, _ := hello["exports"].([]any)
	table := make([]Handler, len(rawExports))
	for i, raw := range rawExports {
		name, _ := raw.(string)
		h, ok := p.handlers[name]
		if !ok {
			p.sendError(0, fmt.Sprintf("no handler for export %q", name))
			return &ExitError{Code: 2, Msg: "noxyplugin: no handler for export " + name}
		}
		table[i] = h
	}
	p.table = table
	body, err := encodeValue(nil, map[string]any{"protocol": protocolVersion, "sdk": "noxyplugin-go/" + Version}, 0)
	if err != nil {
		return err
	}
	p.send(frame{Kind: kindHello, Body: body})
	if err := p.lastWriteError(); err != nil {
		return &ExitError{Code: 2, Msg: "noxyplugin: write HELLO: " + err.Error()}
	}
	return nil
}

func (p *Plugin) dispatch(f frame) {
	args, err := decodeArgs(f.Body)
	if err != nil {
		p.sendError(f.ID, "invalid arguments: "+err.Error())
		return
	}
	if uint64(f.Fn) >= uint64(len(p.table)) {
		p.sendError(f.ID, fmt.Sprintf("unknown export index %d", f.Fn))
		return
	}
	handler := p.table[f.Fn]
	ctx, cancel := context.WithCancel(p.base)
	p.callsMu.Lock()
	p.calls[f.ID] = cancel
	p.callsMu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() {
			p.callsMu.Lock()
			delete(p.calls, f.ID)
			p.callsMu.Unlock()
			cancel()
		}()
		result, err := invoke(ctx, handler, args)
		if err != nil {
			p.sendError(f.ID, err.Error())
			return
		}
		body, err := encodeValue(nil, result, 0)
		if err != nil {
			p.sendError(f.ID, "result cannot cross the boundary: "+err.Error())
			return
		}
		p.send(frame{Kind: kindResult, ID: f.ID, Body: body})
	}()
}

func invoke(ctx context.Context, h Handler, args Args) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	return h(ctx, args)
}

func (p *Plugin) cancel(id uint32) {
	p.callsMu.Lock()
	cancel := p.calls[id]
	p.callsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// shutdown cancela todo handler em voo e espera ate shutdownWait: quem nao
// devolveu ate la e abandonado (spec §2.7, §9.4).
func (p *Plugin) shutdown() {
	p.stop()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownWait):
	}
}

func (p *Plugin) send(f frame) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if p.writeErr != nil {
		return
	}
	p.writeErr = writeFrame(p.out, f)
}

func (p *Plugin) lastWriteError() error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.writeErr
}

func (p *Plugin) sendError(id uint32, msg string) {
	body, err := encodeValue(nil, map[string]any{"message": msg}, 0)
	if err != nil {
		return
	}
	p.send(frame{Kind: kindError, ID: id, Body: body})
}

// Logf envia um LOG ao host (`[ext <name>] <msg>` no stderr do Noxy). Fora
// de Serve escreve em stderr.
func Logf(level Level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	p := current.Load()
	if p == nil {
		fmt.Fprintln(os.Stderr, msg)
		return
	}
	body, err := encodeValue(nil, map[string]any{"level": int64(level), "message": msg}, 0)
	if err != nil {
		return
	}
	p.send(frame{Kind: kindLog, Body: body})
}

// Main serve stdin/stdout e sai com o status do protocolo (spec §9.4):
// protege o canal (os.Stdout passa a apontar para stderr), recusa rodar num
// terminal, ignora SIGINT (Ctrl-C e do host; o filho sai no EOF).
func (p *Plugin) Main() {
	stdout := os.Stdout
	os.Stdout = os.Stderr
	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintln(os.Stderr, "this program is a Noxy extension; install it with 'noxy --get'")
		os.Exit(2)
	}
	signal.Ignore(os.Interrupt)
	err := p.Serve(os.Stdin, stdout)
	var exit *ExitError
	switch {
	case errors.As(err, &exit):
		fmt.Fprintln(os.Stderr, exit.Msg)
		os.Exit(exit.Code)
	case err != nil:
		fmt.Fprintln(os.Stderr, "noxyplugin:", err)
		os.Exit(2)
	}
	os.Exit(0)
}
