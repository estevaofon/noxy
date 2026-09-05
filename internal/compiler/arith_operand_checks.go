package compiler

import (
	"fmt"

	"github.com/estevaofon/noxy/internal/ast"
)

// Checagem estatica dos operandos de `+ - * / %` e `< > <= >=` (issue #75):
// espelha em tempo de compilacao as regras que a VM aplica em runtime
// (executor.go, OP_ADD/OP_SUBTRACT/.../OP_GREATER), com o MESMO texto de erro
// mais "got A and B" — o padrao do #56 para `!`, `~` e bitwise. So le os tipos
// estaticos que o compilador ja calcula para escolher OP_ADD_INT/OP_ADD_FLOAT,
// e roda antes de qualquer emissao: o bytecode de programa valido nao muda.
//
// Fronteira dinamica: `any` e tipo desconhecido (nil) passam e ficam para o
// runtime, como em checkBitwiseOperands. `ref T` nunca chega aqui: o
// caminho infixo rejeita um operando ref antes, por rejectRefRead (R2,
// spec 2026-08-24-explicit-ref). `==`/`!=` tem regra propria (§2.3) e nao
// entram. Struct como operando e recusado antes, por structOperandName.
// (Unica variante textual do runtime nao reproduzida: OP_ADD com dois
// objetos nao-string — array+array, map+map — diz "numbers, strings or
// bytes" com virgula; aqui sai sempre a forma com "or".)
//
// Regras (as do runtime): `+` aceita numeros (int/float, misto promove),
// string+string e bytes+bytes; `-`, `*`, `/` aceitam numeros; `%` aceita
// int e int; `<`, `>`, `<=`, `>=` aceitam numeros ou string com string.
func checkArithmeticOperands(operator string, left, right ast.NoxyType) error {
	var message string
	var ok func(l, r string) bool
	switch operator {
	case "+":
		message = "operands must be numbers or strings or bytes"
		ok = func(l, r string) bool {
			return (isNumericName(l) && isNumericName(r)) || (l == "string" && r == "string") || (l == "bytes" && r == "bytes")
		}
	case "-", "*", "/":
		message = "operands must be numbers"
		ok = func(l, r string) bool { return isNumericName(l) && isNumericName(r) }
	case "%":
		message = "operands for % must be integers"
		ok = func(l, r string) bool { return l == "int" && r == "int" }
	case "<", ">", "<=", ">=":
		message = "operands must be numbers or strings"
		ok = func(l, r string) bool {
			return (isNumericName(l) && isNumericName(r)) || (l == "string" && r == "string")
		}
	default:
		return nil
	}
	if left == nil || right == nil || isAny(left) || isAny(right) {
		return nil
	}
	if ok(left.String(), right.String()) {
		return nil
	}
	return fmt.Errorf("%s, got %s and %s", message, left.String(), right.String())
}

func isNumericName(name string) bool {
	return name == "int" || name == "float"
}
