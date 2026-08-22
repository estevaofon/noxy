package vm

import (
	"strings"
	"testing"
)

// Issue #61 item 2: na fronteira dinamica (`any`) LER um campo inexistente
// ja era "undefined property"; ESCREVER criava o campo em silencio
// (OP_SET_PROPERTY nao conferia a declaracao) — struct e nominal, de campos
// fixos (spec §5). Agora a escrita num nome que nao esta na declaracao do
// struct e o mesmo erro da leitura; com base tipada o compilador ja rejeitava
// (`unknown field`).

func TestSetPropertyThroughAnyRejectsUndeclaredField(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"instance", "let p: any = Point(1, 2)\np.zzz = 1\n", "undefined property 'zzz'"},
		{"through ref", "let q: Point = Point(1, 2)\nlet r: ref Point = ref q\nlet a: any = r\na.www = 5\n", "undefined property 'www'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := interpretVMSource(t, New(), dynamicBoundaryPrelude+tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("esperava %q, obtido %v", tc.want, err)
			}
		})
	}
}

// (`let a: any = r` guarda uma COPIA do valor apontado — `any` nao carrega
// ref —, por isso o oraculo le a.y, nao q.y.)
func TestSetPropertyThroughAnyStillWritesDeclaredFields(t *testing.T) {
	got := captureVMSource(t, dynamicBoundaryPrelude+"let p: any = Point(1, 2)\np.x = 9\nlet q: Point = Point(3, 4)\nlet r: ref Point = ref q\nlet a: any = r\na.y = 8\ntest_report([p.x, a.y])\n")
	cells := semArray(t, got)
	if len(cells) != 2 || cells[0].AsInt != 9 || cells[1].AsInt != 8 {
		t.Fatalf("campos declarados devem continuar escrevendo: %v", got)
	}
}
