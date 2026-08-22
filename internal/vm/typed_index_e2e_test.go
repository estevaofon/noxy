package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Ponta a ponta (fonte -> compilador -> VM) da indexacao tipada de array
// (issue #66, item 1): mesmo resultado, mesma semantica CoW/RC e mesmas
// mensagens de erro do caminho generico. Cada caso "tipado" tem o gemeo
// "any" (base dinamica), que continua no generico: os dois tem de concordar.

func TestTypedIndexGlobalArrayReadWrite(t *testing.T) {
	got := captureVMSource(t, `
let xs: int[] = [1, 2, 3]
let i: int = 1
xs[i] = xs[i] * 10
xs[0] = xs[2]
test_report(xs)
`)
	arr := got.Obj.(*value.ObjArray)
	if arr.Elements[0].Int() != 3 || arr.Elements[1].Int() != 20 || arr.Elements[2].Int() != 3 {
		t.Fatalf("esperado [3, 20, 3], obtido %s", got.String())
	}
}

func TestTypedIndexNestedAndStringElements(t *testing.T) {
	got := captureVMSource(t, `
func f() -> string
    let g: string[][] = [["a", "b"], ["c", "d"]]
    g[0][1] = g[1][0] + "!"
    return g[0][1] + g[1][1]
end
test_report(f())
`)
	if s, ok := got.Obj.(string); !ok || s != "c!d" {
		t.Fatalf("esperado \"c!d\", obtido %s", got.String())
	}
}

// Elemento composto vindo por `any` numa base int[]: o NORC ve que o valor e
// rastreado e cai no generico, que retem — o composto ganha o dono do
// elemento, como no caminho generico.
func TestTypedIndexNorcRetainsCompositeFromAny(t *testing.T) {
	got := captureVMSource(t, `
let inner: int[] = [7]
let xs: int[] = [1, 2]
let v: any = inner
xs[0] = v
test_report(xs)
`)
	outer := got.Obj.(*value.ObjArray)
	if value.OwnersCount(outer.Elements[0]) < 2 {
		t.Fatalf("composto escrito via any deve ter o dono do elemento alem do global: owners=%d", value.OwnersCount(outer.Elements[0]))
	}
}

func TestTypedIndexErrorsMatchGenericPath(t *testing.T) {
	cases := []struct{ name, typed, dynamic, want string }{
		{"leitura fora da faixa", "let a: int[] = [1]\nlet i: int = 5\nprint(a[i])\n", "let a: any = [1]\nlet i: int = 5\nprint(a[i])\n", "array index out of bounds"},
		{"escrita fora da faixa", "let a: int[] = [1]\nlet i: int = 5\na[i] = 2\n", "let a: any = [1]\nlet i: int = 5\na[i] = 2\n", "array index out of bounds"},
		// (a escrita `a[i] = 2` com i: any e erro de COMPILACAO — "array index
		// must be int, got any" — no caminho generico desde antes; so a leitura
		// chega ao runtime.)
		{"indice nao inteiro via any", "let a: int[] = [1]\nlet i: any = \"x\"\nprint(a[i])\n", "let a: any = [1]\nlet i: any = \"x\"\nprint(a[i])\n", "array index must be integer"},
		{"nested fora da faixa", "let a: int[][] = [[1]]\nlet i: int = 5\na[0][i] = 1\n", "let a: any = [[1]]\nlet i: int = 5\na[0][i] = 1\n", "array index out of bounds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typedErr := interpretVMSource(t, New(), tc.typed)
			dynErr := interpretVMSource(t, New(), tc.dynamic)
			if typedErr == nil || !strings.Contains(typedErr.Error(), tc.want) {
				t.Fatalf("tipado: esperava %q, obtido %v", tc.want, typedErr)
			}
			if dynErr == nil || !strings.Contains(dynErr.Error(), tc.want) {
				t.Fatalf("dinamico: esperava %q, obtido %v", tc.want, dynErr)
			}
		})
	}
}

func TestTypedIndexLocalBubbleSortByValue(t *testing.T) {
	got := captureVMSource(t, `
func sorted() -> int[]
    let data: int[] = [5, 1, 4, 2, 3]
    let n: int = length(data)
    let i: int = 0
    while i < n do
        let j: int = 0
        while j < n - i - 1 do
            if data[j] > data[j + 1] then
                let tmp: int = data[j]
                data[j] = data[j + 1]
                data[j + 1] = tmp
            end
            j = j + 1
        end
        i = i + 1
    end
    return data
end
test_report(sorted())
`)
	arr := got.Obj.(*value.ObjArray)
	for k := range 5 {
		if arr.Elements[k].Int() != int64(k+1) {
			t.Fatalf("esperado [1..5], obtido %s", got.String())
		}
	}
}

// CoW: escrita fundida num local cujo array esta compartilhado clona — a copia
// nao ve a mutacao e o clone e exatamente um.
func TestTypedIndexLocalWriteClonesSharedArray(t *testing.T) {
	ResetCloneCount()
	got := captureVMSource(t, `
func f() -> int[]
    let a: int[] = [1, 2, 3]
    let b: int[] = a
    a[0] = 99
    return [a[0], b[0]]
end
test_report(f())
`)
	arr := got.Obj.(*value.ObjArray)
	if arr.Elements[0].Int() != 99 || arr.Elements[1].Int() != 1 {
		t.Fatalf("esperado [99, 1] (b intacto), obtido %s", got.String())
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperava exatamente 1 clone CoW, obtido %d", CloneCountValue())
	}
}

// for-each sobre array com mutacao durante a iteracao: o comportamento de hoje
// (a iteracao continua no mesmo array e ve a escrita) e preservado.
func TestTypedIndexForEachSeesMutationDuringIteration(t *testing.T) {
	got := captureVMSource(t, `
func f() -> int
    let xs: int[] = [1, 2, 3]
    let s: int = 0
    for x in xs do
        if x == 1 then
            xs[2] = 30
        end
        s = s + x
    end
    return s
end
test_report(f())
`)
	if got.Int() != 33 {
		t.Fatalf("esperado 33 (1 + 2 + 30), obtido %s", got.String())
	}
}

func TestTypedIndexLocalErrorsMatchGenericPath(t *testing.T) {
	cases := []struct{ name, typed, dynamic, want string }{
		{"leitura fora da faixa", "func f() -> int\n    let a: int[] = [1]\n    let i: int = 5\n    return a[i]\nend\nprint(f())\n", "func f() -> any\n    let a: any = [1]\n    let i: int = 5\n    return a[i]\nend\nprint(f())\n", "array index out of bounds"},
		{"escrita fora da faixa", "func f() -> int\n    let a: int[] = [1]\n    let i: int = 5\n    a[i] = 2\n    return a[0]\nend\nprint(f())\n", "func f() -> any\n    let a: any = [1]\n    let i: int = 5\n    a[i] = 2\n    return a[0]\nend\nprint(f())\n", "array index out of bounds"},
		{"indice nao inteiro via any", "func f() -> int\n    let a: int[] = [1]\n    let i: any = \"x\"\n    return a[i]\nend\nprint(f())\n", "func f() -> any\n    let a: any = [1]\n    let i: any = \"x\"\n    return a[i]\nend\nprint(f())\n", "array index must be integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typedErr := interpretVMSource(t, New(), tc.typed)
			dynErr := interpretVMSource(t, New(), tc.dynamic)
			if typedErr == nil || !strings.Contains(typedErr.Error(), tc.want) {
				t.Fatalf("tipado: esperava %q, obtido %v", tc.want, typedErr)
			}
			if dynErr == nil || !strings.Contains(dynErr.Error(), tc.want) {
				t.Fatalf("dinamico: esperava %q, obtido %v", tc.want, dynErr)
			}
		})
	}
}
