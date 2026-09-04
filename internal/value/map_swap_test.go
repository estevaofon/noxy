package value

import "testing"

// Issue #133: a escrita de um membro de modulo pelo namespace precisa ler o
// valor velho e gravar o novo sob UM unico Lock — com Get/Set separados dois
// escritores concorrentes observam o mesmo valor velho e ambos o liberam
// (double free de RC que o -race nao ve, porque o store e mutexado).

func TestBindingStoreSwapReturnsOldValueAndAdvancesGeneration(t *testing.T) {
	store := newBindingStore(map[interface{}]Value{"count": NewInt(1)})
	before := store.gen.Load()

	old, existed := store.swap("count", NewInt(2))
	if !existed {
		t.Fatal("swap on an existing key must report existed=true")
	}
	if old.Int() != 1 {
		t.Fatalf("old=%v, want 1", old)
	}
	if got, found := store.get("count"); !found || got.Int() != 2 {
		t.Fatalf("stored=(%v,%t), want (2,true)", got, found)
	}
	if after := store.gen.Load(); after <= before {
		t.Fatalf("gen=%d, want > %d", after, before)
	}
}

func TestBindingStoreSwapOnAbsentKeyStoresAndReportsAbsence(t *testing.T) {
	store := newBindingStore(nil)
	before := store.gen.Load()

	old, existed := store.swap("fresh", NewInt(7))
	if existed {
		t.Fatal("swap on an absent key must report existed=false")
	}
	if old != (Value{}) {
		t.Fatalf("old=%v, want the zero Value", old)
	}
	if got, found := store.get("fresh"); !found || got.Int() != 7 {
		t.Fatalf("stored=(%v,%t), want (7,true)", got, found)
	}
	if after := store.gen.Load(); after <= before {
		t.Fatalf("gen=%d, want > %d", after, before)
	}
}

func TestObjMapSwapDelegatesToTheStore(t *testing.T) {
	mapping := &ObjMap{}
	mapping.Set("name", NewString("a"))

	old, existed := mapping.Swap("name", NewString("b"))
	if !existed {
		t.Fatal("Swap on an existing key must report existed=true")
	}
	if got, ok := old.Obj.(string); !ok || got != "a" {
		t.Fatalf("old=%v, want \"a\"", old)
	}
	if got, found := mapping.Get("name"); !found {
		t.Fatal("key vanished after Swap")
	} else if text, ok := got.Obj.(string); !ok || text != "b" {
		t.Fatalf("stored=%v, want \"b\"", got)
	}
}
