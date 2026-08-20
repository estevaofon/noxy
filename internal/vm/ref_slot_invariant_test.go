package vm

// Invariante do slot `ref T` (issue #50; spec
// docs/superpowers/specs/2026-08-20-ref-slot-invariant-design.md): um slot
// declarado `ref T` contem ref ou null. O runtime nao embrulha mais valor
// cru numa ref para o slot (shim da #51 removido) — e erro explicito — e a
// base `any` se comporta como a base tipada para slots ref.

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

const refSlotPrelude = `
struct Node
    valor: int
    proximo: ref Node
end
func eh_nulo(n: ref Node) -> bool
    return n == null
end
`

// newCorruptingVM registra natives de teste que gravam um valor CRU direto no
// slot, por baixo dos guards — depois deste PR e o unico jeito de fabricar o
// estado impossivel.
func newCorruptingVM(t *testing.T) *VM {
	t.Helper()
	machine := New()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		return value.NewNull()
	})
	machine.DefineNative("corrupt_ref_field", func(args []value.Value) value.Value {
		instance := args[0].Obj.(*value.ObjInstance)
		instance.Fields[args[1].Obj.(string)] = args[2]
		return value.NewNull()
	})
	machine.DefineNative("corrupt_ref_index", func(args []value.Value) value.Value {
		array := args[0].Obj.(*value.ObjArray)
		array.Elements[int(args[1].AsInt)] = args[2]
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "corrupt_ref_field")
	markProbeReadonly(t, machine, "corrupt_ref_index")
	return machine
}

func TestRawValueInRefFieldIsExplicitRuntimeError(t *testing.T) {
	cases := map[string]string{
		"argumento contextual": `
let a: Node = Node(1, null)
corrupt_ref_field(a, "proximo", Node(2, null))
let r: bool = eh_nulo(a.proximo)`,
		"ref explicito": `
let a: Node = Node(1, null)
corrupt_ref_field(a, "proximo", Node(2, null))
let r: bool = eh_nulo(ref a.proximo)`,
		"base any": `
let a: Node = Node(1, null)
corrupt_ref_field(a, "proximo", Node(2, null))
let d: any = a
let r: bool = eh_nulo(d.proximo)`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			machine := newCorruptingVM(t)
			err := interpretVMSource(t, machine, refSlotPrelude+src)
			if err == nil || !strings.Contains(err.Error(), "reference slot 'proximo' holds a non-reference value") {
				t.Fatalf("esperava erro explicito de slot ref com valor cru, veio %v", err)
			}
		})
	}
}

func TestRawValueInRefArrayElementIsExplicitRuntimeError(t *testing.T) {
	machine := newCorruptingVM(t)
	err := interpretVMSource(t, machine, refSlotPrelude+`
let arr: (ref Node)[] = [null]
corrupt_ref_index(arr, 0, Node(2, null))
let r: bool = eh_nulo(arr[0])`)
	if err == nil || !strings.Contains(err.Error(), "reference slot at index 0 holds a non-reference value") {
		t.Fatalf("esperava erro explicito de elemento ref com valor cru, veio %v", err)
	}
}

// Emenda 1 da #50: via base `any` o compilador emite OP_REF_PROPERTY (nao
// conhece o campo); o runtime consulta RefFields e encaminha como o opcode
// contextual — `*n = ...` sobre campo nulo e "cannot update null reference",
// igual a base tipada; campo nao-nulo escreve atraves da ref existente.
func TestAnyBaseRefFieldForwardsLikeTypedBase(t *testing.T) {
	err := runTypedFunctionProgramError(t, refSlotPrelude+`
func preenche(n: ref Node)
    *n = Node(7, null)
end
let a: any = Node(1, null)
preenche(a.proximo)`)
	if err == nil || !strings.Contains(err.Error(), "cannot update null reference") {
		t.Fatalf("via base any, campo ref nulo deve chegar como null: %v", err)
	}

	requireBoolResults(t, refSlotPrelude+`
func preenche(n: ref Node)
    *n = Node(7, null)
end
let b: Node = Node(2, null)
let a: any = Node(1, ref b)
preenche(a.proximo)
test_report([b.valor == 7, eh_nulo(a.proximo), eh_nulo(ref a.proximo)])`, []bool{true, false, false})
}

func TestAnyBaseNullRefFieldJSONLoadsReturnsFalse(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Holder
    child: ref any
end
let h: Holder = Holder(null)
let d: any = h
let ok: bool = json_loads("{\"a\": 1}", d.child)
if !ok && h.child == null then
    test_report(42)
else
    test_report(0)
end`)
	testExpectedObject(t, 42, got)
}

// Array/map etiquetados (RuntimeType) alcancados por `any`: OP_REF_INDEX
// encaminha o elemento/valor ref (null incluido; chave ausente le null).
func TestAnyBaseRefArrayAndMapSlotsForwardNull(t *testing.T) {
	requireBoolResults(t, `
func eh_nulo_int(r: ref int) -> bool
    return r == null
end
let arr: (ref int)[] = [null]
let m: map[string, ref int] = {}
let da: any = arr
let dm: any = m
test_report([eh_nulo_int(da[0]), eh_nulo_int(dm["x"])])`, []bool{true, true})
}

// Slot comum via `any` continua dando ref para o slot (comportamento antigo).
func TestAnyBasePlainFieldStillReferencesTheSlot(t *testing.T) {
	requireBoolResults(t, refSlotPrelude+`
func soma(n: ref int)
    *n = n + 10
end
let a: any = Node(1, null)
soma(a.valor)
let b: Node = a
test_report([b.valor == 11])`, []bool{true})
}
