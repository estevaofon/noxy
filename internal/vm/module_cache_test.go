package vm

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

func TestModuleCacheInitializesKeyOnce(t *testing.T) {
	cache := newModuleCache()
	key := moduleKey{Root: "root", Name: "module"}
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	load := func() (value.Value, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return value.NewInt(42), nil
	}
	results := make(chan value.Value, 2)
	for i := 0; i < 2; i++ {
		go func() {
			got, err := cache.Do(key, nil, load)
			if err != nil {
				t.Error(err)
				return
			}
			results <- got
		}()
	}
	<-entered
	close(release)
	<-results
	<-results
	if calls.Load() != 1 {
		t.Fatalf("loads=%d", calls.Load())
	}
}

func TestModuleCacheRetriesFailedLoad(t *testing.T) {
	cache := newModuleCache()
	key := moduleKey{Root: "root", Name: "module"}
	var calls atomic.Int32

	if _, err := cache.Do(key, nil, func() (value.Value, error) {
		calls.Add(1)
		return value.NewNull(), errors.New("first load failed")
	}); err == nil {
		t.Fatal("first load succeeded")
	}
	got, err := cache.Do(key, nil, func() (value.Value, error) {
		calls.Add(1)
		return value.NewInt(42), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != value.VAL_INT || got.Int() != 42 {
		t.Fatalf("result=%v, want 42", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("loads=%d, want 2", calls.Load())
	}
}

func TestModuleCacheDetectsCrossFlightCycle(t *testing.T) {
	cache := newModuleCache()
	a := moduleKey{Root: "root", Name: "A"}
	b := moduleKey{Root: "root", Name: "B"}
	aEntered := make(chan struct{})
	bEntered := make(chan struct{})
	releaseB := make(chan struct{})
	aResult := make(chan error, 1)

	go func() {
		_, err := cache.Do(a, nil, func() (value.Value, error) {
			close(aEntered)
			_, err := cache.Do(b, &a, func() (value.Value, error) {
				close(bEntered)
				<-releaseB
				return value.NewNull(), nil
			})
			return value.NewNull(), err
		})
		aResult <- err
	}()

	<-aEntered
	<-bEntered
	cycleResult := make(chan error, 1)
	go func() {
		_, err := cache.Do(a, &b, func() (value.Value, error) {
			t.Error("cycle load callback unexpectedly ran")
			return value.NewNull(), nil
		})
		cycleResult <- err
	}()

	select {
	case err := <-cycleResult:
		if err == nil || !strings.Contains(err.Error(), "A -> B -> A") {
			t.Fatalf("cycle error=%v, want path A -> B -> A", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cycle detection waited for an in-flight module")
	}

	close(releaseB)
	if err := <-aResult; err != nil {
		t.Fatal(err)
	}
}
