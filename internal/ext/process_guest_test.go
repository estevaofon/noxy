// internal/ext/process_guest_test.go
package ext

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/estevaofon/noxy/internal/ext/exttest"
	"github.com/estevaofon/noxy/internal/value"
)

const processGuestManifest = `
name = "guest"
abi = 1
kind = "process"
concurrency = "%s"
call_timeout_ms = 2000
%s

[binaries]
%s = "%s"

[[export]]
name = "guest_echo"
params = ["any"]
returns = "any"

[[export]]
name = "guest_add"
params = ["int", "int"]
returns = "int"

[[export]]
name = "guest_fail"
params = ["string"]
returns = "void"

[[export]]
name = "guest_sleep_ms"
params = ["int"]
returns = "void"
timeout_ms = 150

[[export]]
name = "guest_block"
params = []
returns = "void"
timeout_ms = 100

[[export]]
name = "guest_exit"
params = ["int"]
returns = "void"

[[export]]
name = "guest_log"
params = ["string"]
returns = "void"

[[export]]
name = "guest_panic"
params = []
returns = "void"

[[export]]
name = "guest_bytes"
params = ["bytes"]
returns = "bytes"

[[export]]
name = "guest_pid"
params = []
returns = "int"

[[export]]
name = "guest_print"
params = ["string"]
returns = "void"

[[export]]
name = "guest_badtype"
params = []
returns = "int"

[[export]]
name = "guest_noop"
params = []
returns = "void"
`

const (
	fnEcho = iota
	fnAdd
	fnFail
	fnSleep
	fnBlock
	fnExit
	fnLog
	fnPanic
	fnBytes
	fnPid
	fnPrint
	fnBadType
	fnNoop
)

// syncBuffer vem de process_test.go (Task 5).

func guestManifest(t testing.TB, guestPath, concurrency, extra string) *Manifest {
	t.Helper()
	src := fmt.Sprintf(processGuestManifest, concurrency, extra, runtime.GOOS+"-"+runtime.GOARCH, filepath.Base(guestPath))
	m, err := ParseManifest([]byte(src))
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	return m
}

func guestProcess(t *testing.T, concurrency, extra string) (*Process, *syncBuffer) {
	t.Helper()
	path := exttest.BuildProcessGuest(t)
	logs := &syncBuffer{}
	p := NewProcess(guestManifest(t, path, concurrency, extra), ProcessConfig{Path: path, NoxyVersion: "v0.23.0", Log: logs})
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p, logs
}

func call(t *testing.T, p *Process, fn int, args ...value.Value) (value.Value, error) {
	t.Helper()
	return p.Call(context.Background(), fn, args)
}

func TestGuestEchoAddBytes(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	if got, err := call(t, p, fnAdd, value.NewInt(2), value.NewInt(3)); err != nil || got.Int() != 5 {
		t.Fatalf("add: %#v %v", got, err)
	}
	m := value.NewMap()
	m.Obj.(*value.ObjMap).Set("k", value.NewString("v"))
	got, err := call(t, p, fnEcho, m)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got.Obj.(*value.ObjMap).Get("k"); !ok || v.Obj.(string) != "v" {
		t.Fatalf("echo map: %#v", got)
	}
	payload := strings.Repeat("x", 1<<20)
	if got, err := call(t, p, fnBytes, value.NewBytes(payload)); err != nil || got.Obj.(string) != payload {
		t.Fatalf("1 MB bytes round trip: %v", err)
	}
}

func TestGuestFailedPanicBadType(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	if _, err := call(t, p, fnFail, value.NewString("boom")); err == nil || err.Error() != "extension 'guest' failed: boom" {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnPanic); err == nil || err.Error() != "extension 'guest' failed: panic: kaboom" {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnBadType); err == nil || !strings.Contains(err.Error(), `declared return type "int"`) {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnAdd, value.NewString("x"), value.NewInt(1)); err == nil || err.Error() != "extension 'guest' failed: argument 1: expected int, got string" {
		t.Fatalf("got %v", err)
	}
	if got, err := call(t, p, fnAdd, value.NewInt(1), value.NewInt(1)); err != nil || got.Int() != 2 {
		t.Fatalf("none of the above poisons: %v", err)
	}
}

func TestGuestCancelHonoured(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	start := time.Now()
	_, err := call(t, p, fnSleep, value.NewInt(5000))
	if err == nil || err.Error() != "extension 'guest' timed out: guest_sleep_ms exceeded 150 ms" {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("a cancelled call returns promptly")
	}
	if got, err := call(t, p, fnAdd, value.NewInt(1), value.NewInt(1)); err != nil || got.Int() != 2 {
		t.Fatalf("process survives a cancelled call: %v", err)
	}
}

func TestGuestBlockIgnoresCancelIsKilled(t *testing.T) {
	shortGraces(t)
	p, _ := guestProcess(t, "single", "")
	_, err := call(t, p, fnBlock)
	if err == nil || !strings.Contains(err.Error(), "trapped: guest_block exceeded 100 ms and did not cancel; process killed") {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnNoop); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("got %v", err)
	}
}

func TestGuestExitTrapsAndRestart(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	if _, err := call(t, p, fnExit, value.NewInt(3)); err == nil || err.Error() != "extension 'guest' trapped: process exited (status 3)" {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnNoop); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("got %v", err)
	}

	r, _ := guestProcess(t, "stateless", "restart = true")
	first, err := call(t, r, fnPid)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = call(t, r, fnExit, value.NewInt(0))
	second, err := call(t, r, fnPid)
	if err != nil || second.Int() == first.Int() {
		t.Fatalf("restart must spawn a new process: %v %v %v", first, second, err)
	}
}

func TestGuestLogAndStdoutProtection(t *testing.T) {
	p, logs := guestProcess(t, "single", "")
	if _, err := call(t, p, fnLog, value.NewString("hello from guest")); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, p, fnPrint, value.NewString("stray print")); err != nil {
		t.Fatal(err)
	}
	if got, err := call(t, p, fnAdd, value.NewInt(4), value.NewInt(4)); err != nil || got.Int() != 8 {
		t.Fatalf("a stray print must not corrupt the stream: %v", err)
	}
	if !strings.Contains(logs.String(), "[ext guest] hello from guest\n") {
		t.Fatalf("log: %q", logs.String())
	}
}

func TestGuestConcurrentInterleaves(t *testing.T) {
	p, _ := guestProcess(t, "concurrent", "")
	if _, err := call(t, p, fnNoop); err != nil {
		t.Fatal(err)
	}
	sleepDone := make(chan error, 1)
	go func() {
		_, err := call(t, p, fnSleep, value.NewInt(100))
		sleepDone <- err
	}()
	time.Sleep(10 * time.Millisecond)
	start := time.Now()
	if _, err := call(t, p, fnAdd, value.NewInt(1), value.NewInt(2)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 80*time.Millisecond {
		t.Fatal("add must not wait for the sleeping call in concurrent mode")
	}
	if err := <-sleepDone; err != nil {
		t.Fatalf("sleep(100) is under its 150 ms deadline: %v", err)
	}
}

func TestGuestCloseExitsOnEOF(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	pid, err := call(t, p, fnPid)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= shutdownGrace {
		t.Fatal("the SDK exits on EOF before the grace")
	}
	deadline := time.Now().Add(2 * time.Second)
	for exttest.ProcessAlive(int(pid.Int())) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if exttest.ProcessAlive(int(pid.Int())) {
		t.Fatal("guest still alive after Close")
	}
}
