package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

// newMarkingVM registra um native test_mark_shared que retém o composto
// (spec §3: Owners > 1 é a unicidade — simula um segundo dono durável) e
// captura o ponteiro original.
func newMarkingVM() (*VM, *value.Value) {
	machine := New()
	captured := &value.Value{}
	machine.DefineNative("test_mark_shared", func(args []value.Value) value.Value {
		if len(args) == 1 {
			*captured = args[0]
			value.Retain(args[0])
		}
		return value.NewNull()
	})
	return machine, captured
}

// append sobre array Shared deve clonar antes de mutar: o co-dono não pode
// ver o elemento novo, e o slot deve passar a conter o clone.
func TestAppendUnicizesSharedTarget(t *testing.T) {
	machine, original := newMarkingVM()
	if err := interpretVMSource(t, machine, `let a: int[]
append(ref a, 1)
test_mark_shared(a)
append(ref a, 2)
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	after, ok := machine.GetGlobal("a")
	if !ok {
		t.Fatal("global a não encontrado")
	}
	if after.Obj == original.Obj {
		t.Fatal("append em array Shared deveria ter clonado (CoW)")
	}
	if n := len(original.Obj.(*value.ObjArray).Elements); n != 1 {
		t.Fatalf("o objeto original não pode ter sido mutado, tem %d elementos", n)
	}
	if n := len(after.Obj.(*value.ObjArray).Elements); n != 2 {
		t.Fatalf("o clone deve conter o elemento novo, tem %d", n)
	}
}

func TestPopUnicizesSharedTarget(t *testing.T) {
	machine, original := newMarkingVM()
	if err := interpretVMSource(t, machine, `let a: int[]
append(ref a, 1)
append(ref a, 2)
test_mark_shared(a)
pop(ref a)
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	after, _ := machine.GetGlobal("a")
	if after.Obj == original.Obj {
		t.Fatal("pop em array Shared deveria ter clonado (CoW)")
	}
	if n := len(original.Obj.(*value.ObjArray).Elements); n != 2 {
		t.Fatalf("o objeto original não pode ter sido mutado, tem %d elementos", n)
	}
	if n := len(after.Obj.(*value.ObjArray).Elements); n != 1 {
		t.Fatalf("o clone deve refletir o pop, tem %d elementos", n)
	}
}

func TestDeleteUnicizesSharedTarget(t *testing.T) {
	machine, original := newMarkingVM()
	if err := interpretVMSource(t, machine, `let m: map[string, int] = {"x": 1, "y": 2}
test_mark_shared(m)
delete(ref m, "x")
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	after, _ := machine.GetGlobal("m")
	if after.Obj == original.Obj {
		t.Fatal("delete em map Shared deveria ter clonado (CoW)")
	}
	if _, ok := original.Obj.(*value.ObjMap).Get("x"); !ok {
		t.Fatal("o objeto original não pode ter sido mutado")
	}
	if _, ok := after.Obj.(*value.ObjMap).Get("x"); ok {
		t.Fatal("o clone deve refletir o delete")
	}
}

// O item inserido por append fica Shared: o chamador ainda segura um ponteiro.
func TestAppendMarksInsertedComposite(t *testing.T) {
	machine := New()
	if err := interpretVMSource(t, machine, `let inner: int[]
append(ref inner, 1)
let outer: int[][]
append(ref outer, inner)
`); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	inner, _ := machine.GetGlobal("inner")
	if !value.IsShared(inner) {
		t.Fatal("item composto inserido via append deve ficar Shared")
	}
}
