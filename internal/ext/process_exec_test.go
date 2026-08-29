// internal/ext/process_exec_test.go
package ext

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

// TestMain: com NOXY_EXT_HELPER=plugin o binario de teste vira um plugin
// minimo (spec §2) — assim o spawner real e testado sem toolchain extra.
// A Task 13 acrescenta o modo "host".
func TestMain(m *testing.M) {
	switch os.Getenv("NOXY_EXT_HELPER") {
	case "plugin":
		os.Exit(helperPlugin())
	case "host":
		os.Exit(helperHost())
	}
	os.Exit(m.Run())
}

func helperHost() int { return 0 } // substituido na Task 13

// helperPlugin: HELLO → HELLO; CALL fn 0 → echo; fn 1 → ERROR; EOF → sai 0.
func helperPlugin() int {
	in := bufio.NewReader(os.Stdin)
	out := os.Stdout
	limits := DefaultLimits()
	f, err := ReadFrame(in, limits.MaxBytes)
	if err != nil || f.Kind != FrameHello {
		return 2
	}
	body, _ := encodeStringMap(map[string]value.Value{"protocol": value.NewString(ProtocolVersion), "sdk": value.NewString("helper/0")}, limits)
	if err := WriteFrame(out, Frame{Kind: FrameHello, Body: body}); err != nil {
		return 2
	}
	for {
		f, err := ReadFrame(in, limits.MaxBytes)
		if err != nil {
			return 0
		}
		if f.Kind != FrameCall {
			continue
		}
		switch f.Fn {
		case 1:
			msg, _ := encodeStringMap(map[string]value.Value{"message": value.NewString("helper says no")}, limits)
			_ = WriteFrame(out, Frame{Kind: FrameError, ID: f.ID, Body: msg})
		default:
			args, _ := DecodeArgs(f.Body, limits)
			v := value.NewNull()
			if len(args) != 0 {
				v = args[0]
			}
			res, _ := EncodeValue(v, limits)
			_ = WriteFrame(out, Frame{Kind: FrameResult, ID: f.ID, Body: res})
		}
	}
}

// helperProcess sobe o proprio binario de teste como plugin via execSpawner.
func helperProcess(t *testing.T, concurrency string) *Process {
	t.Helper()
	m, err := ParseManifest([]byte(fmt.Sprintf(processTestManifest, concurrency, "")))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOXY_EXT_HELPER", "plugin")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := NewProcess(m, ProcessConfig{Path: self, NoxyVersion: "v0.23.0"})
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p
}

func TestExecSpawnerEchoAndError(t *testing.T) {
	p := helperProcess(t, "single")
	got, err := p.Call(context.Background(), 0, []value.Value{value.NewString("hi")})
	if err != nil || got.Obj.(string) != "hi" {
		t.Fatalf("echo through a real process: %#v %v", got, err)
	}
	_, err = p.Call(context.Background(), 1, nil)
	if err == nil || err.Error() != "extension 'guest' failed: helper says no" {
		t.Fatalf("got %v", err)
	}
}

func TestExecSpawnerCloseExitsOnEOF(t *testing.T) {
	p := helperProcess(t, "single")
	if _, err := p.Call(context.Background(), 0, []value.Value{value.NewInt(1)}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > shutdownGrace {
		t.Fatal("a plugin that honours EOF exits before the grace")
	}
	_, err := p.Call(context.Background(), 0, nil)
	if err == nil || !strings.Contains(err.Error(), "was closed at exit") {
		t.Fatalf("got %v", err)
	}
}

func TestExecSpawnerMissingBinaryIsStartTrap(t *testing.T) {
	m, _ := ParseManifest([]byte(fmt.Sprintf(processTestManifest, "single", "")))
	p := NewProcess(m, ProcessConfig{Path: "/definitely/not/here/noxy-plugin-guest"})
	_, err := p.Call(context.Background(), 0, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "extension 'guest' trapped: start: ") {
		t.Fatalf("got %v", err)
	}
}
