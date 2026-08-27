package value

import (
	"math"
	"testing"
)

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

// Release decide "sai ou decrementa" com uma comparacao unica em uint32
// (cow.go) — forma que cabe no orcamento de inline. Este teste trava a
// equivalencia com `current <= 0 || current >= ownersSaturation` nas bordas.
func TestReleaseSingleCompareMatchesRange(t *testing.T) {
	arr := NewArray(nil)
	owners := ownersOf(arr)
	cases := []struct {
		start, want int32
	}{
		{0, 0},                                       // clamp: dec a mais nao desce
		{-1, -1},                                     // negativo nunca e tocado
		{math.MinInt32, math.MinInt32},               // borda do overflow de current-1
		{1, 0},                                       // ultimo dono vai a zero
		{2, 1},                                       // decremento normal
		{ownersSaturation - 1, ownersSaturation - 2}, // ainda rastreado
		{ownersSaturation, ownersSaturation},         // saturado: permanentemente compartilhado
		{ownersSaturation + 5, ownersSaturation + 5}, // acima da saturacao idem
		{math.MaxInt32, math.MaxInt32},
	}
	for _, c := range cases {
		owners.Store(c.start)
		Release(arr)
		if got := owners.Load(); got != c.want {
			t.Errorf("Release a partir de %d: veio %d, esperado %d", c.start, got, c.want)
		}
	}
}

func TestMapAndInstanceAlsoCount(t *testing.T) {
	m := NewMap()
	Retain(m)
	if OwnersCount(m) != 1 {
		t.Fatalf("map: esperado 1, veio %d", OwnersCount(m))
	}
	inst := NewInstance(&ObjStruct{Name: "P"})
	Retain(inst)
	if OwnersCount(inst) != 1 {
		t.Fatalf("instance: esperado 1, veio %d", OwnersCount(inst))
	}
}

// Cada construtor carimba a dica kind, e ownersOf tem de chegar ao Owners do
// ObjHeader tanto no Value carimbado quanto no montado a mao (kind zero) —
// se divergissem, Retain por um caminho e Release pelo outro contariam em
// objetos diferentes. A dica so tira do type switch o que nunca tem contador
// (objKindNoOwners); nunca decide sozinha que algo e composto.
func TestOwnersOfReachesHeaderWithAndWithoutKindHint(t *testing.T) {
	arr := NewArray(nil)
	arrObj := arr.Obj.(*ObjArray)
	if arr.kind != objKindArray {
		t.Fatalf("NewArray deve carimbar objKindArray, veio %d", arr.kind)
	}
	if NewArrayAdopting(nil).kind != objKindArray {
		t.Fatal("NewArrayAdopting deve carimbar objKindArray")
	}
	if ownersOf(arr) != &arrObj.Owners {
		t.Fatal("ownersOf(array) nao aponta para o Owners do header")
	}
	if ownersOf(Value{Type: VAL_OBJ, Obj: arrObj}) != &arrObj.Owners {
		t.Fatal("caminho lento (kind zero) diverge do rapido para array")
	}

	m := NewMap()
	mObj := m.Obj.(*ObjMap)
	if m.kind != objKindMap || ownersOf(m) != &mObj.Owners {
		t.Fatal("NewMap: dica ou ownersOf errados")
	}
	if NewMapWithData(nil).kind != objKindMap {
		t.Fatal("NewMapWithData deve carimbar objKindMap")
	}
	if ownersOf(Value{Type: VAL_OBJ, Obj: mObj}) != &mObj.Owners {
		t.Fatal("caminho lento diverge para map")
	}

	inst := NewInstance(&ObjStruct{Name: "P"})
	instObj := inst.Obj.(*ObjInstance)
	if inst.kind != objKindInstance || ownersOf(inst) != &instObj.Owners {
		t.Fatal("NewInstance: dica ou ownersOf errados")
	}
	if NewInstanceWith(&ObjStruct{Name: "P"}, nil).kind != objKindInstance {
		t.Fatal("NewInstanceWith deve carimbar objKindInstance")
	}
	if ownersOf(Value{Type: VAL_OBJ, Obj: instObj}) != &instObj.Owners {
		t.Fatal("caminho lento diverge para instance")
	}

	for _, v := range []Value{NewString("s"), NewStruct("S", nil), NewRuntimeTypeInfo(&RuntimeTypeInfo{})} {
		if v.kind != objKindNoOwners {
			t.Fatalf("%s deve carimbar objKindNoOwners, veio %d", v.String(), v.kind)
		}
		if ownersOf(v) != nil {
			t.Fatalf("%s nao tem contador", v.String())
		}
	}
	if ownersOf(NewInt(1)) != nil || ownersOf(NewNull()) != nil || ownersOf(Value{}) != nil {
		t.Fatal("escalares nao tem contador")
	}
	if ownersOf(Value{Type: VAL_OBJ, Obj: "crua"}) != nil {
		t.Fatal("string crua sem carimbo nao tem contador")
	}
}

func TestNewInstanceAdoptingDoesNotRetainAgain(t *testing.T) {
	child := NewArray(nil)
	Retain(child) // o chamador ja reteve em nome da instancia
	inst := NewInstanceAdopting(&ObjStruct{Name: "P", Fields: []string{"a"}}, []Value{child})
	if got := OwnersCount(child); got != 1 {
		t.Fatalf("NewInstanceAdopting nao pode reter de novo: Owners=%d, esperado 1", got)
	}
	if inst.kind != objKindInstance {
		t.Fatal("NewInstanceAdopting deve carimbar objKindInstance")
	}
}
