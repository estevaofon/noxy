package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// shareByOwners estabelece a precondição "compartilhado" pelo mecanismo de
// posse por contagem (spec docs/superpowers/specs/
// 2026-08-17-cow-rc-uniqueness-design.md §3): a unicidade é decidida por
// Owners > 1. Os testes de mecanismo abaixo registram os dois donos
// duráveis que o bytecode real registraria (ex.: slot de um `let` +
// elemento de um contêiner).
func shareByOwners(v value.Value) {
	value.Retain(v)
	value.Retain(v)
}

func TestShareByOwnersAndUnicize(t *testing.T) {
	machine := New()
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	outer := value.NewArray([]value.Value{inner})
	// inner é elemento durável de outer — o OP_ARRAY real teria retido
	// (Task 5); aqui o array foi montado fora do bytecode, então contamos à
	// mão para que o clone raso de outer o leve de 1 para 2 donos.
	value.Retain(inner)

	if value.IsShared(outer) {
		t.Fatal("array novo não deve nascer Shared")
	}
	v, cloned := machine.unicize(outer)
	if cloned || v.Obj != outer.Obj {
		t.Fatal("unicize de objeto não-Shared deve devolver o mesmo objeto sem clonar")
	}

	// spec §3: dois donos duráveis (aqui: o alias de teste) em vez do bit.
	shareByOwners(outer)
	ResetCloneCount()
	v, cloned = machine.unicize(outer)
	if !cloned || v.Obj == outer.Obj {
		t.Fatal("unicize de objeto Shared deve clonar")
	}
	if value.IsShared(v) {
		t.Fatal("clone deve nascer sem donos (não compartilhado)")
	}
	if !value.IsShared(inner) {
		t.Fatal("clone raso deve dar um dono a mais aos filhos compostos (ficam compartilhados)")
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperado 1 clone, contador = %d", CloneCountValue())
	}
	if v.Obj.(*value.ObjArray).Elements[0].Obj != inner.Obj {
		t.Fatal("clone raso deve compartilhar os filhos (mesmo ponteiro)")
	}
}

// NOTA (Task 8): havia aqui um teste que confirmava que a função de marcação
// (bit sticky, aposentada nesta task) era no-op em escalares. O mesmo
// invariante para escalares — IsShared sempre false — segue coberto abaixo;
// Retain/Release/OwnersCount também são no-op/-1 em escalares (ver
// rc_uniqueness_test.go).
func TestIsSharedFalseForScalars(t *testing.T) {
	n := value.NewInt(7)
	if value.IsShared(n) {
		t.Fatal("escalares nunca são compartilhados (sem contador de donos)")
	}
}

func TestUnicizeMapMarksValues(t *testing.T) {
	machine := New()
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	m := value.NewMap()
	m.Obj.(*value.ObjMap).Set("k", inner)
	// dono durável do valor guardado no map (OP_MAP real faria isso).
	value.Retain(inner)
	// spec §3: "compartilhado" agora é Owners > 1, não o bit sticky.
	shareByOwners(m)

	v, cloned := machine.unicize(m)
	if !cloned {
		t.Fatal("map com mais de um dono deve clonar")
	}
	if !value.IsShared(inner) {
		t.Fatal("valores do map clonado devem ganhar um dono (ficam compartilhados)")
	}
	got, ok := v.Obj.(*value.ObjMap).Get("k")
	if !ok || got.Obj != inner.Obj {
		t.Fatal("clone raso de map deve compartilhar os valores (mesmo ponteiro)")
	}
}

func TestUnicizeInstanceMarksFields(t *testing.T) {
	machine := New()
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	structDef := &value.ObjStruct{Name: "Box", Fields: []string{"data"}}
	inst := value.NewInstance(structDef)
	inst.Obj.(*value.ObjInstance).Fields["data"] = inner
	// dono durável do campo (o construtor real faria isso — Task 5).
	value.Retain(inner)
	// spec §3: "compartilhado" agora é Owners > 1, não o bit sticky.
	shareByOwners(inst)

	v, cloned := machine.unicize(inst)
	if !cloned {
		t.Fatal("instância com mais de um dono deve clonar")
	}
	if !value.IsShared(inner) {
		t.Fatal("campos compostos do clone devem ganhar um dono (ficam compartilhados)")
	}
	if v.Obj.(*value.ObjInstance).Fields["data"].Obj != inner.Obj {
		t.Fatal("clone raso de instância deve compartilhar os campos (mesmo ponteiro)")
	}
}
