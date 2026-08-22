package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// Semântica de valor (spec §2.2/§4.3, CRITICAL): uma escrita aninhada através
// de um contêiner nunca vaza para outra variável que compartilhe o mesmo
// valor. Os testes existentes cobrem o caminho "map → elemento" quando o
// elemento NÃO é compartilhado; estes cobrem o ramo de OP_MUT_INDEX/
// OP_MUT_PROPERTY em que o valor guardado no map/array está compartilhado
// com outra variável (ou com outro map) e precisa ser clonado antes da
// escrita — o ramo `changed` que o perfil mostrou sem cobertura.

const nestedContainerBody = `
    let inner: int[] = [1, 2]
    let m: map[string, int[]] = {"a": inner}
    m["a"][0] = 99
    let m2: map[string, int[]] = m
    m2["a"][1] = 77
    let b: Box = Box([1])
    let mb: map[string, Box] = {"b": b}
    mb["b"].values[0] = 5
    let row: int[] = [1, 2]
    let grid: int[][] = [row, row]
    grid[0][0] = 9
    test_report([inner, m["a"], m2["a"], b.values, mb["b"].values, row, grid[0], grid[1]])
`

func assertIntRows(t *testing.T, got value.Value, want [][]int64) {
	t.Helper()
	rows := semArray(t, got)
	if len(rows) != len(want) {
		t.Fatalf("linhas=%d, want %d", len(rows), len(want))
	}
	for i, row := range rows {
		cells := semArray(t, row)
		if len(cells) != len(want[i]) {
			t.Fatalf("linha %d: %s, want %v", i, row.String(), want[i])
		}
		for j, cell := range cells {
			if cell.Type != value.VAL_INT || cell.Int() != want[i][j] {
				t.Fatalf("linha %d: %s, want %v", i, row.String(), want[i])
			}
		}
	}
}

func TestNestedWriteThroughSharedMapValueDoesNotLeak(t *testing.T) {
	want := [][]int64{
		{1, 2},   // inner: intocado pela escrita via m["a"][0]
		{99, 2},  // m["a"]: recebeu a escrita, não a de m2
		{99, 77}, // m2["a"]: cópia de m no momento da atribuição + escrita própria
		{1},      // b.values: intocado pela escrita via mb["b"].values[0]
		{5},      // mb["b"].values
		{1, 2},   // row: intocado pela escrita via grid[0][0]
		{9, 2},   // grid[0]
		{1, 2},   // grid[1]: compartilhava row com grid[0] e não mudou
	}
	prelude := "struct Box\n    values: int[]\nend\n"
	t.Run("locals", func(t *testing.T) {
		assertIntRows(t, captureVMSource(t, prelude+"func main()\n"+nestedContainerBody+"end\nmain()\n"), want)
	})
	t.Run("globals", func(t *testing.T) {
		assertIntRows(t, captureVMSource(t, prelude+nestedContainerBody), want)
	})
}

func TestNestedWriteThroughMissingMapKeyIsRuntimeError(t *testing.T) {
	err := interpretVMSource(t, New(), "let m: map[string, int[]] = {\"a\": [1]}\nm[\"zz\"][0] = 1\n")
	if err == nil || !strings.Contains(err.Error(), "map key not found in mutation path") {
		t.Fatalf("esperava 'map key not found in mutation path', obtido %v", err)
	}
}
