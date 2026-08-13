package vm

import (
	"sync"
	"testing"
)

func TestHandleRegistryUsesMonotonicNonReusableHandles(t *testing.T) {
	registry := newHandleRegistry[string]()
	first := registry.add("first")
	if _, ok := registry.remove(first); !ok {
		t.Fatal("remove failed")
	}
	second := registry.add("second")
	if second <= first {
		t.Fatalf("handles %d then %d", first, second)
	}
	if _, ok := registry.get(first); ok {
		t.Fatal("removed handle resolved")
	}
}

func TestHandleRegistryConcurrentAddsAssignDistinctHandles(t *testing.T) {
	registry := newHandleRegistry[int]()
	const workers = 64

	handles := make(chan int, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for index := 0; index < workers; index++ {
		go func(item int) {
			defer group.Done()
			handles <- registry.add(item)
		}(index)
	}
	group.Wait()
	close(handles)

	seen := make(map[int]bool, workers)
	for handle := range handles {
		if seen[handle] {
			t.Fatalf("duplicate handle %d", handle)
		}
		seen[handle] = true
	}
	if len(seen) != workers {
		t.Fatalf("registered %d handles, want %d", len(seen), workers)
	}
}
