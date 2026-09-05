package vm

// Issue #44 (4): builtin `type(v: any) -> string` — inspecao de tipo em
// runtime. A tabela de nomes e a MESMA do verbo %T do fmt (fonte unica), com
// duas correcoes sobre o comportamento antigo do %T: instancia de struct
// generico usa o nome de exibicao (sem qualificador de modulo) e ref deixa
// de ser "unknown".

import (
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func expectReportedString(t *testing.T, got value.Value, want string, msg string) {
	t.Helper()
	text, ok := got.Obj.(string)
	if got.Type != value.VAL_OBJ || !ok || text != want {
		t.Fatalf("%s: esperado %q, veio %s (%v)", msg, want, got.String(), got.Type)
	}
}

func TestTypeBuiltinNames(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"int", `test_report(type(1))`, "int"},
		{"float", `test_report(type(1.5))`, "float"},
		{"bool", `test_report(type(true))`, "bool"},
		{"null", `test_report(type(null))`, "null"},
		{"string", `test_report(type("x"))`, "string"},
		{"bytes", `test_report(type(b"x"))`, "bytes"},
		{"array", `test_report(type([1, 2]))`, "array"},
		{"map", "let m: map[string, int] = {\"a\": 1}\ntest_report(type(m))", "map"},
		{"funcao nativa", `test_report(type(print))`, "function"},
		{"funcao declarada", "func f() -> int\n    return 1\nend\ntest_report(type(f))", "function"},
		{"struct nominal", "struct Pessoa\n    nome: string\nend\ntest_report(type(Pessoa(\"Ana\")))", "Pessoa"},
		{"struct generico", "struct Caixa<T>\n    valor: T\nend\nlet c: Caixa<int> = Caixa(1)\ntest_report(type(c))", "Caixa<int>"},
		{"generico aninhado", "struct Caixa<T>\n    valor: T\nend\nlet dupla: Caixa<Caixa<int>> = Caixa(Caixa(9))\ntest_report(type(dupla))", "Caixa<Caixa<int>>"},
		{"ref", "struct Pessoa\n    nome: string\nend\nlet p: Pessoa = Pessoa(\"Ana\")\ntest_report(type(ref p))", "ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expectReportedString(t, captureVMSource(t, tt.src), tt.want, "type()")
		})
	}
}

func TestTypeBuiltinRuntimeHandles(t *testing.T) {
	machine := New()
	tests := []struct {
		name string
		arg  value.Value
		want string
	}{
		{"task", value.NewTask(), "task"},
		{"channel", value.NewChannel(1), "channel"},
		{"waitgroup", value.NewWaitGroup(), "waitgroup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "type", tt.arg)
			expectReportedString(t, got, tt.want, "type()")
		})
	}
}

// O construtor como valor (a DEFINICAO do struct, nao uma instancia) reporta
// "struct" — herdado do %T antigo e agora documentado na tabela do spec.
func TestTypeBuiltinStructDefinition(t *testing.T) {
	got := captureVMSource(t, "struct Pessoa\n    nome: string\nend\ntest_report(type(Pessoa))")
	expectReportedString(t, got, "struct", "type(Pessoa)")
}

func TestTypeBuiltinArityIsExactlyOne(t *testing.T) {
	machine := New()
	native := requireBuiltin(t, machine, "type")
	for _, args := range [][]value.Value{nil, {value.NewInt(1), value.NewInt(2)}} {
		if _, err := native.Invoke(machine, args); err == nil {
			t.Fatalf("type com %d argumentos deveria falhar", len(args))
		}
	}
}

// O %T do fmt compartilha a tabela de nomes do type(): instancia de struct
// generico imprime o nome de exibicao, nao a identidade interna com
// qualificador de modulo (antes: "main::Caixa<int>").
func TestFmtTypeVerbUsesDisplayName(t *testing.T) {
	got := captureVMSource(t, "struct Caixa<T>\n    valor: T\nend\ntest_report(fmt(\"%T\", Caixa(1)))")
	expectReportedString(t, got, "Caixa<int>", "fmt %T")
}
