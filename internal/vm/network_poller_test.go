package vm

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

type fakePlatformWake struct {
	mu         sync.Mutex
	signals    int
	closes     int
	closed     bool
	afterClose bool
	signalErr  error
}

func (*fakePlatformWake) descriptor() uintptr { return 99 }

func (wake *fakePlatformWake) signal() error {
	wake.mu.Lock()
	defer wake.mu.Unlock()
	if wake.closed {
		wake.afterClose = true
	}
	wake.signals++
	return wake.signalErr
}

func (wake *fakePlatformWake) close() error {
	wake.mu.Lock()
	defer wake.mu.Unlock()
	wake.closed = true
	wake.closes++
	return nil
}

func TestNetworkWakeSerializesSignalAndClose(t *testing.T) {
	raw := &fakePlatformWake{}
	wake := newNetworkWake(raw)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_ = wake.Signal()
		}()
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		_ = wake.Close()
	}()
	close(start)
	workers.Wait()
	_ = wake.Signal()
	raw.mu.Lock()
	defer raw.mu.Unlock()
	if raw.afterClose {
		t.Fatal("platform signal ran after platform close")
	}
	if raw.signals > 1 || raw.closes != 1 {
		t.Fatalf("signals=%d closes=%d", raw.signals, raw.closes)
	}
}

func TestNetworkWakeSignalFailureRemainsClosable(t *testing.T) {
	signalFailure := errors.New("wake signal failed")
	raw := &fakePlatformWake{signalErr: signalFailure}
	wake := newNetworkWake(raw)
	if err := wake.Signal(); !errors.Is(err, signalFailure) {
		t.Fatalf("signal error=%v, want %v", err, signalFailure)
	}
	if err := wake.Close(); err != nil {
		t.Fatalf("close error=%v", err)
	}
	if err := wake.Close(); err != nil {
		t.Fatalf("second close error=%v", err)
	}
	if err := wake.Signal(); err != nil {
		t.Fatalf("signal after close error=%v", err)
	}
	raw.mu.Lock()
	defer raw.mu.Unlock()
	if raw.afterClose {
		t.Fatal("platform signal ran after platform close")
	}
	if raw.signals != 1 || raw.closes != 1 {
		t.Fatalf("signals=%d closes=%d, want 1 each", raw.signals, raw.closes)
	}
}

func TestValidateNetworkPollArguments(t *testing.T) {
	empty := value.NewArray(nil)
	maximum := int64(math.MaxInt64) / int64(time.Millisecond)
	tests := []struct {
		name      string
		args      []value.Value
		want      time.Duration
		wantError string
	}{
		{"zero", []value.Value{empty, empty, empty, value.NewInt(0)}, 0, ""},
		{"positive", []value.Value{empty, empty, empty, value.NewInt(25)}, 25 * time.Millisecond, ""},
		{"negative", []value.Value{empty, empty, empty, value.NewInt(-1)}, 0, "network poll timeout must be non-negative"},
		{"overflow", []value.Value{empty, empty, empty, value.NewInt(maximum + 1)}, 0, "network poll timeout is too large"},
		{"wrong arity", []value.Value{empty}, 0, "net_select expects exactly 4 arguments"},
		{"wrong set", []value.Value{value.NewNull(), empty, empty, value.NewInt(0)}, 0, "net_select read, write, and error arguments must be arrays"},
		{"wrong timeout", []value.Value{empty, empty, empty, value.NewString("0")}, 0, "network poll timeout must be an int"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got, err := validateNetworkPollArguments(test.args)
			if test.wantError != "" {
				if err == nil || err.Error() != test.wantError {
					t.Fatalf("error=%v want=%q", err, test.wantError)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("timeout=%v error=%v want=%v", got, err, test.want)
			}
		})
	}
}

func TestSelectResultPadsTruncatesAndCountsCopiedValues(t *testing.T) {
	values := make([]value.Value, 65)
	for i := range values {
		values[i] = socketValue(i+1, "test", 0, true)
	}
	result := selectResult(values, values[:2], values[:1])
	mapping := requireBuiltinMap(t, result)
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "read_count"), value.NewInt(64))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "write_count"), value.NewInt(2))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "error_count"), value.NewInt(1))
	read := requireTestMapValue(t, mapping, "read").Obj.(*value.ObjArray)
	if len(read.Elements) != 64 {
		t.Fatalf("read length=%d", len(read.Elements))
	}
}
