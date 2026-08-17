package value

import "testing"

func TestRetainReleaseCountsComposites(t *testing.T) {
	arr := NewArray([]Value{NewInt(1)})
	if OwnersCount(arr) != 0 {
		t.Fatalf("array novo deve nascer com Owners=0, veio %d", OwnersCount(arr))
	}
	if !Retain(arr) {
		t.Fatal("Retain de array deve retornar true")
	}
	if !Retain(arr) {
		t.Fatal("segundo Retain deve retornar true")
	}
	if OwnersCount(arr) != 2 {
		t.Fatalf("esperado Owners=2, veio %d", OwnersCount(arr))
	}
	Release(arr)
	if OwnersCount(arr) != 1 {
		t.Fatalf("esperado Owners=1 apos Release, veio %d", OwnersCount(arr))
	}
}

func TestRetainIgnoresScalarsAndStrings(t *testing.T) {
	if Retain(NewInt(7)) {
		t.Fatal("Retain de int deve retornar false")
	}
	if Retain(NewString("s")) {
		t.Fatal("Retain de string deve retornar false")
	}
	if OwnersCount(NewInt(7)) != -1 {
		t.Fatal("OwnersCount de escalar deve ser -1")
	}
	Release(NewInt(7)) // nao deve entrar em panico
}

func TestReleaseClampsAtZero(t *testing.T) {
	m := NewMap()
	Release(m)
	Release(m)
	if OwnersCount(m) != 0 {
		t.Fatalf("Release nao pode ir abaixo de 0, veio %d", OwnersCount(m))
	}
}

func TestMapAndInstanceAlsoCount(t *testing.T) {
	m := NewMap()
	Retain(m)
	if OwnersCount(m) != 1 {
		t.Fatalf("map: esperado 1, veio %d", OwnersCount(m))
	}
	inst := Value{Type: VAL_OBJ, Obj: &ObjInstance{Fields: map[string]Value{}}}
	Retain(inst)
	if OwnersCount(inst) != 1 {
		t.Fatalf("instance: esperado 1, veio %d", OwnersCount(inst))
	}
}
