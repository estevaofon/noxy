package value

import (
	"sync"
	"testing"
)

func TestObjMapOperationsUseSnapshots(t *testing.T) {
	mapping := NewMap().Obj.(*ObjMap)
	mapping.Set("answer", NewInt(42))
	if got, ok := mapping.Get("answer"); !ok || got.AsInt != 42 {
		t.Fatalf("answer=(%v,%t)", got, ok)
	}

	snapshot := mapping.Snapshot()
	snapshot["answer"] = NewInt(0)
	got, _ := mapping.Get("answer")
	if got.AsInt != 42 {
		t.Fatal("snapshot mutated live map")
	}

	if !mapping.Delete("answer") || mapping.Len() != 0 {
		t.Fatal("delete did not remove key")
	}
}

func TestObjMapReplaceKeepsDataAliasCurrent(t *testing.T) {
	mapping := NewMap().Obj.(*ObjMap)
	data := mapping.Data
	mapping.Set("old", NewInt(1))
	mapping.Replace(map[interface{}]Value{"new": NewInt(2)})

	if mapping.Data == nil || len(data) != 1 {
		t.Fatal("replace did not preserve the legacy data map")
	}
	if _, found := data["old"]; found {
		t.Fatal("replace did not clear legacy data")
	}
	if got, found := data["new"]; !found || got.AsInt != 2 {
		t.Fatalf("legacy data new=(%v,%t)", got, found)
	}
}

func TestObjMapReplaceAcceptsItsDataAlias(t *testing.T) {
	mapping := NewMap().Obj.(*ObjMap)
	mapping.Set("answer", NewInt(42))

	mapping.Replace(mapping.Data)

	if got, found := mapping.Get("answer"); !found || got.AsInt != 42 {
		t.Fatalf("answer=(%v,%t)", got, found)
	}
}

func TestObjMapConcurrentSetAndSnapshot(t *testing.T) {
	mapping := NewMap().Obj.(*ObjMap)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(2)
		go func(id int) {
			defer workers.Done()
			<-start
			for n := 0; n < 100; n++ {
				mapping.Set(int64(id*100+n), NewInt(int64(n)))
			}
		}(i)
		go func() {
			defer workers.Done()
			<-start
			for n := 0; n < 100; n++ {
				_ = mapping.Snapshot()
			}
		}()
	}
	close(start)
	workers.Wait()
}
