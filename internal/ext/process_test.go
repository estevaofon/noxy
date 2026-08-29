// internal/ext/process_test.go
package ext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

// fakeConn e o par de pipes que o host enxerga como o processo do plugin.
type fakeConn struct {
	stdinR  *io.PipeReader // o plugin le daqui
	stdinW  *io.PipeWriter // o host escreve aqui (Stdin)
	stdoutR *io.PipeReader // o host le daqui (Stdout)
	stdoutW *io.PipeWriter // o plugin escreve aqui
	exited  chan struct{}
	once    sync.Once
	waitErr error
	killed  atomic.Bool
}

func newFakeConn() *fakeConn {
	c := &fakeConn{exited: make(chan struct{})}
	c.stdinR, c.stdinW = io.Pipe()
	c.stdoutR, c.stdoutW = io.Pipe()
	return c
}

func (c *fakeConn) Stdin() io.WriteCloser { return c.stdinW }
func (c *fakeConn) Stdout() io.Reader     { return c.stdoutR }
func (c *fakeConn) Wait() error           { <-c.exited; return c.waitErr }

// Kill num processo que ja saiu e um no-op (como os.Process.Kill): so
// conta como "morto pelo host" quando o plugin ainda estava vivo.
func (c *fakeConn) Kill() error {
	select {
	case <-c.exited:
		return nil
	default:
	}
	c.killed.Store(true)
	c.exit(errors.New("killed"))
	return nil
}

// syncBuffer e um bytes.Buffer seguro para o leitor do host escrever
// enquanto o teste le (-race).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// exit encerra o "processo": fecha o lado do plugin dos dois pipes (o host
// ve EOF em Stdout) e libera Wait com err.
func (c *fakeConn) exit(err error) {
	c.once.Do(func() {
		c.waitErr = err
		_ = c.stdoutW.Close()
		_ = c.stdinR.Close()
		close(c.exited)
	})
}

// fakePlugin fala noxy-plugin/1 de dentro do teste.
type fakePlugin struct {
	conn        *fakeConn
	protocol    string                       // "" → ProtocolVersion
	refuse      string                       // != "" → ERROR id 0 no handshake
	silent      bool                         // nao responde ao HELLO
	handle      func(p *fakePlugin, f Frame) // por CALL; nil → echo do 1o argumento
	onCancel    func(p *fakePlugin, id uint32)
	ignoreEOF   bool   // nao sai quando o host fecha stdin
	preHelloLog string // != "" → LOG antes de responder o HELLO
	hello       chan Frame
	writeMu     sync.Mutex
}

func (p *fakePlugin) send(t testing.TB, f Frame) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if err := WriteFrame(p.conn.stdoutW, f); err != nil && t != nil {
		t.Logf("fake plugin write: %v", err)
	}
}

func (p *fakePlugin) result(id uint32, v value.Value) {
	body, _ := EncodeValue(v, DefaultLimits())
	p.send(nil, Frame{Kind: FrameResult, ID: id, Body: body})
}

func (p *fakePlugin) fail(id uint32, msg string) {
	body, _ := encodeStringMap(map[string]value.Value{"message": value.NewString(msg)}, DefaultLimits())
	p.send(nil, Frame{Kind: FrameError, ID: id, Body: body})
}

func (p *fakePlugin) log(level int64, msg string) {
	body, _ := encodeStringMap(map[string]value.Value{"level": value.NewInt(level), "message": value.NewString(msg)}, DefaultLimits())
	p.send(nil, Frame{Kind: FrameLog, Body: body})
}

func (p *fakePlugin) serve() {
	for {
		f, err := ReadFrame(p.conn.stdinR, DefaultLimits().MaxBytes)
		if err != nil {
			if !p.ignoreEOF {
				p.conn.exit(nil) // EOF do host = shutdown, status 0 (spec §2.7)
			}
			return
		}
		switch f.Kind {
		case FrameHello:
			p.hello <- f
			if p.silent {
				continue
			}
			if p.preHelloLog != "" {
				p.log(0, p.preHelloLog)
			}
			if p.refuse != "" {
				p.fail(0, p.refuse)
				continue
			}
			proto := p.protocol
			if proto == "" {
				proto = ProtocolVersion
			}
			body, _ := encodeStringMap(map[string]value.Value{"protocol": value.NewString(proto), "sdk": value.NewString("fake/0")}, DefaultLimits())
			p.send(nil, Frame{Kind: FrameHello, Body: body})
		case FrameCall:
			if p.handle != nil {
				go p.handle(p, f)
				continue
			}
			args, _ := DecodeArgs(f.Body, DefaultLimits())
			v := value.NewNull()
			if len(args) != 0 {
				v = args[0]
			}
			p.result(f.ID, v)
		case FrameCancel:
			if p.onCancel != nil {
				p.onCancel(p, f.ID)
			}
		}
	}
}

// fakeHarness cria um fakePlugin novo por spawn (necessario para restart).
type fakeHarness struct {
	mu      sync.Mutex
	spawns  int
	current *fakePlugin
	setup   func(p *fakePlugin)
	logs    syncBuffer
}

func (h *fakeHarness) spawn(ctx context.Context) (procConn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.spawns++
	p := &fakePlugin{conn: newFakeConn(), hello: make(chan Frame, 4)}
	if h.setup != nil {
		h.setup(p)
	}
	h.current = p
	go p.serve()
	return p.conn, nil
}

func (h *fakeHarness) plugin() *fakePlugin {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current
}

const processTestManifest = `
name = "guest"
abi = 1
kind = "process"
concurrency = "%s"
call_timeout_ms = 200
handshake_timeout_ms = 300
%s

[binaries]
linux-amd64 = "guest-linux-amd64"

[[export]]
name = "guest_echo"
params = ["any"]
returns = "any"

[[export]]
name = "guest_fail"
params = []
returns = "any"

[[export]]
name = "guest_slow"
params = []
returns = "any"
timeout_ms = 50

[[export]]
name = "guest_int"
params = []
returns = "int"
`

func newFakeProcess(t *testing.T, concurrency, extraKeys string, setup func(p *fakePlugin)) (*Process, *fakeHarness) {
	t.Helper()
	m, err := ParseManifest([]byte(fmt.Sprintf(processTestManifest, concurrency, extraKeys)))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	h := &fakeHarness{setup: setup}
	p := newProcess(m, ProcessConfig{NoxyVersion: "v0.23.0", Log: &h.logs}, h.spawn)
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p, h
}

func callEcho(t *testing.T, p *Process, v value.Value) (value.Value, error) {
	t.Helper()
	return p.Call(context.Background(), 0, []value.Value{v})
}

func TestProcessLazyStartAndEcho(t *testing.T) {
	p, h := newFakeProcess(t, "single", "", nil)
	if h.spawns != 0 {
		t.Fatal("NewProcess must not start the process (spec §4.1)")
	}
	got, err := callEcho(t, p, value.NewInt(42))
	if err != nil || got.Int() != 42 {
		t.Fatalf("echo: %#v %v", got, err)
	}
	hello := <-h.plugin().hello
	m, err := decodeBodyMap(hello.Body, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if proto, _ := mapString(m, "protocol"); proto != ProtocolVersion {
		t.Fatalf("host HELLO protocol: %q", proto)
	}
	exports, _ := m.Get("exports")
	names := exports.Obj.(*value.ObjArray).Elements
	if len(names) != 4 || names[2].Obj.(string) != "guest_slow" {
		t.Fatalf("exports in manifest order: %#v", names)
	}
	if _, err := callEcho(t, p, value.NewString("again")); err != nil {
		t.Fatal(err)
	}
	if h.spawns != 1 {
		t.Fatalf("one process per extension, got %d spawns", h.spawns)
	}
}

func TestProcessHandshakeRefusedIsTrapAndPoisons(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) { fp.refuse = `no handler for export "guest_fail"` })
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), `extension 'guest' trapped: handshake: plugin refused: no handler for export "guest_fail"`) {
		t.Fatalf("got %v", err)
	}
	_, err = callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' is poisoned by an earlier trap") {
		t.Fatalf("second call must see the poison, got %v", err)
	}
}

func TestProcessHandshakeVersionMismatch(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) { fp.protocol = "noxy-plugin/2" })
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), `trapped: handshake: plugin speaks "noxy-plugin/2", host speaks "noxy-plugin/1"`) {
		t.Fatalf("got %v", err)
	}
}

func TestProcessHandshakeTimeoutKills(t *testing.T) {
	p, h := newFakeProcess(t, "single", "", func(fp *fakePlugin) { fp.silent = true })
	start := time.Now()
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "trapped: handshake: no reply within 300ms") {
		t.Fatalf("got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("handshake timeout must be honoured, took %v", elapsed)
	}
	if !h.plugin().conn.killed.Load() {
		t.Fatal("a plugin that never replies must be killed")
	}
}

func TestProcessErrorFrameIsFailedNotPoison(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			if f.Fn == 1 {
				fp.fail(f.ID, "boom")
				return
			}
			fp.result(f.ID, value.NewInt(1))
		}
	})
	_, err := p.Call(context.Background(), 1, nil)
	if err == nil || err.Error() != "extension 'guest' failed: boom" {
		t.Fatalf("got %v", err)
	}
	if _, err := callEcho(t, p, value.NewNull()); err != nil {
		t.Fatalf("declared failure must not poison: %v", err)
	}
}

func TestProcessDeclaredReturnIsChecked(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { fp.result(f.ID, value.NewString("not an int")) }
	})
	_, err := p.Call(context.Background(), 3, nil)
	if err == nil || !strings.Contains(err.Error(), `result does not match declared return type "int"`) {
		t.Fatalf("got %v", err)
	}
	if _, err := p.Call(context.Background(), 3, nil); err == nil || strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("a lying result is an error, not a trap: %v", err)
	}
}

func TestProcessLogFramesGoToLog(t *testing.T) {
	p, h := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.preHelloLog = "hello" // LOG antes do HELLO do plugin e permitido (spec §2.4)
		fp.handle = func(fp *fakePlugin, f Frame) {
			fp.log(1, "working")
			fp.result(f.ID, value.NewNull())
		}
	})
	if _, err := callEcho(t, p, value.NewNull()); err != nil {
		t.Fatal(err)
	}
	h.plugin().log(0, "late")
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(h.logs.String(), "[ext guest] late") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	got := h.logs.String()
	for _, want := range []string{"[ext guest] hello\n", "[ext guest] working\n", "[ext guest] late\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output missing %q: %q", want, got)
		}
	}
}

func TestProcessOutOfOrderRepliesConcurrent(t *testing.T) {
	release := make(chan struct{})
	p, _ := newFakeProcess(t, "concurrent", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			args, _ := DecodeArgs(f.Body, DefaultLimits())
			if args[0].Int() == 1 {
				<-release // a primeira chamada so responde depois da segunda
			}
			fp.result(f.ID, args[0])
		}
	})
	var wg sync.WaitGroup
	results := make([]int64, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := callEcho(t, p, value.NewInt(int64(i+1)))
			if err != nil {
				t.Errorf("call %d: %v", i+1, err)
				return
			}
			results[i] = got.Int()
			if i == 1 {
				close(release)
			}
		}(i)
	}
	wg.Wait()
	if results[0] != 1 || results[1] != 2 {
		t.Fatalf("replies must route by id: %v", results)
	}
}

func TestProcessSingleSerializesCalls(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			n := inFlight.Add(1)
			for {
				old := maxInFlight.Load()
				if n <= old || maxInFlight.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			inFlight.Add(-1)
			fp.result(f.ID, value.NewNull())
		}
	})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := callEcho(t, p, value.NewNull()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if maxInFlight.Load() != 1 {
		t.Fatalf("single mode allows one call in flight, saw %d", maxInFlight.Load())
	}
}

func shortGraces(t *testing.T) {
	t.Helper()
	prevCancel, prevShutdown := cancelGrace, shutdownGrace
	cancelGrace, shutdownGrace = 100*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { cancelGrace, shutdownGrace = prevCancel, prevShutdown })
}

func TestProcessTimeoutCancelHonoured(t *testing.T) {
	shortGraces(t)
	cancelled := make(chan uint32, 1)
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		stop := make(map[uint32]chan struct{})
		var mu sync.Mutex
		fp.handle = func(fp *fakePlugin, f Frame) {
			if f.Fn != 2 {
				fp.result(f.ID, value.NewNull())
				return
			}
			ch := make(chan struct{})
			mu.Lock()
			stop[f.ID] = ch
			mu.Unlock()
			<-ch // espera o CANCEL
			fp.fail(f.ID, "cancelled")
		}
		fp.onCancel = func(fp *fakePlugin, id uint32) {
			mu.Lock()
			ch := stop[id]
			mu.Unlock()
			close(ch)
			cancelled <- id
		}
	})
	_, err := p.Call(context.Background(), 2, nil) // guest_slow: timeout_ms = 50
	if err == nil || err.Error() != "extension 'guest' timed out: guest_slow exceeded 50 ms" {
		t.Fatalf("got %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("host must send CANCEL on expiry")
	}
	if _, err := callEcho(t, p, value.NewInt(1)); err != nil {
		t.Fatalf("a cancelled call does not poison: %v", err)
	}
}

func TestProcessTimeoutCancelIgnoredKillsAndPoisons(t *testing.T) {
	shortGraces(t)
	p, h := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { /* nunca responde */ }
	})
	_, err := p.Call(context.Background(), 2, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' trapped: guest_slow exceeded 50 ms and did not cancel; process killed") {
		t.Fatalf("got %v", err)
	}
	if !h.plugin().conn.killed.Load() {
		t.Fatal("plugin must be killed after the cancel grace")
	}
	_, err = callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "is poisoned by an earlier trap") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessTimeoutConcurrentDoesNotBlockOthers(t *testing.T) {
	shortGraces(t)
	p, _ := newFakeProcess(t, "concurrent", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			if f.Fn == 2 {
				return // guest_slow nunca responde; o cancel tambem nao
			}
			fp.result(f.ID, value.NewInt(7))
		}
	})
	done := make(chan error, 1)
	go func() {
		_, err := p.Call(context.Background(), 2, nil)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	got, err := callEcho(t, p, value.NewNull())
	if err != nil || got.Int() != 7 {
		t.Fatalf("other call must complete while guest_slow hangs: %#v %v", got, err)
	}
	err = <-done
	if err == nil || !strings.Contains(err.Error(), "timed out: guest_slow exceeded 50 ms") {
		t.Fatalf("caller gets timed out immediately in concurrent mode, got %v", err)
	}
	// depois da carencia sem resposta ao CANCEL, o processo cai e a proxima
	// chamada ve o poison
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := callEcho(t, p, value.NewNull()); err != nil && strings.Contains(err.Error(), "poisoned") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("process must be killed and poisoned after the cancel grace")
}

func TestProcessExitFailsInFlightAndPoisons(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { fp.conn.exit(errors.New("status 3")) }
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || err.Error() != "extension 'guest' trapped: process exited (status 3)" {
		t.Fatalf("got %v", err)
	}
	_, err = callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' is poisoned by an earlier trap") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessRestartOnlyWithStateless(t *testing.T) {
	p, h := newFakeProcess(t, "stateless", "restart = true", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			args, _ := DecodeArgs(f.Body, DefaultLimits())
			if args[0].Int() == 0 {
				fp.conn.exit(nil)
				return
			}
			fp.result(f.ID, args[0])
		}
	})
	if _, err := callEcho(t, p, value.NewInt(0)); err == nil || !strings.Contains(err.Error(), "process exited (status 0)") {
		t.Fatalf("got %v", err)
	}
	got, err := callEcho(t, p, value.NewInt(5))
	if err != nil || got.Int() != 5 {
		t.Fatalf("restart must respawn on the next call: %#v %v", got, err)
	}
	if h.spawns != 2 {
		t.Fatalf("expected a second spawn, got %d", h.spawns)
	}
}

func TestProcessStartFailureIsTrap(t *testing.T) {
	m, _ := ParseManifest([]byte(fmt.Sprintf(processTestManifest, "single", "")))
	p := newProcess(m, ProcessConfig{}, func(context.Context) (procConn, error) {
		return nil, errors.New("exec format error")
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || err.Error() != "extension 'guest' trapped: start: exec format error" {
		t.Fatalf("got %v", err)
	}
	if _, err := callEcho(t, p, value.NewNull()); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("start failure poisons without restart: %v", err)
	}
}

func TestProcessUnknownReplyIdIsViolation(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { fp.result(99, value.NewNull()) }
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "trapped: protocol violation: reply for unknown call id 99") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessMalformedFrameIsViolation(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			fp.writeMu.Lock()
			_, _ = fp.conn.stdoutW.Write([]byte{0x02, 0, 0, 0, 0xff, 0xff})
			fp.writeMu.Unlock()
		}
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "trapped: protocol violation: frame length 2 below header size") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessMalformedResultIsViolation(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { fp.send(nil, Frame{Kind: FrameResult, ID: f.ID, Body: []byte{0x77}}) }
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "trapped: protocol violation: RESULT body") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessCloseSendsEOFAndRefusesLaterCalls(t *testing.T) {
	shortGraces(t)
	p, h := newFakeProcess(t, "single", "", nil)
	if _, err := callEcho(t, p, value.NewNull()); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn := h.plugin().conn
	select {
	case <-conn.exited:
	default:
		t.Fatal("plugin must have exited on EOF before Close returns")
	}
	if conn.killed.Load() {
		t.Fatal("a plugin that honours EOF must not be killed")
	}
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' was closed at exit") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessCloseKillsAfterGrace(t *testing.T) {
	shortGraces(t)
	p, h := newFakeProcess(t, "single", "", func(fp *fakePlugin) { fp.ignoreEOF = true })
	if _, err := callEcho(t, p, value.NewNull()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = p.Close(context.Background())
	if !h.plugin().conn.killed.Load() {
		t.Fatal("a plugin that ignores EOF must be killed after the shutdown grace")
	}
	if time.Since(start) > time.Second {
		t.Fatal("Close must not wait beyond the grace")
	}
}
