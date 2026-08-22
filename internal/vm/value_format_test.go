package vm

import (
	"io"
	"os"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// print/iprint/eprint escrevem `Value.String()` de cada argumento unido por
// espaço — é o contrato de saída de todo programa Noxy. O perfil de cobertura
// mostrou que os formatos de função, native, instância, bytes e map nunca
// passavam por um teste Go; estes casos fixam o texto exato de cada tipo.

const valueFormatPrelude = `
struct Point
    x: int
    y: int
end
func soma(a: int, b: int) -> int
    return a + b
end
`

func TestValueStringFormatsByType(t *testing.T) {
	cases := []struct{ name, expr, want string }{
		{"int", "42", "42"},
		{"negative int", "-7", "-7"},
		{"float has six decimals", "1.5", "1.500000"},
		{"float whole", "2.0", "2.000000"},
		{"float scientific literal", "1e3", "1000.000000"},
		{"float sum", "0.1 + 0.2", "0.300000"},
		{"bool true", "true", "true"},
		{"bool false", "false", "false"},
		{"null", "null", "null"},
		{"string is bare", "\"hi\"", "hi"},
		{"empty string", "\"\"", ""},
		{"bytes keep the b prefix and quotes", "b\"abc\"", "b\"abc\""},
		{"int array", "[1, 2, 3]", "[1, 2, 3]"},
		{"nested array", "[[1, 2], [3]]", "[[1, 2], [3]]"},
		{"string array elements are bare", "[\"a\", \"b\"]", "[a, b]"},
		{"empty array", "[]", "[]"},
		{"single entry map", "{\"a\": 1}", "{a: 1}"},
		{"int keyed map", "{7: \"x\"}", "{7: x}"},
		{"empty map", "{}", "{}"},
		{"struct instance", "Point(1, 2)", "<Point instance>"},
		{"noxy function", "soma", "<fn soma>"},
		{"native function", "length", "<native fn length>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := captureVMSource(t, valueFormatPrelude+"test_report("+tc.expr+")\n")
			if got.String() != tc.want {
				t.Fatalf("String() de %s = %q, want %q", tc.expr, got.String(), tc.want)
			}
		})
	}
}

// A ordem das entradas de um map com mais de uma chave não é garantida (o
// formato itera o snapshot); o contrato é "chave: valor" separados por ", "
// entre chaves, com todas as entradas presentes.
func TestValueStringMultiEntryMapListsEveryEntry(t *testing.T) {
	got := captureVMSource(t, "test_report({\"a\": 1, \"b\": 2})\n").String()
	if !strings.HasPrefix(got, "{") || !strings.HasSuffix(got, "}") {
		t.Fatalf("map deveria ser envolvido por chaves: %q", got)
	}
	if !strings.Contains(got, "a: 1") || !strings.Contains(got, "b: 2") || !strings.Contains(got, ", ") {
		t.Fatalf("map deveria listar as duas entradas separadas por ', ': %q", got)
	}
}

func TestPrintJoinsArgumentsWithSpacesAndEndsWithNewline(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	machine := New()
	runErr := interpretVMSource(t, machine, valueFormatPrelude+`
print("a", 1, 2.5, [1], Point(1, 2), soma, b"x")
print()
iprint("sem", "quebra")
`)
	_ = writer.Close()
	os.Stdout = previous
	out, _ := io.ReadAll(reader)
	if runErr != nil {
		t.Fatal(runErr)
	}
	want := "a 1 2.500000 [1] <Point instance> <fn soma> b\"x\"\n\nsem quebra"
	if string(out) != want {
		t.Fatalf("stdout=%q, want %q", out, want)
	}
}

// Formato de referência: `print(r)` com r: ref T mostra o VALOR apontado (o
// auto-deref do print), não um endereço — é o que o usuário vê ao imprimir
// um parâmetro `ref` dentro de uma função.
func TestValueStringOfRefShowsPointedValue(t *testing.T) {
	got := captureVMSource(t, `
func show(r: ref int) -> void
    test_report(r)
end
let x: int = 41
show(ref x)
`)
	// Seja qual for a forma em que o native recebe o argumento (ref
	// embrulhado ou já resolvido), o texto é o do valor apontado.
	if got.Type == value.VAL_NULL || got.String() != "41" {
		t.Fatalf("ref int reportado como %q, want %q", got.String(), "41")
	}
}
