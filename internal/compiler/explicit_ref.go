package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// refReadHint e o hint que acompanha todo erro "esperava T, veio ref T"
// (spec 2026-08-24-explicit-ref, R2): a leitura e sempre explicita com '*'.
func refReadHint(expr ast.Expression) string {
	if ident, ok := expr.(*ast.Identifier); ok {
		return fmt.Sprintf("\n  hint: use '*%s' to read the referenced value", ident.Value)
	}
	return "\n  hint: use '*' to read the referenced value"
}

// rejectRefRead aplica R2 nas posicoes que nao passam por areTypesCompatible
// (operando de operador, condicao, indice, colecao de for): onde o compilador
// espera um valor, um `ref T` estatico e erro. `where` nomeia a posicao na
// mensagem ("operand of '+'", "condition", "index").
func (c *Compiler) rejectRefRead(t ast.NoxyType, expr ast.Expression, where string) error {
	if _, isRef := t.(*ast.RefType); !isRef {
		return nil
	}
	return fmt.Errorf("[line %d] %s cannot be %s: a ref is never read implicitly%s",
		c.currentLine, where, noxyTypeName(t), refReadHint(expr))
}
