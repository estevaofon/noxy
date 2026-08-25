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

// refArgument e o resultado de compileRefArgument.
type refArgument struct {
	element ast.NoxyType // tipo apontado; nil para null ou tipo desconhecido
	plain   ast.NoxyType // != nil quando o argumento e um valor T conhecido (R5 violada)
	proven  bool         // modo provado em compilacao (false: any/desconhecido -> validateParameterModes)
}

// compileRefArgument compila um argumento destinado a um parametro ou slot
// `ref T` (R5): `ref x` cria a referencia (R1, compileReferenceArgument);
// qualquer outra expressao e compilada como valor comum e precisa JA ter
// tipo `ref T` (variavel, campo, elemento, chamada que retorna ref), ser
// `null`, ou ter tipo desconhecido/any (fronteira dinamica). Um valor T
// conhecido volta em `plain` para o chamador montar o erro com a posicao.
func (c *Compiler) compileRefArgument(arg ast.Expression) (refArgument, error) {
	if prefix, ok := arg.(*ast.PrefixExpression); ok && prefix.Operator == "ref" {
		element, err := c.compileReferenceArgument(prefix.Right)
		if err != nil {
			return refArgument{}, err
		}
		return refArgument{element: element, proven: true}, nil
	}
	_, actual, err := c.Compile(arg)
	if err != nil {
		return refArgument{}, err
	}
	if ref, ok := actual.(*ast.RefType); ok {
		return refArgument{element: ref.ElementType, proven: true}, nil
	}
	if isNullType(actual) {
		return refArgument{proven: true}, nil
	}
	if actual == nil || isAny(actual) {
		return refArgument{}, nil
	}
	return refArgument{plain: actual}, nil
}

// exprDisplay renderiza expr como o fonte Noxy, para mensagens de diagnostico
// — sem os parenteses de agrupamento que MemberAccessExpression.String() e
// IndexExpression.String() usam internamente (AST, nao mensagem ao usuario).
func exprDisplay(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.MemberAccessExpression:
		return exprDisplay(e.Left) + "." + e.Member
	case *ast.IndexExpression:
		return exprDisplay(e.Left) + "[" + exprDisplay(e.Index) + "]"
	default:
		return expr.String()
	}
}

// refArgumentHint diz como consertar um valor T passado onde se esperava
// ref T: `ref x` para o que e enderecavel; para literal/temporario, uma
// variavel antes.
func refArgumentHint(arg ast.Expression) string {
	switch arg.(type) {
	case *ast.Identifier, *ast.MemberAccessExpression, *ast.IndexExpression:
		return fmt.Sprintf("\n  hint: use 'ref %s'", exprDisplay(arg))
	}
	return "\n  hint: bind the value to a variable and pass 'ref <name>'"
}

// alreadyReferenceError e R1: `ref e` com e ja de tipo `ref T`.
func alreadyReferenceError(line int, expr ast.Expression) error {
	display := exprDisplay(expr)
	return fmt.Errorf("[line %d] '%s' is already a reference\n  hint: pass '%s' directly, without 'ref'",
		line, display, display)
}
