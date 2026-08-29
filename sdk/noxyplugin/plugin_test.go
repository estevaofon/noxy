// sdk/noxyplugin/plugin_test.go
package noxyplugin

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeHost fala noxy-plugin/1 do lado do host, em memoria.
type fakeHost struct {
	toPlugin   *io.PipeWriter
	fromPlugin *bufio.Reader
	served     chan error
}

func startPlugin(t *testing.T, p *Plugin) *fakeHost {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := &fakeHost{toPlugin: inW, fromPlugin: bufio.NewReader(outR), served: make(chan error, 1)}
	go func() { h.served <- p.Serve(inR, outW); _ = outW.Close() }()
	t.Cleanup(func() { _ = inW.Close() })
	return h
}

func (h *fakeHost) write(t *testing.T, f frame) {
	t.Helper()
	if err := writeFrame(h.toPlugin, f); err != nil {
		t.Fatalf("host write: %v", err)
	}
}

func (h *fakeHost) read(t *testing.T) frame {
	t.Helper()
	f, err := readFrame(h.fromPlugin, 64<<20)
	if err != nil {
		t.Fatalf("host read: %v", err)
	}
	return f
}

func (h *fakeHost) hello(t *testing.T, exports ...string) frame {
	t.Helper()
	names := make([]any, len(exports))
	for i, e := range exports {
		names[i] = e
	}
	body, _ := encodeValue(nil, map[string]any{"protocol": protocolVersion, "noxy": "test", "extension": "t", "exports": names}, 0)
	h.write(t, frame{Kind: kindHello, Body: body})
	return h.read(t)
}

func (h *fakeHost) call(t *testing.T, id, fn uint32, args ...any) {
	t.Helper()
	body, err := encodeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	h.write(t, frame{Kind: kindCall, ID: id, Fn: fn, Body: body})
}

func message(t *testing.T, f frame) string {
	t.Helper()
	m, err := decodeStringMap(f.Body)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m["message"].(string)
	return s
}

func newTestPlugin() *Plugin {
	p := New()
	p.Handle("t_add", Func2(func(_ context.Context, a, b int64) (int64, error) { return a + b, nil }))
	p.Handle("t_fail", Func0(func(context.Context) (any, error) { return nil, errors.New("nope") }))
	p.Handle("t_panic", Func0(func(context.Context) (any, error) { panic("kaboom") }))
	p.Handle("t_wait", Func0(func(ctx context.Context) (any, error) { <-ctx.Done(); return nil, ctx.Err() }))
	p.Handle("t_log", Func0(func(context.Context) (any, error) { Logf(LevelWarn, "careful %d", 1); return "ok", nil }))
	return p
}

func TestServeHandshakeAndCall(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	reply := h.hello(t, "t_add", "t_fail")
	if reply.Kind != kindHello {
		t.Fatalf("expected HELLO back, got 0x%02x %s", reply.Kind, message(t, reply))
	}
	m, _ := decodeStringMap(reply.Body)
	if m["protocol"] != protocolVersion || !strings.HasPrefix(m["sdk"].(string), "noxyplugin-go/") {
		t.Fatalf("plugin HELLO: %#v", m)
	}
	h.call(t, 1, 0, int64(2), int64(3))
	res := h.read(t)
	v, _ := decodeValue(res.Body)
	if res.Kind != kindResult || res.ID != 1 || v != int64(5) {
		t.Fatalf("RESULT: %#v %#v", res, v)
	}
}

func TestServeErrorsAndPanics(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	h.hello(t, "t_fail", "t_panic", "t_add")
	h.call(t, 1, 0)
	if f := h.read(t); f.Kind != kindError || message(t, f) != "nope" {
		t.Fatalf("handler error: %#v", f)
	}
	h.call(t, 2, 1)
	if f := h.read(t); f.Kind != kindError || message(t, f) != "panic: kaboom" {
		t.Fatalf("panic: %#v %s", f, message(t, f))
	}
	h.call(t, 3, 2, "x", int64(1))
	if f := h.read(t); f.Kind != kindError || message(t, f) != "argument 1: expected int, got string" {
		t.Fatalf("typed adapter: %s", message(t, f))
	}
	h.call(t, 4, 2, int64(1))
	if f := h.read(t); f.Kind != kindError || message(t, f) != "expected 2 arguments, got 1" {
		t.Fatalf("arity: %s", message(t, f))
	}
}

func TestServeRefusesMissingHandlerAndBadProtocol(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	reply := h.hello(t, "t_add", "t_zzz")
	if reply.Kind != kindError || reply.ID != 0 || message(t, reply) != `no handler for export "t_zzz"` {
		t.Fatalf("got %#v %s", reply, message(t, reply))
	}
	var exit *ExitError
	if err := <-h.served; !errors.As(err, &exit) || exit.Code != 2 {
		t.Fatalf("Serve must return ExitError code 2, got %v", err)
	}

	h2 := startPlugin(t, newTestPlugin())
	body, _ := encodeValue(nil, map[string]any{"protocol": "noxy-plugin/9", "exports": []any{}}, 0)
	h2.write(t, frame{Kind: kindHello, Body: body})
	reply = h2.read(t)
	if reply.Kind != kindError || !strings.Contains(message(t, reply), "unsupported protocol") {
		t.Fatalf("got %#v %s", reply, message(t, reply))
	}
}

func TestServeCancelAndEOF(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	h.hello(t, "t_wait")
	h.call(t, 1, 0)
	h.write(t, frame{Kind: kindCancel, ID: 1})
	if f := h.read(t); f.Kind != kindError || message(t, f) != "context canceled" {
		t.Fatalf("CANCEL must cancel the handler context: %#v %s", f, message(t, f))
	}
	h.call(t, 2, 0)
	_ = h.toPlugin.Close() // EOF: shutdown
	select {
	case err := <-h.served:
		if err != nil {
			t.Fatalf("EOF is a clean exit, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve must return after EOF (handlers cancelled, bounded wait)")
	}
}

func TestServeLogFrames(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	h.hello(t, "t_log")
	h.call(t, 1, 0)
	logf := h.read(t)
	if logf.Kind != kindLog {
		t.Fatalf("LOG must precede the RESULT, got 0x%02x", logf.Kind)
	}
	m, _ := decodeStringMap(logf.Body)
	if m["level"] != int64(LevelWarn) || m["message"] != "careful 1" {
		t.Fatalf("LOG body: %#v", m)
	}
	if f := h.read(t); f.Kind != kindResult {
		t.Fatalf("RESULT after LOG, got 0x%02x", f.Kind)
	}
}

func TestServeUnexpectedFrameExits(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	h.hello(t, "t_add")
	h.write(t, frame{Kind: kindResult, ID: 9})
	var exit *ExitError
	if err := <-h.served; !errors.As(err, &exit) || exit.Code != 2 {
		t.Fatalf("got %v", err)
	}
}

func TestFuncAdaptersConvert(t *testing.T) {
	join := Func1(func(_ context.Context, parts []string) (string, error) { return strings.Join(parts, "+"), nil })
	got, err := join(context.Background(), Args{[]any{"a", "b"}})
	if err != nil || got != "a+b" {
		t.Fatalf("[]any → []string: %v %v", got, err)
	}
	sum := Func1(func(_ context.Context, m map[string]int) (int, error) { return m["x"] + m["y"], nil })
	got, err = sum(context.Background(), Args{map[string]any{"x": int64(1), "y": int64(2)}})
	if err != nil || got != 3 {
		t.Fatalf("map[string]any → map[string]int: %v %v", got, err)
	}
	name := Func1(func(_ context.Context, s Struct) (string, error) { return s.Name, nil })
	got, err = name(context.Background(), Args{Struct{Name: "Point"}})
	if err != nil || got != "Point" {
		t.Fatalf("Struct passthrough: %v %v", got, err)
	}
	small := Func1(func(_ context.Context, n int8) (int8, error) { return n, nil })
	if _, err := small(context.Background(), Args{int64(300)}); err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("overflow must be reported: %v", err)
	}
	optional := Func1(func(_ context.Context, xs []int64) (int, error) { return len(xs), nil })
	got, err = optional(context.Background(), Args{nil})
	if err != nil || got != 0 {
		t.Fatalf("null → nil slice: %v %v", got, err)
	}
}

func TestArgsAccessors(t *testing.T) {
	args := Args{int64(1), "s", []byte{1}, []any{int64(2)}, map[string]any{"k": true}, 2.5, true, Struct{Name: "S"}}
	if v, err := args.Int(0); err != nil || v != 1 {
		t.Fatal(v, err)
	}
	if v, err := args.String(1); err != nil || v != "s" {
		t.Fatal(v, err)
	}
	if v, err := args.Bytes(2); err != nil || len(v) != 1 {
		t.Fatal(v, err)
	}
	if v, err := args.Array(3); err != nil || len(v) != 1 {
		t.Fatal(v, err)
	}
	if v, err := args.Map(4); err != nil || v["k"] != true {
		t.Fatal(v, err)
	}
	if v, err := args.Float(5); err != nil || v != 2.5 {
		t.Fatal(v, err)
	}
	if v, err := args.Bool(6); err != nil || !v {
		t.Fatal(v, err)
	}
	if v, err := args.Struct(7); err != nil || v.Name != "S" {
		t.Fatal(v, err)
	}
	if _, err := args.Int(1); err == nil || err.Error() != "argument 2: expected int, got string" {
		t.Fatalf("type error text: %v", err)
	}
	if _, err := args.Int(99); err == nil || err.Error() != "argument 100: missing" {
		t.Fatalf("missing text: %v", err)
	}
}
