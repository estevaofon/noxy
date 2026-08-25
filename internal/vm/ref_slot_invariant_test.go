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
		array.Elements[int(args[1].Int())] = args[2]
		return value.NewNull()
	})
	machine.DefineNative("corrupt_ref_map", func(args []value.Value) value.Value {
		mapping := args[0].Obj.(*value.ObjMap)
		mapping.Set(args[1].Obj.(string), args[2])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "corrupt_ref_field")
	markProbeReadonly(t, machine, "corrupt_ref_index")
	markProbeReadonly(t, machine, "corrupt_ref_map")
	return machine
}

// R1/R5 (Task 6, spec 2026-08-24-explicit-ref) restringiram quando este
// invariante e checavel por sintaxe legal:
//   - "base any": `ref d.proximo` continua compilando (o compilador nao
//     conhece o campo por base dinamica) e continua caindo no fallback de
//     OP_REF_PROPERTY que consulta RefFields em runtime — mensagem exata
//     preservada.
//   - base TIPADA (`a: Node`, campo `proximo: ref Node` estaticamente
//     conhecido): nao ha mais sintaxe legal que alcance
//     OP_CONTEXT_REF_PROPERTY/forwardRefSlot aqui. `a.proximo` sem `ref` e
//     leitura comum (OP_GET_PROPERTY, sem checagem — R5 nao cria ref
//     implicitamente, entao nao ha mais "argumento contextual"); `ref
//     a.proximo` e agora erro de COMPILACAO (R1, 'a.proximo' ja e uma
//     referencia). O caso abaixo passa pelo boundary dinamico (`func`
//     bare) que ainda AUDITA o modo em runtime (validateParameterModes) —
//     mensagem generica, nao mais a especifica do campo.
func TestRawValueInRefFieldIsExplicitRuntimeError(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "dynamic call via bare func",
			src: `
let a: Node = Node(1, null)
corrupt_ref_field(a, "proximo", Node(2, null))
let dynamic: func = eh_nulo
let r: bool = dynamic(a.proximo)`,
			want: "function 'eh_nulo' argument 1: expected ref Node, got object",
		},
		{
			name: "base any",
			src: `
let a: Node = Node(1, null)
corrupt_ref_field(a, "proximo", Node(2, null))
let d: any = a
let r: bool = eh_nulo(ref d.proximo)`,
			want: "reference slot 'proximo' holds a non-reference value",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machine := newCorruptingVM(t)
			err := interpretVMSource(t, machine, refSlotPrelude+tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("esperava erro explicito de slot ref com valor cru, veio %v", err)
			}
		})
	}
}

// Base TIPADA: `arr[0]` sem `ref` e leitura comum agora (R5 nao cria ref
// implicitamente), sem o check especifico; a auditoria em runtime segue
// existindo no boundary dinamico (`func` bare — mesmo raciocinio de
// TestRawValueInRefFieldIsExplicitRuntimeError acima).
func TestRawValueInRefArrayElementIsExplicitRuntimeError(t *testing.T) {
	machine := newCorruptingVM(t)
	err := interpretVMSource(t, machine, refSlotPrelude+`
let arr: (ref Node)[] = [null]
corrupt_ref_index(arr, 0, Node(2, null))
let dynamic: func = eh_nulo
let r: bool = dynamic(arr[0])`)
	if err == nil || !strings.Contains(err.Error(), "function 'eh_nulo' argument 1: expected ref Node, got object") {
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
let ok: bool = json_loads("{\"a\": 1}", ref d.child)
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
    *n = *n + 10
end
let a: any = Node(1, null)
soma(ref a.valor)
let b: Node = a
test_report([b.valor == 11])`, []bool{true})
}

// Rota 5 (nao listada na issue): OP_SET_PROPERTY/OP_SET_INDEX nao validavam
// nada em base `any` e gravavam T cru em slot `ref T`. Agora e o gemeo
// dinamico do erro de compilacao; ref/null seguem aceitos.
func TestAnyBaseWriteOfRawValueIntoRefFieldIsRuntimeError(t *testing.T) {
	err := runTypedFunctionProgramError(t, refSlotPrelude+`
let a: any = Node(1, null)
a.proximo = Node(9, null)`)
	if err == nil || !strings.Contains(err.Error(), "cannot assign Node to ref Node") {
		t.Fatalf("esperava 'cannot assign Node to ref Node' em runtime, veio %v", err)
	}
}

func TestAnyBaseWriteOfRefOrNullIntoRefFieldIsAllowed(t *testing.T) {
	requireBoolResults(t, refSlotPrelude+`
let b: Node = Node(2, null)
let a: any = Node(1, null)
a.proximo = ref b
let ligado: bool = !eh_nulo(a.proximo)
a.proximo = null
test_report([ligado, eh_nulo(a.proximo)])`, []bool{true, true})
}

func TestAnyBaseWriteOfRawValueIntoRefElementIsRuntimeError(t *testing.T) {
	cases := map[string]string{
		"array": `
let arr: (ref int)[] = [null]
let d: any = arr
d[0] = 5`,
		"map": `
let m: map[string, ref int] = {}
let d: any = m
d["k"] = 5`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			err := runTypedFunctionProgramError(t, src)
			if err == nil || !strings.Contains(err.Error(), "cannot assign int to ref int") {
				t.Fatalf("esperava 'cannot assign int to ref int' em runtime, veio %v", err)
			}
		})
	}
}

// Campo comum via `any` segue fronteira dinamica sem checagem (inalterado).
func TestAnyBasePlainFieldWriteIsStillUnchecked(t *testing.T) {
	got := runTypedFunctionProgram(t, refSlotPrelude+`
let a: any = Node(1, null)
a.valor = "texto"
test_report(type(a.valor))`)
	if got.Type != value.VAL_OBJ || got.Obj.(string) != "string" {
		t.Fatalf("campo comum via any continua sem checagem; veio %v", got)
	}
}

// Valor de map `ref T`: mesmo invariante (encaminha ref/null; cru e erro
// explicito com `for key "k"`). Base TIPADA so alcanca mais o check
// especifico via boundary dinamico (`func` bare — R5 tornou `m["k"]` sem
// `ref` uma leitura comum); base `any` continua exata via `ref d["k"]`
// (OP_REF_INDEX ainda consulta o schema em runtime nesse caso).
func TestRawValueInRefMapValueIsExplicitRuntimeError(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "base tipada via dynamic call",
			src: `
let m: map[string, ref Node] = {"k": null}
corrupt_ref_map(m, "k", Node(2, null))
let dynamic: func = eh_nulo
let r: bool = dynamic(m["k"])`,
			want: "function 'eh_nulo' argument 1: expected ref Node, got object",
		},
		{
			name: "base any",
			src: `
let m: map[string, ref Node] = {"k": null}
corrupt_ref_map(m, "k", Node(2, null))
let d: any = m
let r: bool = eh_nulo(ref d["k"])`,
			want: `reference slot for key "k" holds a non-reference value`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machine := newCorruptingVM(t)
			err := interpretVMSource(t, machine, refSlotPrelude+tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("esperava erro explicito de valor de map ref com valor cru, veio %v", err)
			}
		})
	}
}

// Invariante universal (criterio de aceite da #50): para campo, elemento e
// valor de map, depois de cada produtor legitimo (rebind `ref novo`, null),
// `let viz: ref T = slot; type(ref viz)` e "ref" ou "null".
func TestEveryRefSlotHoldsRefOrNullAfterLegitimateWrites(t *testing.T) {
	requireBoolResults(t, refSlotPrelude+`
let novo: Node = Node(9, null)
let a: Node = Node(1, null)
let arr: (ref Node)[] = [null]
let m: map[string, ref Node] = {"k": null}
a.proximo = ref novo
arr[0] = ref novo
m["k"] = ref novo
let v1: ref Node = a.proximo
let v2: ref Node = arr[0]
let v3: ref Node = m["k"]
let depois_ref: bool = type(v1) == "ref" && type(v2) == "ref" && type(v3) == "ref"
a.proximo = null
arr[0] = null
m["k"] = null
let n1: ref Node = a.proximo
let n2: ref Node = arr[0]
let n3: ref Node = m["k"]
let depois_null: bool = type(n1) == "null" && type(n2) == "null" && type(n3) == "null"
test_report([depois_ref, depois_null])`, []bool{true, true})
}
