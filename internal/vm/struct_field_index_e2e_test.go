package vm

import (
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

// Ponta a ponta (fonte -> compilador -> VM) do campo de struct por índice
// (issue #96): mesmo resultado, mesma semântica CoW/RC e mesmas mensagens de
// erro do caminho por nome. Cada caso "tipado" tem o gêmeo `any`, que continua
// no genérico: os dois têm de concordar.

const fieldIndexPrelude = `
struct Ponto
    x: int
    y: int
end
struct Caixa
    tag: string
    p: Ponto
end
`

func reportedStrings(t *testing.T, got value.Value, want []string) {
	t.Helper()
	cells := semArray(t, got)
	if len(cells) != len(want) {
		t.Fatalf("células=%d, want %d: %s", len(cells), len(want), got.String())
	}
	for i, cell := range cells {
		if s, ok := cell.Obj.(string); !ok || s != want[i] {
			t.Fatalf("célula %d: got %s, want %q", i, cell.String(), want[i])
		}
	}
}

func TestFieldIndexReadWriteMatchesByName(t *testing.T) {
	got := captureVMSource(t, fieldIndexPrelude+`
func f(p: Ponto) -> int
    p.y = p.x * 10
    return p.y + p.x
end
let g: Ponto = Ponto(3, 4)
let a: any = Ponto(3, 4)
g.y = g.x * 10
a.y = a.x * 10
test_report([to_str(f(Ponto(2, 0))), to_str(g.y + g.x), to_str(a.y + a.x)])
`)
	reportedStrings(t, got, []string{"22", "33", "33"})
}

// A GUARDA (spec §3.1): json_loads cria a própria definição de struct com os
// campos em ORDEM ALFABÉTICA para todo valor de struct que ele constrói. A
// instância entra num contêiner tipado, então a base tipada aponta para um
// layout diferente da declaração (slot 0 = `proximo`, não `valor`). Ler e
// escrever por índice nela tem de conferir o nome e cair no caminho por nome.
func TestFieldIndexGuardsReorderedJSONInstance(t *testing.T) {
	got := captureVMSource(t, `
struct Node
    valor: int
    proximo: ref Node
end
let xs: Node[] = []
let ok: bool = json_loads("[{\"valor\": 7, \"proximo\": null}]", ref xs)
let n: Node = xs[0]
n.valor = n.valor + 1
xs[0].valor = xs[0].valor * 10
let dyn: any = xs[0]
test_report([to_str(ok), to_str(n.valor), to_str(xs[0].valor), to_str(dyn.valor), to_str(n.proximo == null)])
`)
	reportedStrings(t, got, []string{"true", "8", "70", "70", "true"})
}

func TestFieldIndexNestedWriteClonesSharedChildOnce(t *testing.T) {
	got := captureVMSource(t, fieldIndexPrelude+`
let a: Caixa = Caixa("a", Ponto(1, 2))
let b: Caixa = a
a.p.x = 5
a.tag = "z"
test_report([to_str(b.p.x), to_str(a.p.x), b.tag, a.tag])
`)
	reportedStrings(t, got, []string{"1", "5", "a", "z"})
}

func TestFieldIndexWriteRetainsNewReleasesOld(t *testing.T) {
	got := captureVMSource(t, fieldIndexPrelude+`
struct Holder
    inner: int[]
end
let y1: int[] = [1]
let y2: int[] = [2]
let h: Holder = Holder([])
h.inner = y1
h.inner = y2
test_report([y1, y2, h.inner])
`)
	cells := semArray(t, got)
	y1 := cells[0].Obj.(*value.ObjArray)
	y2 := cells[1].Obj.(*value.ObjArray)
	// y1: a variável global (1) — o campo soltou; y2: global + campo + a
	// célula do array reportado (o test_report retém ao construir o literal).
	if y1.Owners.Load() >= y2.Owners.Load() {
		t.Fatalf("esperava y1 com menos donos que y2 depois de h.inner = y2: y1=%d y2=%d", y1.Owners.Load(), y2.Owners.Load())
	}
}

func TestFieldIndexRefTypedFieldWriteAndReadThrough(t *testing.T) {
	got := captureVMSource(t, `
struct Node
    v: int
    next: ref Node?
end
func main() -> void
    let a: Node = Node(1, null)
    let b: Node = Node(2, null)
    a.next = ref b
    b.v = 3
    let seen: int = 0
    if a.next != null then
        seen = a.next.v
    end
    a.next = null
    test_report([to_str(seen), to_str(a.next == null), to_str(b.v)])
end
main()
`)
	reportedStrings(t, got, []string{"3", "true", "3"})
}

// newFieldCorruptingVM registra `corrupt_field(inst, nome, v)`, que grava v
// direto no slot do campo, por baixo do checador. Desde a spec §2.4 uma base
// TIPADA não-anulável nunca guarda null por sintaxe legal (`let p: Ponto =
// null` e `p.x` sobre `Ponto?` sem teste são erros de compilação), então é o
// único jeito de alcançar o estado de runtime que a tabela abaixo compara.
func newFieldCorruptingVM(t *testing.T) *VM {
	t.Helper()
	machine := New()
	machine.DefineNative("corrupt_field", func(args []value.Value) value.Value {
		args[0].Obj.(*value.ObjInstance).MustSet(args[1].Obj.(string), args[2])
		return value.NewNull()
	})
	markProbeReadonly(t, machine, "corrupt_field")
	return machine
}

// Tabela de erros idênticos: a base tipada erra com a MESMA mensagem que a
// base `any` para o mesmo estado de runtime. O null chega à base tipada por
// corrupção (campo) ou pela fronteira `any` (parâmetro ref).
func TestFieldIndexErrorsMatchByName(t *testing.T) {
	cases := []struct{ name, typed, dynamic, want string }{
		{"read through null base",
			"let c: Caixa = Caixa(\"t\", Ponto(1, 2))\ncorrupt_field(c, \"p\", null)\nlet v: int = c.p.x\n",
			"let p: any = null\nlet v: any = p.x\n",
			"only instances and maps have properties"},
		{"write through null base",
			"let c: Caixa = Caixa(\"t\", Ponto(1, 2))\ncorrupt_field(c, \"p\", null)\nc.p.x = 1\n",
			"let p: any = null\np.x = 1\n",
			"only instances and maps have properties"},
		{"nested write through null base",
			"struct Saco\n    c: Caixa\nend\nlet s: Saco = Saco(Caixa(\"t\", Ponto(1, 2)))\ncorrupt_field(s, \"c\", null)\ns.c.p.x = 1\n",
			"let c: any = null\nc.p.x = 1\n",
			"only instances and maps have properties"},
		{"read through null ref",
			"func f(r: ref Ponto) -> int\n    return r.x\nend\nlet a: any = null\nlet v: int = f(a)\n",
			"", // sem gêmeo any: o OP_DEREF é da base ref tipada
			"cannot dereference null reference"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typedErr := interpretVMSource(t, newFieldCorruptingVM(t), fieldIndexPrelude+tc.typed)
			if typedErr == nil || !strings.Contains(typedErr.Error(), tc.want) {
				t.Fatalf("tipado: esperava %q, obtido %v", tc.want, typedErr)
			}
			if tc.dynamic == "" {
				return
			}
			dynErr := interpretVMSource(t, New(), fieldIndexPrelude+tc.dynamic)
			if dynErr == nil || !strings.Contains(dynErr.Error(), tc.want) {
				t.Fatalf("any: esperava %q, obtido %v", tc.want, dynErr)
			}
		})
	}
}

// Linha do erro: o operando novo tem 3 bytes; a linha reportada continua a
// da statement.
func TestFieldIndexErrorLineIsTheStatement(t *testing.T) {
	err := interpretVMSource(t, newFieldCorruptingVM(t), fieldIndexPrelude+`
let c: Caixa = Caixa("t", Ponto(1, 2))
corrupt_field(c, "p", null)
let a: int = 1
let v: int = c.p.x
`)
	if err == nil || !strings.Contains(err.Error(), "line 14]") {
		t.Fatalf("esperava erro na linha 14, obtido %v", err)
	}
}
