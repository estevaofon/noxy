package compiler

// Issue #75: operandos aritmeticos/de comparacao de tipo estatico
// incompativel sao erro de COMPILACAO, com o texto do runtime + "got A and
// B" — mesmo padrao do #56 para `!`, `~` e bitwise. `any`, tipo desconhecido
// (nil) e `ref` (auto-deref) continuam no caminho generico com checagem em
// runtime. Bytecode de programa valido nao muda (a checagem so le os tipos
// que o compilador ja calcula para escolher OP_ADD_INT/OP_ADD_FLOAT).

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
)

func TestArithmeticOperandMismatchIsCompileError(t *testing.T) {
	cases := []struct{ source, want string }{
		{"let x: int = 10\nprint(x + \"ola\")\n", "[line 2] operands must be numbers or strings or bytes, got int and string"},
		{"let x = 10\nprint(x + \"ola\")\n", "[line 2] operands must be numbers or strings or bytes, got int and string"},
		{"print(true * 2)\n", "[line 1] operands must be numbers, got bool and int"},
		{"print(\"a\" - \"b\")\n", "[line 1] operands must be numbers, got string and string"},
		{"print(\"a\" / 2)\n", "[line 1] operands must be numbers, got string and int"},
		{"print(1.5 % 2)\n", "[line 1] operands for % must be integers, got float and int"},
		{"print(b\"a\" < b\"b\")\n", "[line 1] operands must be numbers or strings, got bytes and bytes"},
		{"print(\"a\" < 1)\n", "[line 1] operands must be numbers or strings, got string and int"},
		{"print(1 >= \"a\")\n", "[line 1] operands must be numbers or strings, got int and string"},
		{"print(true > false)\n", "[line 1] operands must be numbers or strings, got bool and bool"},
		{"print(b\"a\" + \"b\")\n", "[line 1] operands must be numbers or strings or bytes, got bytes and string"},
		{"let xs: int[] = [1]\nprint(xs + 1)\n", "[line 2] operands must be numbers or strings or bytes, got int[] and int"},
		{"print(1 + null)\n", "[line 1] operands must be numbers or strings or bytes, got int and null"},
		// Dentro de generico a checagem roda por instancia, na cadeia do §9.
		{"func soma<T>(a: T, b: T) -> T\n    return a + b\nend\nprint(soma(true, false))\n", "em soma<bool> (instanciado na linha 4): operands must be numbers or strings or bytes, got bool and bool"},
	}
	for _, tc := range cases {
		_, _, err := New().Compile(parse(tc.source))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("source %q: error=%v, want %q", tc.source, err, tc.want)
		}
	}
}

func TestArithmeticOperandsCompatibleStillCompile(t *testing.T) {
	for _, source := range []string{
		"print(1 + 2.5)\n",
		"print(2.5 - 1)\n",
		"print(\"a\" + \"b\")\n",
		"print(b\"a\" + b\"b\")\n",
		"print(\"a\" < \"b\")\n",
		"print(\"a\" <= \"b\", \"a\" >= \"b\")\n",
		"print(1 < 2.5)\n",
		"print(2.5 >= 1, 1 <= 2.5)\n",
		"func soma<T>(a: T, b: T) -> T\n    return a + b\nend\nprint(soma(1, 2), soma(\"a\", \"b\"), soma(1.5, 2.5))\n",
		"print(7 % 2)\n",
		// Fronteira dinamica: any e tipo desconhecido ficam para o runtime.
		"let v: any = 1\nprint(v + \"x\")\nprint(v < 2)\nprint(v % 2)\n",
		"print(desconhecido + 1)\n",
		// R2: leitura de `ref int` e sempre explicita com '*r'; depois do
		// deref e int + int.
		"let n: int = 1\nlet r: ref int = ref n\nprint(*r + 1)\nprint(1 < *r)\n",
		// Igualdade tem regra propria e nao entra aqui.
		"print(1 == \"a\")\nprint(1 != \"a\")\n",
	} {
		if _, _, err := New().Compile(parse(source)); err != nil {
			t.Errorf("source %q deveria compilar: %v", source, err)
		}
	}
}

// A checagem e uma funcao PURA sobre os tipos estaticos (nil = erro nenhum) e
// roda antes de qualquer emissao: o bytecode de programa valido nao muda por
// construcao (prova no corpus: diff de disassembly antes x depois, no PR).
func TestCheckArithmeticOperandsIsPureOverStaticTypes(t *testing.T) {
	intT, strT, anyT := builtinType("int"), builtinType("string"), builtinType("any")
	if err := checkArithmeticOperands("+", intT, strT); err == nil {
		t.Fatal("int + string deveria falhar")
	}
	for _, pair := range [][2]interface{}{{intT, intT}, {anyT, strT}, {nil, strT}, {strT, nil}} {
		var l, r = toType(pair[0]), toType(pair[1])
		if err := checkArithmeticOperands("+", l, r); err != nil {
			t.Fatalf("%v + %v nao deveria falhar: %v", l, r, err)
		}
	}
	// Operador fora do conjunto (igualdade, logico, bitwise) e no-op.
	for _, op := range []string{"==", "!=", "&&", "||", "&", "<<"} {
		if err := checkArithmeticOperands(op, intT, strT); err != nil {
			t.Fatalf("%s nao e da alcada desta checagem: %v", op, err)
		}
	}
}

func toType(v interface{}) ast.NoxyType {
	if v == nil {
		return nil
	}
	return v.(ast.NoxyType)
}
